# Vendored Dart SDK headers

The C sources in this directory are copied **verbatim** from the Dart SDK's
`runtime/include` (the Dart Native API dynamic-linking headers):

- `dart_api_dl.c`, `dart_api_dl.h`
- `dart_api.h`, `dart_native_api.h`, `dart_version.h`
- `internal/dart_api_dl_impl.h`

They are © the Dart project authors and licensed under a BSD-style license (see
the header comment in each file). flugo bundles them so the Go backend can post
messages to a Dart isolate via `Dart_PostCObject_DL` — see `post.go`.

Do not edit these files. To update, re-copy them from a Dart SDK's
`bin/cache/dart-sdk/include/` (current pin: `DART_API_DL` v2.6, see
`dart_version.h`).

Upstream license text: https://github.com/dart-lang/sdk/blob/main/LICENSE
