# WebAuthn / FIDO2 Security Keys

Flugo ships a `webauthn` Flutter plugin for driving **external security-key** ceremonies on mobile — a **login**
(assertion) or an **enroll** (attestation) — so a Flutter app can authenticate to a WebAuthn server with a
physical NFC / USB-C key.

You pass the **server's** WebAuthn options JSON, a self-asserted `origin`, and the key's `pin`; the plugin runs
the native ceremony and returns the authenticator's response JSON in the shape a WebAuthn relying party (e.g. a
`go-webauthn` backend) parses — `{id, rawId, type: "public-key", response: {…}}`, every binary field base64url
without padding. The plugin is server-agnostic: it doesn't talk to your backend, it just performs the ceremony.

> Scope: **external security keys only** (the same physical keys you'd enroll on desktop). Platform passkeys and
> device biometrics are a separate concern, not handled here.

> **⚠️ iOS is untested:** the iOS backend is implemented but has **not been tested end-to-end** yet — expect
> rough edges until it's confirmed on a real device. Android is the exercised path.

## Adding it

The plugin lives in the Flugo repo under `plugins/webauthn`. Depend on it from your app's
`frontend/pubspec.yaml`:

```yaml
dependencies:
  webauthn:
    git:
      url: https://github.com/hkdb/flugo.git
      ref: v0.1.13              # pin to the flugo version you build against
      path: plugins/webauthn
```

Standard Flutter plugin — the native side registers automatically.

## Usage

```dart
import 'package:webauthn/webauthn.dart';

// Gate the hardware-key UI on mobile.
if (!await WebauthnPlugin.isAvailable()) return;

// LOGIN: hand the server's assertion options (the login challenge's WebAuthn
// payload) to the OS ceremony, then POST the returned JSON back to your server.
try {
  final assertionJson = await WebauthnPlugin.getAssertion(
    serverOptionsJson,
    origin: 'https://your-cloud.example',   // your RP origin, self-asserted
    pin: keyPin,                             // collected from the user (Android CTAP2 UV)
  );
  await sendToServer(assertionJson);
} on WebauthnException catch (e) {
  if (e.code == 'cancelled') { /* user dismissed */ }
}

// ENROLL: same shape, with the server's creation options.
final attestationJson = await WebauthnPlugin.makeCredential(
  serverCreationOptionsJson,
  origin: 'https://your-cloud.example',
  pin: keyPin,
);
```

## API Reference

### isAvailable

Whether this device can drive an external-security-key ceremony (Android: USB host or NFC; iOS: the
AuthenticationServices security-key API, iOS 16+). Use it to gate the hardware-key UI.

```dart
static Future<bool> isAvailable()
```

### getAssertion

Run a **login** (assertion) ceremony over the server's request options (the full `{publicKey: {…}}` object, or
its `publicKey` contents). Blocks while the OS presents its tap/UV sheet. Returns the assertion response JSON.

```dart
static Future<String> getAssertion(
  String optionsJson, {
  required String origin,
  required String pin,
})
```

### makeCredential

Run an **enroll** (attestation) ceremony over the server's creation options. Returns the attestation response
JSON.

```dart
static Future<String> makeCredential(
  String optionsJson, {
  required String origin,
  required String pin,
})
```

### cancelAssertion

Abort an in-flight `getAssertion` / `makeCredential` (the user dismissed the prompt). Best-effort.

```dart
static Future<void> cancelAssertion()
```

### WebauthnException

`code` is one of `cancelled`, `unsupported_platform`, `no_activity`, `bad_args`, `interrupted`, `unavailable`,
`unknown` (plus any platform code passed through); `message` carries detail.

## Platform backends & notes

| Platform | Native layer | UV (user verification) |
|----------|--------------|------------------------|
| Android  | Play-Services-**free** CTAP2 client over USB/NFC (yubikit protocol layer) — works on de-Googled builds like GrapheneOS | The CTAP2 flow needs the key's **PIN**, so collect it and pass it through |
| iOS      | `AuthenticationServices` `ASAuthorizationSecurityKeyPublicKeyCredentialProvider` (iOS 16+) | iOS runs its own UV sheet and **ignores** the `pin` argument |

- The `origin` you pass is self-asserted (like a desktop authenticator's origin) — it must match what your
  server expects for the ceremony to verify.
- Because the Android backend avoids Google Play Services, security-key login works on devices without Google
  services installed.
