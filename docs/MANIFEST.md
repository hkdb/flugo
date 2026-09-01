# Platform Display Name

`flugo create` scaffolds platform runners via `flutter create`, which sets the app display name to the project's dart-safe name (e.g. `ic_app`). To show a proper display name (e.g. "Instacrypt") on home screens, app lists, and window titles, update the platform manifest files below.

## Android

**File**: `frontend/android/app/src/main/AndroidManifest.xml`

Change `android:label`:

```xml
<application
    android:label="YourAppName"
    ...>
```

## iOS

**File**: `frontend/ios/Runner/Info.plist`

Change `CFBundleDisplayName` and `CFBundleName`:

```xml
<key>CFBundleDisplayName</key>
<string>YourAppName</string>
...
<key>CFBundleName</key>
<string>YourAppName</string>
```

## macOS

**File**: `frontend/macos/Runner/Configs/AppInfo.xcconfig`

Change `PRODUCT_NAME`:

```
PRODUCT_NAME = YourAppName
```

## Windows

**File**: `frontend/windows/runner/main.cpp`

Find the `CreateAndShow` call and change the window title:

```cpp
if (!window.CreateAndShow(L"YourAppName", origin, size)) {
```

## Linux

Linux uses the `.desktop` file for the display name. It is generated from `app.name` in `flugo.yaml` **when the project is scaffolded**:

```yaml
app:
  name: YourAppName
```

The `.desktop` file at `assets/linux/{AppID}.desktop` gets its `Name=` from this value at `flugo create` time. Changing `app.name` afterward does **not** regenerate the `.desktop` file — edit `assets/linux/{AppID}.desktop` directly. The window title is set in `app_dart.tmpl` via `MaterialApp(title: ...)`.

## macOS Sandbox

By default, flugo scaffolds the macOS app with the sandbox **disabled** (`com.apple.security.app-sandbox = false`), so the app can read and write files freely, like a regular desktop app.

To enable the sandbox (required for Mac App Store distribution), set `sandbox: true` in `flugo.yaml`:

```yaml
platforms:
  macos:
    sandbox: true
```

`flugo build macos` and `flugo run macos` apply this value to both entitlements files (`Release.entitlements` and `DebugProfile.entitlements`) before building. When the sandbox is enabled, the app can only write to files the user explicitly selects via a file dialog. Use the [save dialog flow](FILESYSTEM.md#writing-output-files) for output files, same as on mobile.

## Platform Permissions

### Auto-scaffolded by flugo

`flugo create` automatically patches these permissions after `flutter create`:

- **macOS**: `files.user-selected.read-write` (file picker dialogs) and `network.client` (outbound network)
- **iOS**: `NSPhotoLibraryUsageDescription` (file selection)

### App-specific permissions (add manually if needed)

**Camera access** (e.g. for QR code scanning):

Android — add to `frontend/android/app/src/main/AndroidManifest.xml`:
```xml
<manifest ...>
    <uses-permission android:name="android.permission.CAMERA"/>
    <application ...>
```

iOS — add to `frontend/ios/Runner/Info.plist`:
```xml
<key>NSCameraUsageDescription</key>
<string>Camera access is needed to scan QR codes</string>
```

**Microphone access** (e.g. for voice recording):

Android:
```xml
<uses-permission android:name="android.permission.RECORD_AUDIO"/>
```

iOS:
```xml
<key>NSMicrophoneUsageDescription</key>
<string>Microphone access is needed for voice recording</string>
```

**Location access**:

Android:
```xml
<uses-permission android:name="android.permission.ACCESS_FINE_LOCATION"/>
```

iOS:
```xml
<key>NSLocationWhenInUseUsageDescription</key>
<string>Location access is needed for...</string>
```
