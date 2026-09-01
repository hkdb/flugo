package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/hkdb/flugo/pkg/bridge/dartpost"
)

// streamBuffer is the per-stream producer→drainer queue depth. Small: progress
// events are frequent and cheap, and a slow subscriber applies backpressure.
const streamBuffer = 32

var (
	errEmitterClosed = errors.New("flugo: stream cancelled")
	errStreamFailed  = errors.New("flugo: stream failed")
)

// Emitter is a typed, one-way Go→Dart stream. A bound method that returns an
// *Emitter[T] is exposed to Dart as a Stream<T> (see internal/codegen). The
// producer goroutine calls Send for each item and exactly one of Close/Fail
// when finished; long-running producers should also select on Done to stop
// early when the Dart subscriber cancels.
//
//	func (s *Downloads) Progress(id string) *bridge.Emitter[Progress] {
//	    em := bridge.NewEmitter[Progress]()
//	    go func() {
//	        defer em.Close()
//	        for p := range work {
//	            if em.Send(p) != nil { return } // subscriber went away
//	        }
//	    }()
//	    return em
//	}
type Emitter[T any] struct {
	ch         chan T
	res        chan error    // buffered(1): nil on Close, err on Fail
	cancelled  chan struct{} // closed when the subscriber cancels
	closeOnce  sync.Once
	cancelOnce sync.Once
}

// NewEmitter creates an Emitter for a stream method to return.
func NewEmitter[T any]() *Emitter[T] {
	return &Emitter[T]{
		ch:        make(chan T, streamBuffer),
		res:       make(chan error, 1),
		cancelled: make(chan struct{}),
	}
}

// Send delivers one item to the stream, blocking if the buffer is full. It
// returns an error once the subscriber has cancelled — a producer that keeps
// Sending will keep getting errors, so checking the return value is enough to
// stop even without watching Done.
func (e *Emitter[T]) Send(v T) error {
	// Give cancellation priority: once it's observed, Sends fail deterministically
	// (a bare two-case select could still pick the buffer if both are ready).
	select {
	case <-e.cancelled:
		return errEmitterClosed
	default:
	}
	select {
	case <-e.cancelled:
		return errEmitterClosed
	case e.ch <- v:
		return nil
	}
}

// Close ends the stream normally (Dart sees the Stream close).
func (e *Emitter[T]) Close() { e.closeOnce.Do(func() { e.res <- nil }) }

// Fail ends the stream with an error (Dart sees a Stream error). A nil err is
// replaced with a generic sentinel.
func (e *Emitter[T]) Fail(err error) {
	e.closeOnce.Do(func() {
		if err == nil {
			err = errStreamFailed
		}
		e.res <- err
	})
}

// Done is closed when the subscriber cancels. Producers may select on it to
// abandon expensive work early.
func (e *Emitter[T]) Done() <-chan struct{} { return e.cancelled }

// cancel is invoked by the runtime when the Dart subscriber unsubscribes.
func (e *Emitter[T]) cancel() { e.cancelOnce.Do(func() { close(e.cancelled) }) }

// drain runs the delivery loop, calling emit for each item (emit returns false
// if the post failed / subscriber is gone). Returns done=true when the producer
// finished on its own (with failErr from Fail, or nil from Close); done=false
// when the subscriber cancelled or a post failed (no terminal should be sent).
func (e *Emitter[T]) drain(emit func(any) bool) (done bool, failErr error) {
	for {
		select {
		case v := <-e.ch:
			if !emit(v) {
				return false, nil
			}
		case err := <-e.res:
			// Producer finished: flush anything still buffered, then terminate.
			for {
				select {
				case v := <-e.ch:
					if !emit(v) {
						return false, nil
					}
				default:
					return true, err
				}
			}
		case <-e.cancelled:
			return false, nil
		}
	}
}

// streamSource is the non-generic view of *Emitter[T] the runtime drains
// without knowing T.
type streamSource interface {
	drain(emit func(any) bool) (bool, error)
	cancel()
}

var (
	streamMu  sync.Mutex
	streams   = map[int64]streamSource{}
	streamSeq atomic.Int64
)

func registerStream(src streamSource) int64 {
	id := streamSeq.Add(1)
	streamMu.Lock()
	streams[id] = src
	streamMu.Unlock()
	return id
}

func unregisterStream(id int64) {
	streamMu.Lock()
	delete(streams, id)
	streamMu.Unlock()
}

// CancelStream is called (via FlugoStreamCancel) when the Dart subscriber
// unsubscribes; it signals the producer to stop.
func CancelStream(id int64) {
	streamMu.Lock()
	src := streams[id]
	streamMu.Unlock()
	if src != nil {
		src.cancel()
	}
}

