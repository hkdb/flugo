# Flugo

![logo](assets/icon.png)

**Go + Flutter app framework** — write your backend in Go, your UI in Flutter, and let Flugo bridge them via FFI.

Flugo generates type-safe Dart wrappers from your Go structs automatically. No manual FFI plumbing, no code-sharing hacks, no REST servers. Just `bridge.Bind()` your Go types and call them from Dart.

**Design principles:** convention over configuration, zero boilerplate FFI, cross-platform from day one.

Flugo was created to leverage the simplicity and performance of Go and the near native UI performance of Flutter to build apps ready for all major platforms (Windows, macOS, Linux, Android, iOS) in the market in a single code base. It was originally created as a foundational piece in delivering the [Instacrypt App](https://github.com/instacryptio/ic-app) for the [Instacrypt Project](https://instacrypt.io).


This project is in very early stage and currently being actively developed. If you find any issues, submit an issue in this repo.

If you find this useful, give us a star or buy us a coffee (Mention Flugo in the "Say something nice..." field):

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/yellow_img.png)](https://www.buymeacoffee.com/3dfosi)


## Architecture

```
┌──────────────────┐     JSON/FFI      ┌──────────────────┐
│   Flutter UI     │ ◄──────────────►  │   Go Backend     │
│  (Dart)          │   dart:ffi        │  (C-shared lib)  │
│                  │                   │                  │
│  bridge.gen.dart │   FlugoCall()     │  bridge.Bind()   │
│  (generated)     │ ──────────────►   │  (reflection)    │
└──────────────────┘                   └──────────────────┘
```

Method calls are serialized as JSON, dispatched over a C FFI boundary (`FlugoCall`), and routed to Go methods via reflection. Large binary payloads use a separate byte-buffer channel (`FlugoCallBytes` / `FlugoGetBytes`). Sensitive intake (passphrases, key material) uses a third channel — `FlugoCallSecure` — that bypasses JSON entirely and seals bytes into a `memguard` enclave on the Go side; see "Secure Intake" below. For the reverse direction — Go pushing a sequence of values to Dart in real time — a method can return a `bridge.Emitter[T]`, generated as a Dart `Stream<T>` over the Dart Native API (in-process, no sockets); see [docs/STREAMING.md](docs/STREAMING.md).

## Why

Go is one of the best cross-platform languages to develop application logic but it just doesn't have a modern UI framework that does what Flutter can do. Flugo is my way of putting together the best of two worlds and making them one.

This is not supposed to be some hot, up and coming framework but rather just a very practical way to quickly build desktop and mobile apps in a single code base.

Shoutout to [Wails](https://wails.io) for inspiring me to make many parts of Flugo the way I did.

## Prerequisites

**Required:**

| Tool | Version |
|------|---------|
| Go | >= 1.26.6 |
| Flutter SDK | >= 3.41 (Dart >= 3.11) |
| GCC | Any recent version |
| pkg-config | Any |
| lld | Matching your LLVM version (e.g. `lld-18`) |

**Optional:**

| Tool | Purpose |
|------|---------|
| rsvg-convert or ImageMagick | Icon generation from SVG |
| flatpak-builder | Linux Flatpak packaging |
| appstreamcli | AppStream metadata validation |
| Android NDK | Android builds (auto-installed if missing) |
| Xcode | iOS builds (macOS only) |
| MinGW-w64 (`x86_64-w64-mingw32-gcc`) | Windows cross-compilation |

Run `flugo doctor` to check your environment, or `flugo doctor --full` to also run `flutter doctor`.

## Installation

```bash
go install github.com/hkdb/flugo/cmd/flugo@latest
```

## Quick Start

```bash
# Scaffold a new project
flugo create myapp --module github.com/yourname/myapp

# Enter the project
cd myapp

# Generate bridge code from Go structs
flugo generate

# Build and run on your current platform
flugo run
```

## Binding Go Services

### 1. Write a Go struct with public methods

```go
// backend/service.go
package main

import "fmt"

type GreeterService struct{}

func (s *GreeterService) Greet(name string) (string, error) {
    return fmt.Sprintf("Hello, %s!", name), nil
}

func (s *GreeterService) Add(a, b int) (int, error) {
    return a + b, nil
}
```

### 2. Register it with the bridge

```go
// backend/main.go
package main

import "github.com/hkdb/flugo/pkg/bridge"

func init() {
    bridge.Bind(&GreeterService{})
}

func main() {}
```

### 3. List the service file in flugo.yaml

```yaml
backend:
  module: github.com/yourname/myapp
  services:
    - service.go
```

### 4. Generate and use from Dart

```bash
flugo generate
```

This produces `frontend/lib/bridge/bridge.gen.dart` with a typed `GreeterService` class and a top-level `greeterService` instance:

```dart
// Auto-generated — call your Go methods directly
final result = await greeterService.greet("World");
// result == "Hello, World!"

final sum = await greeterService.add(2, 3);
// sum == 5
```

Errors returned from Go are thrown as `FlugoException` in Dart. The exception's `toString()` returns only the error message (no class name prefix), so it's safe to display directly to end users:

```dart
try {
  await myService.doSomething();
} on FlugoException catch (e) {
  // e.toString() returns the clean Go error message, e.g. "identity not found"
  // e.message is the same string
  showError(e.toString());
}
```

## Secure Intake (Sensitive Material)

The standard JSON path is fine for nearly everything but leaks plaintext when you pass sensitive bytes — passphrases, raw key material, anything you'd want to wipe from memory the moment you're done with it. Dart Strings and Go strings are immutable and GC-managed; once a passphrase is materialized as a String it stays in the heap until the runtime decides to reclaim the page (and may show up in swap, hibernation images, or heap dumps in the meantime).

Flugo's secure-intake channel solves this. Bind a method that takes one `*bridge.Secret` parameter:

```go
package main

import "github.com/hkdb/flugo/pkg/bridge"

type MyService struct{}

func (s *MyService) Unlock(secret *bridge.Secret) error {
    defer secret.Destroy()

    buf, err := secret.Open()
    if err != nil {
        return err
    }
    defer buf.Destroy()

    // Use buf.Bytes() in the smallest possible scope. Don't store
    // the bytes (or a string copy of them) outside this function.
    return s.validate(buf.Bytes())
}

func main() {
    bridge.Bind(&MyService{})
}
```

`flugo generate` detects the `*bridge.Secret` parameter and emits a Dart wrapper that takes a `Uint8List` and routes through `FlugoCallSecure` — bytes never get JSON-encoded, never become a Go string in the bridge layer, and the Go side seals them into a `memguard` enclave on intake. The recommended caller wipes the input after dispatch:

```dart
final secret = Uint8List.fromList(utf8.encode(controller.text));
try {
  await myService.unlock(secret);
} finally {
  secret.fillRange(0, secret.length, 0);
}
```

Methods using the secure path must take **exactly one** user-visible parameter, of type `*bridge.Secret` — `flugo generate` fails with a clear error on any signature that mixes a Secret with other parameters. Multi-arg flows (e.g. an unlock that needs both an identity name and a passphrase) split into two calls: stage the secret via a lone-Secret method, then consume it from the method carrying the other arguments (see docs/SECRETS.md).

A bound method may return **at most one non-error value**, optionally followed by an `error` (i.e. `T`, `(T, error)`, `error`, or nothing) — `flugo generate` fails with a clear error on more than one non-error return. Return a struct when you need to send multiple values.

For the threat model, the wire-level flow, codegen behavior, and pitfalls, see [docs/SECRETS.md](docs/SECRETS.md).

## CLI Commands

| Command | Description |
|---------|-------------|
| `flugo create <name>` | Scaffold a new project (`-m`/`--module`, `--flugo-path`) |
| `flugo generate` | Regenerate bridge code from bound Go structs (`--regen-platforms`) |
| `flugo build <platform>` | Build for linux/macos/windows/android/ios/appimage/flatpak/all (`--release`) |
| `flugo run [platform]` | Build and run (default: current desktop; or `android`/`ios`) |
| `flugo update` | Refresh flugo-managed files from the current templates — framework files incl. the Android `MainActivity` (`--all`, `--dry-run`) |
| `flugo upgrade [ref]` | Self-update the CLI binary (`--flugo-path` for local builds) |
| `flugo appimage` | Scaffold AppImage packaging assets |
| `flugo flathub` | Generate Flathub submission assets |
| `flugo icons` | Generate platform icons from source SVG (default Flugo logo provided; replace `assets/icons/icon.svg` with your own) |
| `flugo doctor` | Check environment for required tools (`--full` to include flutter doctor) |
| `flugo lint [target]` | Run Go and/or Dart linters (`backend`, `frontend`, or both; `--fix` to apply auto-fixes) |
| `flugo clean` | Clean build artifacts |
| `flugo version` | Print Flugo version |

## flugo.yaml Configuration

```yaml
flugo_version: 0.1.0                  # CLI version that created or last updated this project

app:
  id: com.example.myapp              # Reverse-domain app ID
  name: MyApp                        # Display name
  version: "0.1.0"                   # App version
  description: "A Flugo application" # Short description
  author: ""                         # Author name
  license: ""                        # License identifier
  url: ""                            # Homepage URL
  url_scheme: ""                     # Custom URL scheme for deep links (optional)
  window:
    width: 1280                        # Default window width
    height: 720                        # Default window height
    min_width: 360                     # Minimum window width
    min_height: 480                    # Minimum window height
    titlebar_style: default            # "native", "default", "clean", or "custom"

backend:
  module: github.com/example/myapp   # Go module path
  services:                          # Go files containing bound types
    - service.go

platforms:
  linux:
    flatpak:
      runtime: org.freedesktop.Platform
      runtime_version: "24.08"
      sdk: org.freedesktop.Sdk
      permissions: []
  macos:
    bundle_id: com.example.myapp
    minimum_version: "13.0"
    sandbox: false                   # true = enable the macOS App Sandbox (e.g. Mac App Store)
  windows:
    publisher: ""
  android:
    min_sdk: 24
    target_sdk: 35
  ios:
    minimum_version: "16.0"

icons:
  source: assets/icons/icon.svg      # Source SVG for icon generation
```

## Upgrading Flugo

Flugo templates are embedded in the CLI binary, so upgrading involves two steps:

1. **Upgrade the CLI binary:**
   ```bash
   flugo upgrade              # Install latest release
   flugo upgrade main         # Install from main branch
   flugo upgrade v0.1.1       # Install a specific version
   ```

   To build from a local clone instead of GitHub:
   ```bash
   flugo upgrade main --flugo-path /path/to/flugo
   ```

2. **Update your project's framework files:**
   ```bash
   flugo update               # Apply the new binary's templates
   ```

When you run `flugo run` or `flugo build`, Flugo will print a hint if the project's `flugo_version` differs from the CLI version, reminding you to run `flugo update`.

## Linting

Scaffolded projects ship with linting baked in for both layers — no setup needed:

- `backend/.golangci.yml` — [golangci-lint](https://golangci-lint.run/) config (Go)
- `frontend/analysis_options.yaml` — Dart analyzer config (extends `package:flutter_lints/flutter.yaml`)

Both configs follow the same philosophy: **correctness-first**, not style nitpicks. Real bug catches like unchecked errors, ignored futures, leaked subscriptions, wrong-type comparisons — but no rules that exist purely to enforce a particular formatting opinion.

```bash
flugo lint                  # both linters
flugo lint backend          # Go only
flugo lint frontend         # Dart only
flugo lint --fix            # apply auto-fixes (golangci-lint --fix, dart fix --apply, dart format)
```

The Makefile exposes `make lint` and `make format` for the common case.

**Pre-requisites:**

| Tool | Install |
|------|---------|
| `golangci-lint` | https://golangci-lint.run/welcome/install |
| `flutter` / `dart` | comes with the Flutter SDK |

`flugo lint` only requires the tool for the target you're running — `flugo lint frontend` doesn't need golangci-lint, and vice versa.

### Going stricter

The shipped configs are a sensible baseline. To opt into stricter checks:

- **Dart** — swap `include: package:flutter_lints/flutter.yaml` in `analysis_options.yaml` for [`very_good_analysis`](https://pub.dev/packages/very_good_analysis) (~150 opinionated rules including style enforcement). Add to `dev_dependencies` in `pubspec.yaml`.
- **Go** — enable additional linters in `.golangci.yml`. Common adds: `gosec` (security), `gocritic` (opinionated checks), `revive` (golint replacement), `exhaustive` (switch coverage). See the [linters list](https://golangci-lint.run/usage/linters/).
- **Pre-commit hooks** — wire `flugo lint` into [`pre-commit`](https://pre-commit.com/) or a `.git/hooks/pre-commit` script so dirty code can't be committed.
- **CI** — `flugo lint` exits non-zero on any finding, drop-in for GitHub Actions / GitLab CI / etc.

The shipped baseline is what we'd recommend most projects keep. Override it when you've got a concrete reason to.

## Custom Title Bar

Scaffolded projects use a custom Flutter `CustomTitleBar` widget by default, controlled by the `titlebar_style` field in `flugo.yaml`. The native title bar is hidden via `TitleBarStyle.hidden` in `main.dart`, and the custom widget is injected above your app through the `MaterialApp.builder` callback (an `Overlay` → `Column` whose first child is the title bar) on desktop platforms.

**Style options:**

| Style | Description |
|-------|-------------|
| `native` | Uses the OS native titlebar; the custom widget is hidden |
| `default` | macOS: centered title; Linux/Windows: left-aligned title with window control buttons on the right |
| `clean` | Centered title on all platforms; Linux/Windows show traffic-light style window control buttons on the right |
| `custom` | Framework hands off `titlebar.dart` entirely — you own the file and it will not be overwritten by `flugo run`, `flugo build`, or `flugo update` |

```yaml
app:
  window:
    titlebar_style: clean
```

Then run `flugo run` or `flugo update` to apply the change.

**Runtime API:** `main.dart` exposes a `titlebarStyle` `ValueNotifier<CustomTitlebarStyle>` and a `setTitlebarStyle()` function for switching styles at runtime. Import them from `main.dart`:

```dart
import '../main.dart' show titlebarStyle, setTitlebarStyle, CustomTitlebarStyle;

// Switch style on button press
onPressed: () => setTitlebarStyle(CustomTitlebarStyle.clean),

// React to changes
ValueListenableBuilder<CustomTitlebarStyle>(
  valueListenable: titlebarStyle,
  builder: (context, style, _) { ... },
),
```

**Platform behavior:**

- **macOS** — left padding (70px) to avoid overlapping the native traffic light buttons (close/minimize/maximize)
- **Linux / Windows** — custom minimize, maximize/restore, and close buttons rendered on the right
- **Mobile** — the title bar is not rendered (desktop-only guard via `Platform.isLinux || Platform.isMacOS || Platform.isWindows`)

The entire title bar area is wrapped in a `DragToMoveArea` widget (from the [`window_manager`](https://pub.dev/packages/window_manager) package), allowing users to drag the window by clicking anywhere on the bar.

**Customization:** Set `titlebar_style: custom` in `flugo.yaml` for full control over `frontend/lib/app/titlebar.dart`. With the `custom` style, the framework will never overwrite your changes during `flugo run`, `flugo build`, or `flugo update`. For other styles, avoid editing `titlebar.dart` directly as your changes may be lost on `flugo update`.

## Icons

Generate all platform icons from a single source SVG:

```bash
flugo icons
```

Replace `assets/icons/icon.svg` with your own, then run `flugo icons`. Requires `rsvg-convert` or ImageMagick.

**Generated outputs:**

| Platform | Location | Sizes (px) |
|----------|----------|------------|
| Standard | `assets/icons/icon-{size}x{size}.png` | 64, 128, 256, 512 |
| Android | `frontend/android/.../mipmap-*/ic_launcher.png` | 48, 72, 96, 144, 192 |
| iOS | `frontend/ios/.../AppIcon.appiconset/` | 20--1024 (all required sizes) |

## Project Structure

After `flugo create myapp` (files marked *generated* are produced later by
`flugo generate`, not by `create`):

```
myapp/
├── flugo.yaml                 # Project configuration
├── Makefile                   # Common build targets
├── .gitignore                 # Standard ignores
├── README.md                  # Project readme
├── backend/
│   ├── go.mod                 # Go module
│   ├── .golangci.yml          # Go lint config (correctness-first)
│   ├── main.go                # Bridge initialization (bridge.Bind)
│   ├── service.go             # Your Go services
│   └── bridge/                # Generated bridge code
│       ├── bridge.gen.go
│       └── bridge.gen.h
├── frontend/
│   ├── pubspec.yaml           # Flutter dependencies
│   ├── analysis_options.yaml  # Dart analyzer config
│   ├── ffigen.yaml            # FFI generator config
│   ├── hook/
│   │   └── build.dart         # Native assets build hook
│   ├── linux/                 # Flutter platform runners
│   ├── macos/                 #   (generated by flutter create)
│   ├── windows/               #
│   ├── android/               #
│   ├── ios/                   #
│   └── lib/
│       ├── main.dart          # App entry point
│       ├── app/
│       │   ├── app.dart       # App widget
│       │   └── titlebar.dart  # Custom title bar (desktop)
│       └── bridge/
│           ├── bridge.gen.dart  # Generated Dart wrappers
│           └── filechooser.dart # Cross-platform file chooser (generated)
└── assets/
    ├── icons/
    │   └── icon.svg           # Source icon (Flugo default; replace with your own)
    └── linux/
        ├── {AppID}.desktop    # Desktop entry
        ├── {AppID}.metainfo.xml
        └── {AppID}.yaml       # Flatpak manifest
```

## Type Mapping

| Go | Dart |
|----|------|
| `string` | `String` |
| `int`, `int8`–`int64`, `uint`–`uint64` | `int` |
| `float32`, `float64` | `double` |
| `bool` | `bool` |
| `[]byte` | `Uint8List` (base64 over JSON) |
| `[]T` | `List<T>` |
| `map[K]V` | `Map<K, V>` |
| Custom struct | Dart class with `fromJson`/`toJson` |
| `error` | Throws `FlugoException` |

## Platform Support

| Platform | Output | Build Tool |
|----------|--------|------------|
| Linux | `.so` shared library + Flutter desktop | GCC (CGO) |
| macOS | `.dylib` + Flutter desktop | GCC/Clang (CGO) |
| Windows | `.dll` + Flutter desktop | MinGW (CGO) |
| Android | `.so` via NDK cross-compilation + Flutter APK | Android NDK |
| iOS ⚠️ | `.a` via Xcode cross-compilation + Flutter IPA | Xcode SDK |

> **⚠️ Pending — iOS and Flatpak are not yet release-ready:**
> - **iOS** builds require macOS + Xcode and are still **untested**. `flugo build
>   all` includes iOS, so it will fail on a non-macOS host.
> - **Flatpak** is wired for x86_64 and aarch64, but has **not been tested
>   end-to-end** — treat it as unverified until a real build is confirmed.
>
> Both are actively being worked on.

## Linux Packaging

### AppImage

Scaffold the required AppImage assets:

```bash
flugo appimage
```

This creates `assets/linux/appimage/` with an `AppRun` script, `.desktop` file, and icon placeholder. Then build:

```bash
flugo build appimage
```

This will:
- Auto-build the Linux release if not already built
- Auto-install `appimagetool` if not found (downloads a pinned, SHA-256-verified release from GitHub to `~/.local/bin/`)
- Assemble an AppDir with your app, Go backend, and bundled shared library dependencies
- Output: `build/package/linux/<AppName>-<version>-x86_64.AppImage`

### Flatpak

> **⚠️ Not yet verified:** the manifest builds the backend against a pinned Go
> toolchain for both **x86_64 and aarch64**, but Flatpak packaging has **not been
> tested end-to-end** yet — expect rough edges until it's confirmed on a real build.

Build a Flatpak from the manifest at `assets/linux/<AppID>.yaml`:

```bash
flugo build flatpak
```

Generate Flathub submission assets:

```bash
flugo flathub
```

## Documentation

Flugo includes built-in bridge functions that handle common cross-platform concerns automatically. Developers don't need to write platform-specific code for file dialogs, app directory resolution, or other OS-level differences -- the framework handles it.

- [File Chooser Service](docs/FILESYSTEM.md) -- cross-platform file pick, save, and open dialogs
- [App Base Directory](docs/PATHS.md) -- platform-correct app home directory (`bridge.BaseDir()`)
- [Platform Display Name](docs/MANIFEST.md) -- setting the app name on each platform's home screen / app list
- [Secure Intake (`bridge.Secret`)](docs/SECRETS.md) -- passphrase / key-material intake without JSON or string materialization
- [Real-time Streams (`bridge.Emitter`)](docs/STREAMING.md) -- Go→Dart push of a typed sequence, generated as a Dart `Stream<T>` (progress, live events)

## License

[Apache License 2.0](LICENSE) — Copyright 2026 [hkdb](https://github.com/hkdb). See
[`NOTICE`](NOTICE) for attribution.

