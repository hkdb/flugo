# Secure Intake (`bridge.Secret`)

Flugo's standard FFI path (`FlugoCall`) serializes method arguments as JSON, which is fine for almost everything but actively bad for sensitive bytes — passphrases, key material, anything the user expects to never appear in plaintext on disk or in a heap dump. Dart Strings and Go strings are immutable and GC-managed; once they exist, you cannot zero them. The secure intake path (`bridge.Secret` + `FlugoCallSecure`) gives you a parallel channel: bytes go from a wipeable Dart `Uint8List`, through a wipeable C buffer, into a `memguard` enclave on the Go side, never materialized as a string by the bridge layer.

Use this when you receive a passphrase, secret key, or comparable material from the UI and want to seal it into protected memory before doing anything else with it.

## Why the standard path leaks

A normal `FlugoCall` produces around five plaintext copies of every string argument:

1. The originating Dart String (e.g. `controller.text` from a `TextField`) — immutable, unwipeable.
2. The intermediate `jsonEncode([..., passphrase])` String — also unwipeable.
3. A UTF-8 `Pointer<Utf8>` allocated by `toNativeUtf8()` and freed via `calloc.free` (no zeroing).
4. The Go `[]byte` copy from `C.GoBytes`, then a `string` produced by `json.Unmarshal`.
5. The handler's parameter — yet another Go string copy.

Garbage collection might eventually reclaim the heap pages, but a process snapshot, swap file, or hibernation image taken between the call and GC will contain every copy. Logging, panic dumps, and crash reports can also leak them.

**In scope** for the secure path: offline disk reads (swap, hibernation, core dumps), live heap dumps from a non-root local process, idle-state inspection.

**Out of scope:** root attacker with full process control during active use, microarchitectural side channels, hardware bus snooping. Those need different mitigations (TPM-bound keys, sealed enclaves, etc.).

## API

The public surface is one type and three methods, in `flugo/pkg/bridge/secret.go`:

```go
type Secret struct { /* opaque */ }

// NewSecret wraps a byte slice into a sealed memguard enclave. The input
// slice IS wiped during sealing -- do not retain or reuse it.
func NewSecret(b []byte) (*Secret, error)

// Open temporarily exposes the bytes inside a memguard.LockedBuffer.
// Caller MUST defer Destroy on the returned buffer.
func (s *Secret) Open() (*memguard.LockedBuffer, error)

// Destroy wipes the underlying enclave. Idempotent and nil-safe.
func (s *Secret) Destroy()
```

The canonical handler shape is:

```go
defer secret.Destroy()
buf, err := secret.Open()
if err != nil { return err }
defer buf.Destroy()
// use buf.Bytes() within this scope
```

User code rarely needs to call `NewSecret` directly — `DispatchSecure` does it on intake when the bridge receives bytes via `FlugoCallSecure`. `NewSecret` is exposed mainly for tests and for the rare case where a Go-side caller wants to construct a `*Secret` to pass into a method that already speaks the secure-handler API.

## Wire flow

For `unlock(Uint8List)` from Dart to a Go method `Unlock(secret *bridge.Secret) error`:

```
Dart side                                  Go side
─────────                                  ───────
Uint8List secret  ──┐
                    │
FlugoBridge        │
  .callSecure() ───┤
    │              │
    ▼              │
calloc<Uint8>      │   FlugoCallSecure (cgo)
  copy bytes ──────┼──► C.GoBytes(ptr, len) ──► []byte
  call FFI         │
  wipe C buffer    │   DispatchSecure(name, b)
  free C buffer    │     memguard.NewBufferFromBytes(b).Seal()  ← wipes b
                   │     ↓
                   │   *bridge.Secret  ──►  Unlock(secret)
                   │                          buf := secret.Open()
                   │                          defer buf.Destroy()
                   │                          // use buf.Bytes()
                   │                          defer secret.Destroy()
                   │
caller wipes       ▼
secret.fillRange(0,…)
```

Wipeable copies: the Dart `Uint8List` (caller wipes via `fillRange`), the C-allocated buffer (`callSecure` wipes before `calloc.free`), the Go `[]byte` (`memguard.NewBufferFromBytes` wipes during seal). The only residual unwipeable copy is whatever held the bytes before they reached `Uint8List` — typically a `TextEditingController.text` String for a one-call dialog scope.

## Usage from Go

Bind a method that takes exactly one `*bridge.Secret` parameter:

```go
package main

import (
    "fmt"

    "github.com/hkdb/flugo/pkg/bridge"
)

type MyService struct{}

func (s *MyService) Unlock(secret *bridge.Secret) error {
    defer secret.Destroy()

    buf, err := secret.Open()
    if err != nil {
        return fmt.Errorf("opening secret: %w", err)
    }
    defer buf.Destroy()

    // Use buf.Bytes() here. If you must pass it to a string-typed
    // callback (e.g. some library that takes a passphrase string),
    // scope the materialization to that single call -- never store
    // the value in a Go string variable that outlives the call.
    return s.validate(buf.Bytes())
}

func main() {
    bridge.Bind(&MyService{})
}
```

