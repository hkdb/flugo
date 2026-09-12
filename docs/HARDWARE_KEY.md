# Hardware-Key Challenge-Response

Flugo ships a `hardware_key` Flutter plugin for **HMAC-SHA1 challenge-response** with an external hardware
security key (YubiKey) over **NFC or USB**, on Android and iOS. Give it a challenge and a slot; it returns the
device's 20-byte HMAC-SHA1 response.

The plugin is **transport only** — it talks to the key and hands you the raw response. What you do with that
response (derive a key-encryption key, unlock a vault, gate a login, …) is entirely up to your app, typically in
your Go backend. This keeps the plugin generic and reusable by any Flugo app.

> Vendor scope: **YubiKey**. Other devices (e.g. NitroKey 3, which uses a different NFC applet) need a separate
> transport and are not covered yet.

> **⚠️ iOS is untested:** the iOS backend is implemented but has **not been tested end-to-end** yet — expect
> rough edges until it's confirmed on a real device. Android is the exercised path.

## Adding it

The plugin lives in the Flugo repo under `plugins/hardware_key`. Depend on it from your app's
`frontend/pubspec.yaml`:

```yaml
dependencies:
  hardware_key:
    git:
      url: https://github.com/hkdb/flugo.git
      ref: v0.1.11              # pin to the flugo version you build against
      path: plugins/hardware_key
```

It's a standard Flutter plugin, so Flutter registers the native side automatically — no manual channel wiring.

## Usage

```dart
import 'package:hardware_key/hardware_key.dart';

// Gate the UI: only offer NFC if the device supports it and it's turned on.
if (await HardwareKey.isNFCAvailable() && await HardwareKey.isNFCEnabled()) {
  // show NFC affordances
}

// Show your own "tap / plug in your key" sheet BEFORE calling this — it blocks
// until the key responds (NFC tap, or USB-OTG / Lightning), or the platform times out.
try {
  final res = await HardwareKey.challengeResponse(
    challenge: myChallengeBytes,          // Uint8List
    slot: HardwareKeySlot.slot2,          // long-press slot (default)
  );
  // res.response is the 20-byte HMAC-SHA1 — feed it to your own KEK/verify logic.
  useResponse(res.response);
} on HardwareKeyException catch (e) {
  if (e.code == 'cancelled') {
    // user dismissed — call HardwareKey.cancel() from your sheet's close handler
  }
}
```

## API Reference

### challengeResponse

Perform an HMAC-SHA1 challenge-response against the given slot. Blocks until the user taps/plugs in the key or
the platform's discovery window times out.

```dart
static Future<HardwareKeyResponse> challengeResponse({
  required Uint8List challenge,
  HardwareKeySlot slot = HardwareKeySlot.slot2,
})
```

On Android the platform layer runs NFC discovery and USB-OTG listening **in parallel** — whichever device
arrives first wins. Throws `HardwareKeyException` on `cancelled`, `no_device`, `wrong_slot`, `timeout`,
`unsupported_platform`, or `unknown`.

### isNFCAvailable

Whether the device hardware advertises NFC at all (not whether it's enabled). Use it to decide whether to show
NFC UI.

```dart
static Future<bool> isNFCAvailable()
```

### isNFCEnabled

Whether NFC is currently turned on. Android-specific; iOS always returns `true` (CoreNFC is either present or
unavailable).

```dart
static Future<bool> isNFCEnabled()
```

### cancel

Cancel an in-flight `challengeResponse` (e.g. the user dismissed your prompt). Safe to call when nothing is in
flight.

```dart
static Future<void> cancel()
```

## Types

### HardwareKeyResponse

```dart
class HardwareKeyResponse {
  final Uint8List response;   // 20-byte HMAC-SHA1
  final String serial;        // device serial ("" if not surfaced)
  final String family;        // "yubikey" (reserved for future vendors)
}
```

### HardwareKeySlot

`slot1` (short-press, `0x30`) and `slot2` (long-press, `0x38`). Most apps use `slot2`. The plugin maps the enum
to the platform-specific constant.

### HardwareKeyException

`code` is one of `cancelled`, `no_device`, `wrong_slot`, `timeout`, `unsupported_platform`, `unknown`; `message`
carries platform detail.

## Platform backends

| Platform | Transport | Native layer |
|----------|-----------|--------------|
| Android  | NFC + USB-OTG | Yubico `com.yubico.yubikit:yubiotp` |
| iOS      | NFC + MFi Lightning / USB-C | Yubico `YubiKit` pod |

The slot must be programmed on the key for HMAC-SHA1 challenge-response (e.g. with the YubiKey Manager /
`ykman`). The plugin doesn't program keys — it only issues challenges to an already-configured slot.
