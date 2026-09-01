//go:build !windows

package bridge

// RegisterURLScheme is a no-op outside Windows: scheme registration is
// static there (Info.plist, AndroidManifest, .desktop MimeType), written by
// `flugo deeplink`. On Windows it writes the per-user registry handler.
func RegisterURLScheme(scheme string) error { return nil }
