package bridge

/*
#include <stdint.h>
*/
import "C"

import (
	"unsafe"

	"github.com/hkdb/flugo/pkg/bridge/dartpost"
)

// FlugoInitDartApi wires up the Dart dynamic-linking API so Go can push stream
// messages to Dart ports. Dart calls this ONCE at startup with
// NativeApi.initializeApiDLData. Returns 1 on success, 0 on failure.
//
//export FlugoInitDartApi
func FlugoInitDartApi(data unsafe.Pointer) C.int {
	if dartpost.InitAPI(data) {
		return 1
	}
	return 0
}

// FlugoOpenStream starts a bound stream method. name/payload mirror FlugoCall
// (method key + JSON params array); port is the target Dart ReceivePort's native
// port (RawReceivePort.sendPort.nativePort). Returns a stream id for
// FlugoStreamCancel, or 0 if the stream ended immediately.
//
//export FlugoOpenStream
func FlugoOpenStream(namePtr *C.char, nameLen C.int, payloadPtr *C.char, payloadLen C.int, port C.int64_t) C.int64_t {
	name := C.GoStringN(namePtr, nameLen)
	var payload []byte
	if payloadLen > 0 {
		payload = C.GoBytes(unsafe.Pointer(payloadPtr), payloadLen)
	}
	sid := DispatchStream(name, payload, int64(port))
	return C.int64_t(sid)
}

// FlugoStreamCancel signals the producer to stop when the Dart subscriber
// unsubscribes.
//
//export FlugoStreamCancel
func FlugoStreamCancel(sid C.int64_t) {
	CancelStream(int64(sid))
}
