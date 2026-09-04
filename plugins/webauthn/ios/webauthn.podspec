# flugo webauthn plugin — iOS pod spec
#
# WebAuthn/FIDO2 external security keys via AuthenticationServices
# (ASAuthorizationSecurityKeyPublicKeyCredentialProvider). The app must also
# declare an `webcredentials:<RP_ID>` associated domain (handled by flugo's
# iOS entitlement patch) and the RP must host apple-app-site-association.

Pod::Spec.new do |s|
  s.name             = 'webauthn'
  s.version          = '0.1.0'
  s.summary          = 'WebAuthn/FIDO2 external security keys (Credential Manager / AuthenticationServices).'
  s.description      = 'flugo plugin for external-security-key WebAuthn login + enrollment on mobile.'
  s.homepage         = 'https://example.com'
  s.license          = { :file => '../LICENSE' }
  s.author           = { 'flugo' => 'noreply@example.com' }
  s.source           = { :path => '.' }
  s.source_files     = 'Classes/**/*'
  s.dependency 'Flutter'
  s.frameworks       = 'AuthenticationServices'
  s.platform = :ios, '16.0'
  s.swift_version = '5.0'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
