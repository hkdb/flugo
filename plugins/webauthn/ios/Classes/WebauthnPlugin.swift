// flugo webauthn — iOS plugin.
//
// Bridges a Flutter MethodChannel to AuthenticationServices for WebAuthn/FIDO2
// EXTERNAL security keys (NFC / USB-C / Lightning) via
// ASAuthorizationSecurityKeyPublicKeyCredentialProvider. Unlike Android's
// CredentialManager (which hands back spec-shaped response JSON), iOS returns
// raw Data fields, so we assemble the response JSON by hand — base64url, no
// padding — to match what the server (go-webauthn) parses and what the desktop
// libfido2 authenticator emits.
//
// Requires the app to declare `webcredentials:<RP_ID>` in Associated Domains
// and the RP to host apple-app-site-association; without that the OS refuses to
// release a credential for the RP ID.

import AuthenticationServices
import Flutter
import UIKit

public class WebauthnPlugin: NSObject, FlutterPlugin {
    // Retains the in-flight ceremony (delegate + presentation provider) until it
    // completes; ASAuthorizationController does not keep its delegate alive.
    private var activeCeremony: AnyObject?

    public static func register(with registrar: FlutterPluginRegistrar) {
        let channel = FlutterMethodChannel(name: "flugo/webauthn", binaryMessenger: registrar.messenger())
        registrar.addMethodCallDelegate(WebauthnPlugin(), channel: channel)
    }

    public func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
        switch call.method {
        case "isAvailable":
            if #available(iOS 16.0, *) { result(true) } else { result(false) }
        case "getAssertion":
            runCeremony(call, result: result, register: false)
        case "makeCredential":
            runCeremony(call, result: result, register: true)
        default:
            result(FlutterMethodNotImplemented)
        }
    }

    private func runCeremony(_ call: FlutterMethodCall, result: @escaping FlutterResult, register: Bool) {
        guard #available(iOS 16.0, *) else {
            result(FlutterError(code: "unsupported_platform", message: "requires iOS 16+", details: nil))
            return
        }
        guard
            let args = call.arguments as? [String: Any],
            let optionsJSON = args["options"] as? String,
            let pub = WebauthnPlugin.publicKey(from: optionsJSON)
        else {
            result(FlutterError(code: "bad_args", message: "missing or invalid options", details: nil))
            return
        }

        let request: ASAuthorizationRequest
        do {
            request = register ? try WebauthnPlugin.registrationRequest(pub)
                               : try WebauthnPlugin.assertionRequest(pub)
        } catch {
            result(FlutterError(code: "bad_args", message: "\(error)", details: nil))
            return
        }

        let ceremony = Ceremony(register: register) { [weak self] outcome in
            self?.activeCeremony = nil
            switch outcome {
            case .success(let json): result(json)
            case .failure(let err): result(err)
            }
        }
        activeCeremony = ceremony
        let controller = ASAuthorizationController(authorizationRequests: [request])
        controller.delegate = ceremony
        controller.presentationContextProvider = ceremony
        controller.performRequests()
    }

    // MARK: - options parsing

    // publicKey returns the WebAuthn `publicKey` object; the server marshals its
    // options as {"publicKey":{...}}. Falls back to the top-level object.
    private static func publicKey(from optionsJSON: String) -> [String: Any]? {
        guard
            let data = optionsJSON.data(using: .utf8),
            let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return nil }
        if let inner = obj["publicKey"] as? [String: Any] { return inner }
        return obj
    }

    @available(iOS 16.0, *)
    private static func assertionRequest(_ pub: [String: Any]) throws -> ASAuthorizationRequest {
        guard
            let rpId = pub["rpId"] as? String,
            let challengeB64 = pub["challenge"] as? String,
            let challenge = Data(base64URLEncoded: challengeB64)
        else { throw CeremonyError.badOptions }

        let provider = ASAuthorizationSecurityKeyPublicKeyCredentialProvider(relyingPartyIdentifier: rpId)
        let req = provider.createCredentialAssertionRequest(challenge: challenge)
        if let allow = pub["allowCredentials"] as? [[String: Any]] {
            req.allowedCredentials = allow.compactMap { entry in
                guard let idB64 = entry["id"] as? String, let id = Data(base64URLEncoded: idB64) else { return nil }
                return ASAuthorizationSecurityKeyPublicKeyCredentialDescriptor(
                    credentialID: id,
                    transports: ASAuthorizationSecurityKeyPublicKeyCredentialDescriptor.Transport.allSupported
                )
            }
        }
        req.userVerificationPreference = .required
        return req
    }

    @available(iOS 16.0, *)
    private static func registrationRequest(_ pub: [String: Any]) throws -> ASAuthorizationRequest {
        let rp = pub["rp"] as? [String: Any]
        let user = pub["user"] as? [String: Any]
        guard
            let rpId = rp?["id"] as? String,
            let challengeB64 = pub["challenge"] as? String,
            let challenge = Data(base64URLEncoded: challengeB64),
            let userName = user?["name"] as? String,
            let userIdB64 = user?["id"] as? String,
            let userId = Data(base64URLEncoded: userIdB64)
        else { throw CeremonyError.badOptions }
        let displayName = (user?["displayName"] as? String) ?? userName

        let provider = ASAuthorizationSecurityKeyPublicKeyCredentialProvider(relyingPartyIdentifier: rpId)
        let req = provider.createCredentialRegistrationRequest(
            challenge: challenge,
            displayName: displayName,
            name: userName,
            userID: userId
        )
        req.credentialParameters = [ASAuthorizationPublicKeyCredentialParameters(algorithm: .ES256)]
        req.userVerificationPreference = .required
        return req
    }
}