// streamMsg is the JSON envelope posted to the stream's Dart port. Each stream
// owns its own port, so no stream id is needed on the wire.
type streamMsg struct {
	Ev   string `json:"ev"`             // "data" | "done" | "err"
	Data any    `json:"data,omitempty"` // present for "data"
	Err  string `json:"err,omitempty"`  // present for "err"
}

func postMsg(port int64, m streamMsg) bool {
	b, err := json.Marshal(m)
	if err != nil {
		// A value that won't marshal is posted as a stream error AND terminates
		// the stream (return false) — the caller stops draining rather than
		// silently continuing after an un-encodable item.
		eb, _ := json.Marshal(streamMsg{Ev: "err", Err: "marshal: " + err.Error()})
		dartpost.PostString(port, string(eb))
		return false
	}
	return dartpost.PostString(port, string(b))
}

// DispatchStream invokes a bound stream method (one returning *Emitter[T],
// optionally with a trailing error) and drains it to the Dart port in a
// background goroutine. It returns a stream id the Dart side passes to
// FlugoStreamCancel on unsubscribe. A returned id of 0 means the stream ended
// immediately (an error terminal was already posted, or there was nothing to
// stream).
func DispatchStream(method string, paramsJSON []byte, port int64) int64 {
	info, ok := lookup(method)
	if !ok {
		postMsg(port, streamMsg{Ev: "err", Err: "unknown method: " + method})
		return 0
	}
	args, err := decodeArgs(info, paramsJSON)
	if err != nil {
		postMsg(port, streamMsg{Ev: "err", Err: err.Error()})
		return 0
	}

	// callMethod recovers panics so a panicking stream method returns an error
	// terminal instead of unwinding across the cgo boundary and aborting.
	results, callErr := callMethod(info, args)
	if callErr != nil {
		postMsg(port, streamMsg{Ev: "err", Err: callErr.Error()})
		return 0
	}

	// Allow (*Emitter[T]) or (*Emitter[T], error).
	if len(results) == 2 && isErrorType(results[1].Type()) && !results[1].IsNil() {
		postMsg(port, streamMsg{Ev: "err", Err: results[1].Interface().(error).Error()})
		return 0
	}
	if len(results) == 0 {
		postMsg(port, streamMsg{Ev: "err", Err: method + ": stream method returned no emitter"})
		return 0
	}
	src, ok := results[0].Interface().(streamSource)
	if !ok || results[0].IsNil() {
		// Nil emitter (or wrong type): nothing to stream — close cleanly.
		postMsg(port, streamMsg{Ev: "done"})
		return 0
	}

	sid := registerStream(src)
	go func() {
		defer unregisterStream(sid)
		// Recover any panic from the drain path so it becomes a stream error
		// rather than crashing the process.
		defer func() {
			if r := recover(); r != nil {
				postMsg(port, streamMsg{Ev: "err", Err: fmt.Sprintf("stream panic: %v", r)})
			}
		}()
		done, failErr := src.drain(func(v any) bool {
			return postMsg(port, streamMsg{Ev: "data", Data: v})
		})
		if !done {
			// Subscriber cancelled or a post failed: make sure the producer stops.
			src.cancel()
			return
		}
		if failErr != nil {
			postMsg(port, streamMsg{Ev: "err", Err: failErr.Error()})
			return
		}
		postMsg(port, streamMsg{Ev: "done"})
	}()
	return sid
}

// decodeArgs positionally decodes a JSON params array into the reflect args for
// info's method (receiver first), matching Dispatch's decoding. Shared by the
// stream path.
func decodeArgs(info *methodInfo, paramsJSON []byte) ([]reflect.Value, error) {
	methodType := info.method.Type
	numIn := methodType.NumIn() - 1

	var rawParams []json.RawMessage
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &rawParams); err != nil {
			return nil, fmt.Errorf("invalid params JSON: %w", err)
		}
	}
	if len(rawParams) != numIn {
		return nil, fmt.Errorf("expects %d params, got %d", numIn, len(rawParams))
	}

	args := make([]reflect.Value, numIn+1)
	args[0] = info.receiver
	for i := 0; i < numIn; i++ {
		paramType := methodType.In(i + 1)
		paramPtr := reflect.New(paramType)
		if err := json.Unmarshal(rawParams[i], paramPtr.Interface()); err != nil {
			return nil, fmt.Errorf("param %d: %w", i, err)
		}
		args[i+1] = paramPtr.Elem()
	}
	return args, nil
}
