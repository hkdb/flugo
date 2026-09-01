# File Chooser Service

Flugo generates a cross-platform `FileChooserService` that works on Linux, macOS, Windows, Android, and iOS. On Linux, it uses XDG Desktop Portal via the Go backend (Flatpak-compatible). On macOS, Windows, Android, and iOS, it uses the `file_picker`, `url_launcher`, `share_plus`, and `open_file_manager` Flutter plugins.

The service is available as a global instance:

```dart
import 'bridge/filechooser.dart';

final path = await fileChooserService.pickFile('Select a file');
```

## API Reference

### pickFile

Pick a single file. Returns the chosen path, or `null` if cancelled.

```dart
Future<String?> pickFile(String title)
```

### pickFiles

Pick multiple files. Returns a list of paths, or `null` if cancelled.

```dart
Future<List<String>?> pickFiles(String title)
```

### pickDirectory

Pick a directory. Returns the chosen path, or `null` if cancelled.

```dart
Future<String?> pickDirectory(String title)
```

### saveFile

Show a save file dialog. Returns a `SaveFileResult` with `tempPath` (for sharing), `savedPath` (for display), and `cancelled`/`error` status.

```dart
Future<SaveFileResult> saveFile(
  String title,
  String suggestedName,
  String directory, {
  Uint8List? bytes,
  String? targetPath,
})
```

When `bytes` is provided, the file is written automatically on all platforms. On Android and iOS, `bytes` is **required** by the platform -- omitting it will fail on mobile.

When `targetPath` is provided, flugo handles the platform differences automatically:

| Platform | targetPath behavior |
|----------|-------------------|
| Linux (native) | Write directly to `targetPath`, no dialog |
| Linux (Flatpak) | Ignore `targetPath`, show portal dialog (sandbox requires it) |
| macOS / Windows | Write directly to `targetPath`, no dialog |
| Android / iOS | Write to `targetPath` (app sandbox), then show SAF save dialog |

This is useful for operations where you know where the output should go (e.g., writing encrypted output next to the input file). On desktop, the file is written silently. On mobile, the user gets a save dialog because mobile apps are sandboxed.

**Basic save dialog (all platforms):**

```dart
final data = utf8.encode('Hello, world!');
final path = await fileChooserService.saveFile(
  'Save File',
  'output.txt',
  '',
  bytes: Uint8List.fromList(data),
);
```

**Silent write with Flatpak + mobile safety:**

```dart
// Desktop: writes directly, no dialog
// Flatpak: shows portal dialog
// Mobile: writes to sandbox, shows SAF dialog
final path = await fileChooserService.saveFile(
  'Save Output',
  'output.icfx',
  '',
  bytes: encryptedBytes,
  targetPath: '${inputPath}.icfx',
);
```

### saveFiles

Show a save dialog for multiple files. Returns the chosen paths, or `null` if cancelled. Internally calls `saveFile` for each file.

```dart
Future<List<String>?> saveFiles(
  String title,
  List<String> filenames,
  String directory,
)
```

### openFile

Open a file with the default application. Works on all platforms — desktop via native openers, and Android/iOS via the system default handler (`url_launcher`). Throws if the file can't be opened.

```dart
Future<void> openFile(String filePath)
```

### openDirectory

Open the containing folder of a file in the system file manager. Works on all platforms — Linux/macOS/Windows open the desktop file manager, Android opens the system DocumentsUI positioned at the folder via the `open_file_manager` plugin (`ACTION_OPEN_DOCUMENT` + `INITIAL_URI`; positioning needs API 26+, degrades to unpositioned below), and iOS opens the Files app via a `shareddocuments://` URL. Throws if the folder can't be opened.

```dart
Future<void> openDirectory(String filePath)
```

### openUrl

Open a URL in the default browser (or the app associated with the scheme). Returns `true` on success. On Linux it uses the XDG Desktop Portal via the Go backend; on other platforms it uses `url_launcher`.

```dart
Future<bool> openUrl(String url)
```

### shareFile

Share a file via the platform's native share sheet. Works on Android and iOS. No-op on Linux, macOS, and Windows — use `openDirectory` instead.

```dart
Future<void> shareFile(String filePath)
```

### isDocPortalPath

Check if a path is on the XDG document portal FUSE mount. Always returns `false` on non-Linux platforms.

```dart
Future<bool> isDocPortalPath(String path)
```

## Platform Behavior

| Method | Linux | macOS | Windows | Android | iOS |
|--------|-------|-------|---------|---------|-----|
| pickFile | XDG Portal (Go) | file_picker | file_picker | file_picker | file_picker |
| pickFiles | XDG Portal (Go) | file_picker | file_picker | file_picker | file_picker |
| pickDirectory | XDG Portal (Go) | file_picker | file_picker | file_picker | file_picker |
| saveFile | XDG Portal (Go) + write | file_picker + write | file_picker + write | file_picker (SAF) | file_picker (SAF) |
| openFile | XDG Portal (Go) | url_launcher | url_launcher | url_launcher | url_launcher |
| openDirectory | XDG Portal (Go) | url_launcher | url_launcher | open_file_manager | url_launcher |
| openUrl | XDG Portal (Go) | url_launcher | url_launcher | url_launcher | url_launcher |
| shareFile | No-op | No-op | No-op | share_plus | share_plus |

## Writing Output Files

Flugo provides two functions for writing output files. Both return `SaveFileResult` and handle all platform differences (native desktop, Flatpak, mobile) automatically. The developer never writes platform checks.

### Which function to use

