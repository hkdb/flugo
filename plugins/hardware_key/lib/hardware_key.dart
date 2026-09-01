// Hardware-key challenge-response plugin.
//
// Bridges the Flutter app to the platform's hardware-key transport. On
// Android, wraps Yubico's `com.yubico.yubikit:yubiotp` (NFC + USB-OTG).
// On iOS [B.2], wraps Yubico's `YubiKit` pod (NFC + MFi Lightning/USB-C).
//
// The plugin only handles the transport: given a challenge, return the
// device's 20-byte HMAC-SHA1 response. KEK derivation lives in icfx Go
// code; this plugin is the platform mouth that talks to the YubiKey.
//
// Vendor scope: YubiKey only. NitroKey 3 NFC uses a different applet
// (OATH Secrets-app extension) and will need a separate code path —
// tracked as Phase B.3.

import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/services.dart';

/// Slot identifier passed to the device. YubiKey OTP slot numbering:
/// slot 1 = 0x30 (short-press), slot 2 = 0x38 (long-press). The plugin
/// translates this enum to the platform-specific constant.
enum HardwareKeySlot {
  slot1,
  slot2,
}

/// Result of a successful challenge-response.
class HardwareKeyResponse {
  /// The 20-byte HMAC-SHA1 response from the device.
  final Uint8List response;

  /// Device serial as a string (may be empty if the device or transport
  /// didn't surface one).
  final String serial;

  /// Device family ("yubikey"). Reserved for future vendor expansion
  /// (NitroKey 3).
  final String family;

  const HardwareKeyResponse({
    required this.response,
    required this.serial,
    required this.family,
  });
}

/// Transport that was used to talk to the device.
enum HardwareKeyTransport {
  nfc,
  usb,
}

/// Cancellation/error categories surfaced from the platform layer.
class HardwareKeyException implements Exception {
  /// One of: 'cancelled', 'no_device', 'wrong_slot', 'timeout',
  /// 'unsupported_platform', 'unknown'.
  final String code;
  final String message;
  HardwareKeyException(this.code, this.message);
  @override
  String toString() => 'HardwareKeyException($code): $message';
}

class HardwareKey {
  HardwareKey._();

  static const _channel = MethodChannel('flugo/hardware_key');

  /// Detects whether the device hardware advertises NFC support. Does
  /// not check whether NFC is enabled — that's a separate platform call.
  /// Used by the UI to decide whether to show NFC affordances at all.
  static Future<bool> isNFCAvailable() async {
    try {
      final ok = await _channel.invokeMethod<bool>('isNFCAvailable');
      return ok ?? false;
    } on PlatformException {
      return false;
    }
  }

  /// Detects whether NFC is currently enabled on the device. Android-
  /// specific; iOS always returns true (CoreNFC is on or unavailable).
  static Future<bool> isNFCEnabled() async {
    try {
      final ok = await _channel.invokeMethod<bool>('isNFCEnabled');
      return ok ?? false;
    } on PlatformException {
      return false;
    }
  }

  /// Performs a YubiKey HMAC-SHA1 challenge-response against the given
  /// slot. Blocks until the user taps the key (or plugs it into USB-OTG
  /// on Android / Lightning on iOS), or until the platform times out.
  ///
  /// On Android, the platform layer starts NFC discovery and listens
  /// for both NFC and USB-OTG devices in parallel; whichever arrives
  /// first wins. The caller is expected to have already shown a "tap
  /// your key" UI before invoking this.
  ///
  /// Throws [HardwareKeyException] with code 'cancelled' if the user
  /// dismisses the operation, 'no_device' if no compatible device is
  /// reachable, 'timeout' if the platform's discovery window expires.
  static Future<HardwareKeyResponse> challengeResponse({
    required Uint8List challenge,
    HardwareKeySlot slot = HardwareKeySlot.slot2,
  }) async {
    try {
      final raw = await _channel.invokeMethod<Map<dynamic, dynamic>>(
        'challengeResponse',
        {
          'challenge': challenge,
          'slot': slot == HardwareKeySlot.slot2 ? 2 : 1,
        },
      );
      if (raw == null) {
        throw HardwareKeyException('unknown', 'platform returned null');
      }
      final response = raw['response'];
      if (response is! Uint8List) {
        throw HardwareKeyException('unknown', 'platform returned non-bytes response');
      }
      return HardwareKeyResponse(
        response: response,
        serial: (raw['serial'] as String?) ?? '',
        family: (raw['family'] as String?) ?? 'yubikey',
      );
    } on PlatformException catch (e) {
      throw HardwareKeyException(e.code, e.message ?? '');
    }
  }

  /// Cancels an in-progress challengeResponse. Used when the user
  /// dismisses the "tap your key" UI. Safe to call when no operation
  /// is in flight.
  static Future<void> cancel() async {
    try {
      await _channel.invokeMethod('cancel');
    } on PlatformException {
      // Best-effort.
    }
  }
}
