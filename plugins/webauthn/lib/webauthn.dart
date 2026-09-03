// WebAuthn / FIDO2 external-security-key plugin.
//
// Bridges the Flutter app to a native WebAuthn stack so an external security
// key (NFC / USB-C) can complete a cloud-account login (assertion) or enroll a
// new key (attestation) on mobile:
//   - Android: a Play-Services-FREE generic CTAP2 client (yubikit protocol
//     layer over USB/NFC) — works on GrapheneOS with no Google Play Services.
//   - iOS:     AuthenticationServices ASAuthorizationSecurityKeyPublicKeyCredentialProvider
//
// The plugin takes the SERVER's WebAuthn options JSON plus the self-asserted
// `origin` (the cloud base URL, like the desktop libfido2 authenticator) and
// the key's `pin`, and returns the authenticator's response JSON in the shape
// the server (go-webauthn) parses — {id, rawId, type:"public-key",
// response:{...}}, every binary field base64url (no padding). On Android the
// CTAP2 user-verification needs the key's PIN, so the caller collects it and
// passes it through; iOS runs Apple's own UV sheet and ignores the PIN.
//
// Scope: EXTERNAL security keys only (the same physical keys enrolled on
// desktop). Platform passkeys / device biometrics are a separate future factor.

import 'dart:async';

import 'package:flutter/services.dart';

/// Error categories surfaced from the platform layer. `code` is one of:
/// 'cancelled', 'unsupported_platform', 'no_activity', 'bad_args',
/// 'interrupted', 'unknown' (plus any platform-specific code passed through).
class WebauthnException implements Exception {
  final String code;
  final String message;
  WebauthnException(this.code, this.message);
  @override
  String toString() => 'WebauthnException($code): $message';
}

/// Native WebAuthn ceremonies for external security keys.
class WebauthnPlugin {
  WebauthnPlugin._();

  static const _channel = MethodChannel('flugo/webauthn');

  /// Whether this device can drive an external-security-key WebAuthn ceremony
  /// (Android: USB host support or NFC present; iOS: AuthenticationServices
  /// security-key API, iOS 16+). Used to gate the hardware-key UI on mobile.
  static Future<bool> isAvailable() async {
    try {
      final ok = await _channel.invokeMethod<bool>('isAvailable');
      return ok ?? false;
    } on MissingPluginException {
      return false; // native plugin not registered on this platform/build
    } on PlatformException {
      return false;
    }
  }

  /// Runs a LOGIN (assertion) ceremony over the server's request options
  /// (the login challenge's `webauthn` payload — the full `{publicKey:{...}}`
  /// object, or its `publicKey` contents). Blocks while the OS presents its
  /// tap/UV sheet. Returns the assertion response JSON to hand to the backend.
  static Future<String> getAssertion(
    String optionsJson, {
    required String origin,
    required String pin,
  }) async {
    return _invokeCeremony('getAssertion', {'options': optionsJson, 'origin': origin, 'pin': pin});
  }

  /// cancelAssertion aborts an in-flight [getAssertion]/[makeCredential] (the
  /// user dismissed the "present your key" prompt). Best-effort.
  static Future<void> cancelAssertion() async {
    try {
      await _channel.invokeMethod('cancelAssertion');
    } on PlatformException {
      // best-effort
    }
  }

  /// Runs an ENROLL (attestation) ceremony over the server's creation options
  /// (from the register/begin step). Returns the attestation response JSON.
  static Future<String> makeCredential(
    String optionsJson, {
    required String origin,
    required String pin,
  }) async {
    return _invokeCeremony('makeCredential', {'options': optionsJson, 'origin': origin, 'pin': pin});
  }

  /// probeKey (Phase-1 SPIKE, Android): discover a security key over USB/NFC and
  /// read its CTAP2 `getInfo()` with NO Google Play Services — validates the
  /// generic transport before the full ceremony is built. Returns a
  /// human-readable info string; throws [WebauthnException] on failure.
  static Future<String> probeKey() async {
    try {
      final resp = await _channel.invokeMethod<String>('probeKey');
      return resp ?? '(no info)';
    } on PlatformException catch (e) {
      throw WebauthnException(e.code, e.message ?? '');
    }
  }

  /// cancelProbe stops an in-flight [probeKey] (e.g. the user dismissed the
  /// "present your key" prompt). Best-effort.
  static Future<void> cancelProbe() async {
    try {
      await _channel.invokeMethod('cancelProbe');
    } on PlatformException {
      // best-effort
    }
  }

  static Future<String> _invokeCeremony(String method, Map<String, dynamic> args) async {
    try {
      final resp = await _channel.invokeMethod<String>(method, args);
      if (resp == null || resp.isEmpty) {
        throw WebauthnException('unknown', 'platform returned no response');
      }
      return resp;
    } on MissingPluginException {
      throw WebauthnException('unavailable', 'webauthn plugin not available on this device');
    } on PlatformException catch (e) {
      throw WebauthnException(e.code, e.message ?? '');
    }
  }
}