Constraints (enforced by `DispatchSecure` at runtime):

- The method must take **exactly one** user-visible parameter.
- That parameter must be of type `*bridge.Secret`.

Multi-arg variants like `Unlock(name string, secret *bridge.Secret) error` are not supported — `flugo generate` REJECTS any signature that mixes a `*bridge.Secret` with other parameters (it used to silently emit broken Dart). Pass additional context via a separate prior JSON call (e.g. `SetTargetIdentity(name)`), or use the stage-then-consume pattern: a lone-Secret method parks the secret in a memguard enclave, and the multi-arg method consumes it single-use. Splitting is the cheaper fix in practice.

## Usage from Dart

`flugo generate` detects the `*bridge.Secret` parameter and emits a Dart wrapper that takes a `Uint8List`:

```dart
class MyService {
  Future<void> unlock(Uint8List secret) async {
    final response = FlugoBridge.callSecure('MyService.Unlock', secret);
    FlugoBridge.decodeResponse(response);
  }
}
```

The recommended caller pattern wipes the bytes after dispatch:

```dart
final secret = Uint8List.fromList(utf8.encode(controller.text));
try {
  await myService.unlock(secret);
} finally {
  secret.fillRange(0, secret.length, 0);
}
```

The generated wrapper is **synchronous** under the hood (no `Isolate.run`). Secure intake is rare in practice — typically once per session for an unlock — so the cost of a `TransferableTypedData` round-trip is not worth the per-call complexity. If you have a workload that fires many secure calls per second (you probably shouldn't), file an issue.

The residual leak is the source String: `TextEditingController.text` is a Dart String, immutable, unwipeable. The mitigation is to keep its lifetime bounded — dispose the controller as soon as the dialog closes, and don't cache the value anywhere. There is no way to fully eliminate this without replacing Flutter's text input widget with a custom Uint8List-backed one, which is rarely worth the engineering cost.

## Codegen behavior

`flugo generate` walks every bound method's signature. A method with exactly one `*bridge.Secret` parameter is treated as secure: the Dart wrapper takes a `Uint8List` and routes through `callSecure`. All other methods stay on the standard JSON path.

The generator also conditionally emits supporting code only when at least one bound method uses the secure path:

- `import 'dart:typed_data'` in `bridge.gen.dart`.
- The `_FlugoCallSecureNative` / `_FlugoCallSecureDart` typedefs.
- The `_flugoCallSecure` lookup in `FlugoBridge.init()`.
- The `FlugoBridge.callSecure` static helper.

Services that don't use secrets get the same minimal generated output as before — no unused imports, no unreachable bindings.

The generated C header (`bridge.gen.h`) always declares `FlugoCallSecure` regardless of whether your service uses it. This costs nothing (one extern declaration) and keeps the header stable across changes to your service's signature mix.

## When NOT to use it

- **The data isn't actually sensitive.** JSON is fine for usernames, file paths, settings — don't reach for the secure path just because.
- **The method needs more than one parameter alongside the secret.** Split it into two calls (`SetContext(...)` followed by `Unlock(secret)`, or stage-then-consume). The bridge does not support mixed-arity secure dispatch, and `flugo generate` fails loudly on such signatures rather than degrading to the JSON path.
- **The secret is already in your hand as an unwipeable String.** If you got the value from an OAuth library that returned a String, the leak is upstream — sealing it now into a `Secret` doesn't undo the GC-tracked copies that already exist.
- **You want long-lived secret storage.** `bridge.Secret` is for *intake*. For long-lived sealed material in your service, hold a `*memguard.Enclave` directly and manage its lifecycle yourself.

## Pitfalls

- **Forgetting `defer secret.Destroy()`.** The enclave then leaks until GC runs (which it might never do for a long-lived service). Always defer at the top of the handler, even before the `Open` call.
- **Re-using the same `Uint8List` for two calls.** Wiping after the first call zeros the source for the second. Allocate a fresh `Uint8List` per dispatch.
- **Logging the bytes.** Hex-dumping a `buf.Bytes()` "for debugging" prints the secret to stderr where it may be captured by journald, the IDE console, or a CI log. Don't.
- **Calling `secret.Open()` twice without re-sealing.** Each `Open` produces a fresh `LockedBuffer` that you must Destroy independently. A double-Open without two matching Destroys leaks.
- **Synchronous wrapper on the UI isolate.** `callSecure` blocks the calling isolate for the duration of the Go-side handler. If the handler is slow (e.g. an expensive KDF), you'll see a frame drop. Either make the handler fast or move the call off the UI isolate via your own `Isolate.run` (manually copying the `Uint8List`, since the secure wrapper itself doesn't isolate-hop).

## See also

- `flugo/pkg/bridge/secret.go` — canonical type docstrings.
- `flugo/pkg/bridge/handler.go` — `DispatchSecure` implementation.
- README's "Secure Intake (Sensitive Material)" section — the quick-start summary.
