//go:build android

package bridge

/*
#cgo LDFLAGS: -landroid -llog

#include <jni.h>
#include <stdlib.h>

static JavaVM* _flugo_jvm = NULL;

__attribute__((visibility("default")))
jint JNI_OnLoad(JavaVM* vm, void* reserved) {
	_flugo_jvm = vm;
	return JNI_VERSION_1_6;
}

static JavaVM* flugo_get_jvm() {
	return _flugo_jvm;
}

static int flugo_jvm_is_null() {
	return _flugo_jvm == NULL ? 1 : 0;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// GetJVMPtr returns the raw JavaVM pointer as uintptr, or 0 if not captured.
func GetJVMPtr() uintptr {
	if C.flugo_jvm_is_null() != 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(C.flugo_get_jvm()))
}

// JNIStatus returns a diagnostic string.
func JNIStatus() string {
	if C.flugo_jvm_is_null() != 0 {
		return "JNI_OnLoad not called (JVM is null)"
	}
	return fmt.Sprintf("OK (JVM=%v)", GetJVMPtr() != 0)
}
