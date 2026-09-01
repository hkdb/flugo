# flugo hardware_key plugin — iOS pod spec
#
# B.1 ships an empty Swift stub (everything returns 'unsupported_platform').
# B.2 fills in the YubiKit wiring and adds:
#
#   s.dependency 'YubiKit', '~> 4.7.0'

Pod::Spec.new do |s|
  s.name             = 'hardware_key'
  s.version          = '0.1.0'
  s.summary          = 'Hardware-key HMAC-SHA1 challenge-response (YubiKey, NFC + MFi).'
  s.description      = 'flugo plugin for YubiKey HMAC-SHA1 challenge-response over NFC and MFi Lightning/USB-C. iOS implementation pending (B.2).'
  s.homepage         = 'https://example.com'
  s.license          = { :file => '../LICENSE' }
  s.author           = { 'flugo' => 'noreply@example.com' }
  s.source           = { :path => '.' }
  s.source_files     = 'Classes/**/*'
  s.dependency 'Flutter'
  s.platform = :ios, '13.0'
  s.swift_version = '5.0'
  s.pod_target_xcconfig = { 'DEFINES_MODULE' => 'YES' }
end
