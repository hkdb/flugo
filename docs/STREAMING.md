# Real-time streams (`bridge.Emitter`)

Flugo's standard FFI path (`FlugoCall`) is strictly request/response: Dart calls a Go method, Go computes one answer, Dart gets it back. That's the right shape for almost everything, but it can't express *progress* — a long download reporting percent-complete, a tail of log lines, a live counter. Historically apps faked this by polling a Go getter on a `Timer`, which is laggy and wasteful.

The streaming channel gives Go a way to **push** a sequence of typed values to Dart. A bound method that returns a `*bridge.Emitter[T]` is generated as a Dart `Stream<T>`. Under the hood it uses the Dart Native API (`Dart_PostCObject_DL`) to deliver messages to a Dart `ReceivePort` from any Go goroutine — in-process, no sockets, on every platform flugo targets (desktop, Android, iOS).

Use it for anything a `Stream` models: progress, incremental results, live events.

## API

The public surface is one generic type, in `flugo/pkg/bridge/stream.go`:

```go
type Emitter[T any] struct { /* opaque */ }

func NewEmitter[T any]() *Emitter[T]

func (e *Emitter[T]) Send(v T) error     // deliver one item (errors once cancelled)
func (e *Emitter[T]) Close()             // end the stream normally
func (e *Emitter[T]) Fail(err error)     // end the stream with an error
func (e *Emitter[T]) Done() <-chan struct{} // closed when the subscriber cancels
```

Rules:

- A streaming method returns **exactly one** `*bridge.Emitter[T]`, optionally with a trailing `error` (e.g. for setup that fails before any item). It cannot also take a `*bridge.Secret` (the secure channel and the stream channel are separate).
- Call `Send` for each item, then exactly one of `Close` / `Fail`. A producer that keeps `Send`ing after cancellation just gets errors back — checking the return value is enough to stop.
- Long-running producers should also `select` on `Done()` so they abandon expensive work the moment the Dart side unsubscribes.

> **Note — `omitempty` on scalar / list / map / bytes fields is fine.** An omitted field is absent from the JSON (decodes to `null` in Dart); the generated `fromJson` now defaults it to the type's zero value (`''`, `0`, `false`, empty `List`/`Map`/`Uint8List`). The one exception is a **nested-object (struct) field** — a `null` there has no zero value, so the generated `X.fromJson(... as Map<String, dynamic>)` cast would still throw. Keep struct fields non-`omitempty` (always send the object) until the generator grows nullable-field support.

## Declaring a stream

Write a normal flugo service method that returns an emitter, and start a goroutine that fills it:

```go
type Downloads struct{}

type Progress struct {
    Phase string  `json:"phase"` // "download" | "decrypt"
    Pct   float64 `json:"pct"`   // 0..1
}

func (d *Downloads) Watch(id string) *bridge.Emitter[Progress] {
    em := bridge.NewEmitter[Progress]()
    go func() {
        defer em.Close()
        for p := range work(id) {
            if em.Send(Progress{Phase: "download", Pct: p}) != nil {
                return // subscriber went away
            }
        }
    }()
    return em
}
```

Bind it as usual (`bridge.Bind(&Downloads{})`) and run `flugo generate`.

## What gets generated

```dart
Stream<Progress> watch(String id) {
  return FlugoBridge.openStream<Progress>('Downloads.Watch', [id], (d) => Progress.fromJson(d as Map<String, dynamic>));
}
```

Consume it like any Dart stream — cancelling the subscription tells Go to stop:

```dart
final sub = downloads.watch(id).listen((p) => setState(() => _pct = p.pct));
// ... later, or automatically on widget dispose:
await sub.cancel();
```

## How it works

- **Transport.** Each open stream creates a Dart `RawReceivePort`. Go receives the port's native id and posts each item as a small JSON envelope (`{ev: "data", data: …}`, then `{ev: "done"}` or `{ev: "err", …}`) via `Dart_PostCObject_DL`, which copies the payload into the Dart heap at post time — safe to call from any thread with no lifetime races. The Dart side decodes each envelope into `T` and feeds a `StreamController`.
- **One-time init.** The first `openStream` call wires up the Dart dynamic-linking API (`Dart_InitializeApiDL` via `NativeApi.initializeApiDLData`). Nothing to do in app code beyond the usual `FlugoBridge.init(...)` at startup.
- **Cancellation.** Cancelling the Dart subscription calls `FlugoStreamCancel`, which unblocks the producer (its `Send` starts failing and `Done()` closes). A dead port (app backgrounded, isolate gone) is detected on the next post and also stops the producer.
- **Backpressure.** The producer→Dart queue is small; a slow subscriber slows the producer via `Send` rather than growing memory without bound.

## Vendored Dart headers

The transport is implemented in `flugo/pkg/bridge/dartpost`, which vendors the Dart SDK's Native-API dynamic-linking C sources (`dart_api_dl.*` et al., BSD-licensed — see `dartpost/DART_LICENSE.md`). They are copied verbatim; refresh them from a Dart SDK's `bin/cache/dart-sdk/include/` if the ABI version bumps.
