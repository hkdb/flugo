//go:build !android

package bridge

// GetJVMPtr returns 0 on non-Android platforms.
func GetJVMPtr() uintptr { return 0 }

// JNIStatus returns "not Android" on non-Android platforms.
func JNIStatus() string { return "not Android" }
