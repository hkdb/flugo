// Package dartpost is flugo's low-level Go→Dart push transport. It wraps the
// Dart Native API (dynamic-linking) so Go can post messages to a Dart isolate's
// ReceivePort from any thread — the mechanism behind flugo's streaming bridge
// (pkg/bridge Emitter/DispatchStream). Unlike the request/response FFI, this
// lets Go initiate delivery to Dart.
//
// The C sources in this directory (dart_api_dl.*, dart_api.h, dart_native_api.h,
// dart_version.h, internal/dart_api_dl_impl.h) are vendored verbatim from the
// Dart SDK's runtime/include and are BSD-licensed (see each file's header and
// DART_LICENSE.md). Do not edit them; refresh by re-copying from the SDK.
package dartpost

/*
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include "dart_api_dl.h"

// flugo_init_api wires up the DL function pointers from the data blob Dart hands
// over (NativeApi.initializeApiDLData). Returns 0 on success.
static intptr_t flugo_init_api(void* data) {
    return Dart_InitializeApiDL(data);
}

// flugo_post_string posts a UTF-8 C string to a Dart port as a message. The
// string is copied into the Dart heap synchronously, so the caller may free it
// right after. Returns false if the API isn't initialized yet or the post fails
// (e.g. the port was closed).
static bool flugo_post_string(int64_t port, const char* str) {
    if (Dart_PostCObject_DL == 0) {
        return false;
    }
    Dart_CObject obj;
    obj.type = Dart_CObject_kString;
    obj.value.as_string = (char*)str;
    return Dart_PostCObject_DL((Dart_Port)port, &obj);
}
*/
import "C"

import "unsafe"

// InitAPI initializes the Dart dynamic-linking API from the pointer Dart passes
// as NativeApi.initializeApiDLData. Call once per process before PostString.
// Returns true on success.
func InitAPI(data unsafe.Pointer) bool {
	return C.flugo_init_api(data) == 0
}

// PostString posts a UTF-8 string (typically a JSON message) to the Dart port.
// Safe from any goroutine/OS thread. Returns false if the API is uninitialized
// or the port is gone. The message is copied into the Dart heap, so s need not
// outlive the call.
func PostString(port int64, s string) bool {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return bool(C.flugo_post_string(C.int64_t(port), cs))
}
