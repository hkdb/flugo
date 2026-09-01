package bridge

import (
	"errors"
	"reflect"
	"testing"
)

// TestEmitterDrainClose: items sent then Close arrive in order, terminating
// with done=true and no error.
func TestEmitterDrainClose(t *testing.T) {
	e := NewEmitter[int]()
	go func() {
		_ = e.Send(1)
		_ = e.Send(2)
		_ = e.Send(3)
		e.Close()
	}()

	var got []int
	done, failErr := e.drain(func(v any) bool {
		got = append(got, v.(int))
		return true
	})
	if !done || failErr != nil {
		t.Fatalf("want done=true err=nil, got done=%v err=%v", done, failErr)
	}
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("items = %v, want [1 2 3]", got)
	}
}

// TestEmitterDrainFail: Fail propagates the error terminal, and any item
// buffered before the failure is still flushed.
func TestEmitterDrainFail(t *testing.T) {
	e := NewEmitter[string]()
	go func() {
		_ = e.Send("a")
		e.Fail(errors.New("boom"))
	}()

	var got []string
	done, failErr := e.drain(func(v any) bool {
		got = append(got, v.(string))
		return true
	})
	if !done {
		t.Fatal("want done=true")
	}
	if failErr == nil || failErr.Error() != "boom" {
		t.Fatalf("failErr = %v, want boom", failErr)
	}
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("items = %v, want [a]", got)
	}
}

// TestEmitterCancel: cancelling ends the drain with done=false (no terminal),
// and subsequent Sends fail deterministically.
func TestEmitterCancel(t *testing.T) {
	e := NewEmitter[int]()
	drained := make(chan bool, 1)
	go func() {
		done, _ := e.drain(func(any) bool { return true })
		drained <- done
	}()

	e.cancel()
	if done := <-drained; done {
		t.Fatal("want done=false on cancel")
	}
	if err := e.Send(9); err == nil {
		t.Fatal("Send after cancel must error")
	}
}

// TestEmitterEmitFalseStops: when emit reports the subscriber is gone (post
// failed), the drain stops without a terminal.
func TestEmitterEmitFalseStops(t *testing.T) {
	e := NewEmitter[int]()
	go func() {
		// Producer keeps trying; Send starts failing once the drain cancels.
		for i := 0; i < 1000; i++ {
			if e.Send(i) != nil {
				return
			}
		}
		e.Close()
	}()

	done, _ := e.drain(func(any) bool { return false }) // first delivery "fails"
	if done {
		t.Fatal("emit=false should end the drain with done=false")
	}
	// The runtime cancels the source after an emit failure; do the same here so
	// the producer unblocks.
	e.cancel()
}

// TestDispatchStreamUnknownMethod: an unknown method id posts an error terminal
// and returns stream id 0. (dartpost isn't initialized in tests, so the post is
// a no-op; we're asserting the control path, not delivery.)
func TestDispatchStreamUnknownMethod(t *testing.T) {
	if sid := DispatchStream("Nope.Missing", nil, 0); sid != 0 {
		t.Fatalf("unknown method should return sid 0, got %d", sid)
	}
}
