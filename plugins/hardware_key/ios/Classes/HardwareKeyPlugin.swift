// flugo hardware_key — iOS plugin.
//
// B.1 stub: returns 'unsupported_platform' for every challengeResponse
// call so the Flutter app can detect "iOS HW key support not built yet"
// gracefully. B.2 wires this to YubiKit:
//
//   YKFManager.shared.nfcSession.start(...)
//   YKFChallengeResponseSession.sendChallenge(...)
//
// See flugo/plugins/hardware_key/ios/hardware_key.podspec for the
// dependency that B.2 will add.

import Flutter
import UIKit

public class HardwareKeyPlugin: NSObject, FlutterPlugin {
    public static func register(with registrar: FlutterPluginRegistrar) {
        let channel = FlutterMethodChannel(
            name: "flugo/hardware_key",
            binaryMessenger: registrar.messenger()
        )
        let instance = HardwareKeyPlugin()
        registrar.addMethodCallDelegate(instance, channel: channel)
    }

    public func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
        switch call.method {
        case "isNFCAvailable":
            // NFCNDEFReaderSession.readingAvailable is the closest proxy.
            // B.2 will refine this with YKFManager.shared.nfcSession's
            // own capability check.
            result(true)
        case "isNFCEnabled":
            // iOS doesn't have an enable/disable switch; NFC is always
            // "available" when the hardware supports it.
            result(true)
        case "challengeResponse":
            result(FlutterError(
                code: "unsupported_platform",
                message: "iOS hardware key support is not yet built (Phase B.2 pending). Use the desktop app for HW-protected identities.",
                details: nil
            ))
        case "cancel":
            result(nil)
        default:
            result(FlutterMethodNotImplemented)
        }
    }
}
