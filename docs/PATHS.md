# App Base Directory

Flugo provides a universal app base directory via `bridge.BaseDir()`. This is the platform-correct equivalent of `$HOME` for your app -- a single writable directory where you can organize your app's data however you want.

Flutter sets this directory at startup using `path_provider`'s `getApplicationSupportDirectory()`. Your Go backend reads it via `bridge.BaseDir()`. You never need to worry about platform-specific paths.

## Platform Locations

| Platform | Base Directory |
|----------|---------------|
| Linux | `~/.local/share/<app>` (XDG_DATA_HOME) |
| macOS | `~/Library/Application Support/<app>` |
| Windows | `C:\Users\<user>\AppData\Roaming\<app>` |
| Android | `/data/data/<pkg>/files/` (app sandbox) |
| iOS | `<app>/Library/Application Support/` |

## Usage from Go

```go
import "github.com/hkdb/flugo/pkg/bridge"

func (s *MyService) Init() error {
    base := bridge.BaseDir()

    configDir := filepath.Join(base, "config")
    dataDir := filepath.Join(base, "data")
    cacheDir := filepath.Join(base, "cache")

    // Create your directories
    os.MkdirAll(configDir, 0700)
    os.MkdirAll(dataDir, 0700)

    // Store files
    os.WriteFile(filepath.Join(configDir, "settings.json"), data, 0600)
    return nil
}
```

Flugo does not impose any subdirectory structure. You decide how to organize config, data, cache, or anything else within the base directory.

## How It Works

The generated `main.dart` calls `flugoPathService.SetBaseDir` before `runApp()`:

```dart
final appDir = await getApplicationSupportDirectory();
await FlugoBridge.callAsync('flugoPathService.SetBaseDir', [appDir.path]);
```

This happens automatically on all platforms for scaffolded projects. No setup required.

## Overriding from Go

If you need to override the base directory (for testing or custom deployments):

```go
bridge.SetBaseDir("/custom/path")
```

This takes effect immediately for all subsequent `bridge.BaseDir()` calls.

## Temporary Directory

Flugo also provides `bridge.TmpDir()` — a temporary directory inside the app's cache, safe for scratch files. This directory is NOT inside `share_plus`'s cache folder, so files here can be shared via `fileChooserService.shareFile()`.

```go
tmpPath := filepath.Join(bridge.TmpDir(), "output.bin")
os.WriteFile(tmpPath, data, 0600)
```

Set by Flutter at startup from `getTemporaryDirectory()` + `/flugo_tmp`. On all platforms, this is handled automatically by the generated `main.dart`.

## Notes

- `BaseDir()` returns an empty string if called before Flutter sets it (e.g. in unit tests or CLI tools that don't use Flutter). Handle this case in your code if your Go backend is shared with a non-Flutter frontend.
- The directory is created by the OS / `path_provider` -- you don't need to create it yourself. Subdirectories within it are your responsibility.