private enum CeremonyError: Error { case badOptions }

private enum CeremonyOutcome {
    case success(String)
    case failure(FlutterError)
}

// Ceremony is the ASAuthorizationController delegate + presentation provider for
// one ceremony. It assembles the spec-shaped, base64url-no-pad response JSON
// from the raw Data the OS returns.
@available(iOS 16.0, *)
private class Ceremony: NSObject, ASAuthorizationControllerDelegate, ASAuthorizationControllerPresentationContextProviding {
    private let register: Bool
    private let completion: (CeremonyOutcome) -> Void

    init(register: Bool, completion: @escaping (CeremonyOutcome) -> Void) {
        self.register = register
        self.completion = completion
    }

    func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let window = scenes.flatMap { $0.windows }.first { $0.isKeyWindow }
        return window ?? ASPresentationAnchor()
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithAuthorization authorization: ASAuthorization) {
        if register, let reg = authorization.credential as? ASAuthorizationSecurityKeyPublicKeyCredentialRegistration {
            let json: [String: Any] = [
                "id": reg.credentialID.base64URLEncodedString(),
                "rawId": reg.credentialID.base64URLEncodedString(),
                "type": "public-key",
                "response": [
                    "clientDataJSON": reg.rawClientDataJSON.base64URLEncodedString(),
                    "attestationObject": (reg.rawAttestationObject ?? Data()).base64URLEncodedString(),
                ],
            ]
            emit(json)
            return
        }
        if !register, let asrt = authorization.credential as? ASAuthorizationSecurityKeyPublicKeyCredentialAssertion {
            var response: [String: Any] = [
                "clientDataJSON": asrt.rawClientDataJSON.base64URLEncodedString(),
                "authenticatorData": asrt.rawAuthenticatorData.base64URLEncodedString(),
                "signature": asrt.signature.base64URLEncodedString(),
            ]
            if !asrt.userID.isEmpty {
                response["userHandle"] = asrt.userID.base64URLEncodedString()
            }
            let json: [String: Any] = [
                "id": asrt.credentialID.base64URLEncodedString(),
                "rawId": asrt.credentialID.base64URLEncodedString(),
                "type": "public-key",
                "response": response,
            ]
            emit(json)
            return
        }
        completion(.failure(FlutterError(code: "unknown", message: "unexpected credential type", details: nil)))
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: Error) {
        if let asErr = error as? ASAuthorizationError, asErr.code == .canceled {
            completion(.failure(FlutterError(code: "cancelled", message: "cancelled", details: nil)))
            return
        }
        completion(.failure(FlutterError(code: "unknown", message: error.localizedDescription, details: nil)))
    }

    private func emit(_ json: [String: Any]) {
        guard
            let data = try? JSONSerialization.data(withJSONObject: json),
            let str = String(data: data, encoding: .utf8)
        else {
            completion(.failure(FlutterError(code: "unknown", message: "failed to encode response", details: nil)))
            return
        }
        completion(.success(str))
    }
}

private extension Data {
    // base64url without padding — every binary field on the WebAuthn wire.
    func base64URLEncodedString() -> String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    init?(base64URLEncoded s: String) {
        var b64 = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        let pad = b64.count % 4
        if pad != 0 { b64 += String(repeating: "=", count: 4 - pad) }
        self.init(base64Encoded: b64)
    }
}

@available(iOS 16.0, *)
private extension ASAuthorizationSecurityKeyPublicKeyCredentialDescriptor.Transport {
    static var allSupported: [ASAuthorizationSecurityKeyPublicKeyCredentialDescriptor.Transport] {
        [.usb, .nfc, .bluetooth, .lightning]
    }
}