| Function | Who decides the path | When to use |
|----------|---------------------|-------------|
| **`WriteFile`** (Go) + **`handleWriteResult`** (Dart) | **Your code** decides the path | The output has a natural location derived from the input — e.g., compressing `report.pdf` → `report.pdf.gz`, converting `image.png` → `image.jpg`. The user doesn't need to choose. |
| **`saveFile`** (Dart) | **The user** picks the path via a dialog | The output has no natural location — e.g., exporting a backup, generating a report, saving a download. The user decides where it goes. |

**Why two functions?** Some operations produce output that logically belongs next to the input file (compressing, converting, processing). Others produce standalone output with no obvious destination (backups, exports, generated reports). The first case should be silent on desktop — don't interrupt the user with a dialog when the path is obvious. The second must always ask. Both need to work on Flatpak (portal) and mobile (SAF) without the developer thinking about it.

Both return `SaveFileResult` → both work with the same success dialog pattern (Share on mobile, Open Folder on desktop, cleanupTemp on OK).

---

### `SaveFileResult`

Both `handleWriteResult` and `saveFile` return this. It has everything you need for the success dialog:

```dart
class SaveFileResult {
  final String? tempPath;   // real filesystem path — use for shareFile() on mobile
  final String? savedPath;  // final destination — use for display and openDirectory()
  final bool cancelled;     // user cancelled the dialog (Flatpak/mobile only)
  final bool exists;        // file already exists (native desktop, WriteFile with force=false)
  final String? error;      // error message if something failed
  bool get ok => ...;       // convenience: not cancelled, not exists, no error
}
```

### `cleanupTemp`

Deletes the temp directory used for sharing. Call on the OK button — **never** immediately after `shareFile()` (Android's share sheet returns before the receiving app finishes reading).

```dart
fileChooserService.cleanupTemp(result);
```

On native desktop this is a no-op. On Flatpak/mobile it deletes the unique temp subdirectory (`flugo_tmp/<id>/`).

---

### Option 1: `WriteFile` — code-specified path

Use when your code knows where the file should go.

**Go side:**

```go
import "github.com/hkdb/flugo/pkg/filechooser"

// Your code decides the output path based on the input
targetPath := inputPath + ".compressed"
result, err := filechooser.WriteFile(targetPath, data, force)

// Return JSON for Flutter
resultJSON, _ := json.Marshal(result)
return string(resultJSON), nil
```

`WriteFile` returns a `WriteResult`:

```go
type WriteResult struct {
    Path   string `json:"path"`
    Env    string `json:"env"`              // "native", "flatpak", or "mobile"
    Exists bool   `json:"exists,omitempty"` // file exists and force=false (native only)
}
```

| Environment | What happens |
|-------------|-------------|
| Native desktop | Writes directly to `targetPath`. If file exists and `force=false`, returns `Exists: true` without writing. |
| Flatpak | Writes to a unique temp subdirectory. |
| Mobile | Writes to a unique temp subdirectory. |

**Flutter side:**

```dart
final resultJSON = await myService.processFile(inputPath, force);

final result = await fileChooserService.handleWriteResult(resultJSON, 'output.bin');

// Handle overwrite (native desktop only)
if (result.exists) {
  final confirmed = await _showOverwriteDialog(result.savedPath!);
  if (!confirmed) return;
  await _processFile(inputPath, force: true);
  return;
}

if (result.cancelled) return;
if (result.error != null) { showError(result.error!); return; }

_showSuccessDialog('Done', result);
```

`handleWriteResult` does nothing on native desktop (file is already written). On Flatpak it shows a portal dialog. On mobile it shows a SAF dialog. The developer doesn't check.

---

### Option 2: `saveFile` — user-specified path

Use when the user should choose where to save (exports, backups, reports).

```dart
final result = await fileChooserService.saveFile(
  'Export Profile',
  'backup.tar.icfx',
  '',
  bytes: Uint8List.fromList(data),
);

if (result.cancelled) return;
_showSuccessDialog('Exported', result);
```

On all platforms, a save dialog is shown. On mobile, a temp file is automatically created for `shareFile()` to work. `cleanupTemp` on OK deletes it.

---

### Success dialog pattern

Both functions return `SaveFileResult`, so the success dialog is the same:

```dart
void _showSuccessDialog(String message, SaveFileResult result) {
  showDialog(
    context: context,
    builder: (ctx) => AlertDialog(
      title: const Text('Success'),
      content: Text(message),
      actions: [
        if (Platform.isAndroid || Platform.isIOS)
          OutlinedButton(
            onPressed: () => fileChooserService.shareFile(result.tempPath!),
            child: const Text('Share'),
          ),
        if (!Platform.isAndroid && !Platform.isIOS)
          OutlinedButton(
            onPressed: () => fileChooserService.openDirectory(result.savedPath!),
            child: const Text('Open Folder'),
          ),
        FilledButton(
          onPressed: () {
            fileChooserService.cleanupTemp(result);
            Navigator.of(ctx).pop();
          },
          child: const Text('OK'),
        ),
      ],
    ),
  );
}
```

### Important notes

- **Always call `cleanupTemp(result)` on OK dismiss.** No-op on native desktop. Deletes temp files on Flatpak/mobile.
- **Do NOT call `cleanupTemp` after `shareFile()`** — Android's share sheet returns before the receiving app finishes reading.
- **Show Share only on mobile, Open Folder only on desktop.** Both are no-ops on the other platform, but dead buttons confuse users.
- **`tempPath` is always a real filesystem path** — never a SAF URI. Safe for `shareFile()`.

## Dependencies

These are included automatically in scaffolded projects:

- `file_picker` -- file/directory dialogs on macOS, Windows, Android, and iOS
- `url_launcher` -- opening files, directories, and URLs on macOS, Windows, Android, and iOS
- `share_plus` -- file sharing on Android and iOS
- `open_file_manager` -- opening a file's containing folder (DocumentsUI) on Android
