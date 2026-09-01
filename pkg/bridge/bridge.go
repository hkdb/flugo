package bridge

/*
#include <stdlib.h>
#include <stdint.h>
*/
import "C"

import (
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Bind registers a struct instance so all its public methods become callable
// from Flutter via the FFI bridge. Methods are registered as "TypeName.MethodName".
//
// Usage:
//
//	bridge.Bind(&CryptoService{})
func Bind(service interface{}) {
	v := reflect.ValueOf(service)
	t := v.Type()

	// Derive the concrete type name. The usual call is bridge.Bind(&Svc{}); a
	// value is also accepted (only its value-receiver methods bind). Guard the
	// non-pointer/anonymous cases so we return a clear error instead of an
	// opaque reflect panic from t.Elem() on a non-pointer.
	typeName := t.Name()
	if t.Kind() == reflect.Pointer {
		typeName = t.Elem().Name()
	}
	if typeName == "" {
		panic("bridge.Bind: service must be a named struct or a pointer to one")
	}

	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		if !method.IsExported() {
			continue
		}
		name := typeName + "." + method.Name
		Register(name, v, method)
	}
}

// --- Byte buffer store for FlugoCallBytes ---

// maxByteStoreEntries bounds the pending-results map so results that Dart never
// fetches (abandoned/errored calls) can't accumulate forever. Evicted buffers
// are zeroed in case they hold sensitive response data.
const maxByteStoreEntries = 256

var (
	byteStore   = map[int64][]byte{}
	byteStoreMu sync.Mutex
	byteStoreID atomic.Int64
)

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func storeBytes(data []byte) int64 {
	id := byteStoreID.Add(1)
	byteStoreMu.Lock()
	byteStore[id] = data
	// Bound the store: if it grew past the cap, evict the oldest entry (the
	// smallest id — an id Dart never drained). ids are monotonic, so min id is
	// the oldest. Cheap since the map is capped small.
	if len(byteStore) > maxByteStoreEntries {
		oldest := id
		for k := range byteStore {
			if k < oldest {
				oldest = k
			}
		}
		if buf, ok := byteStore[oldest]; ok {
			zeroBytes(buf)
			delete(byteStore, oldest)
		}
	}
	byteStoreMu.Unlock()
	return id
}

// --- C-exported FFI gateway functions ---

//export FlugoCall
func FlugoCall(namePtr *C.char, nameLen C.int, payloadPtr *C.char, payloadLen C.int) *C.char {
	name := C.GoStringN(namePtr, nameLen)
	var payload []byte
	if payloadLen > 0 {
		payload = C.GoBytes(unsafe.Pointer(payloadPtr), payloadLen)
	}
	result := Dispatch(name, payload)
	return C.CString(string(result))
}

//export FlugoCallBytes
func FlugoCallBytes(namePtr *C.char, nameLen C.int, dataPtr *C.char, dataLen C.int) C.int64_t {
	name := C.GoStringN(namePtr, nameLen)
	var data []byte
	if dataLen > 0 {
		data = C.GoBytes(unsafe.Pointer(dataPtr), dataLen)
	}
	result := Dispatch(name, data)
	id := storeBytes(result)
	return C.int64_t(id)
}

// FlugoCallSecure is the raw-bytes intake path for sensitive material. The
// caller passes a pointer to a byte buffer (typically a Dart Uint8List)
// and a length; the bytes are copied into a Go []byte via C.GoBytes,
// dispatched to the named method via DispatchSecure (which seals them into
// a memguard enclave, wiping the intermediate Go copy — the caller must still
// zero the Dart-side buffer, which Go cannot touch), and the JSON response is
// returned through the same malloc'd-CString channel as FlugoCall.
//
// Use this for methods bound to take exactly one *bridge.Secret parameter.
// Caller still frees the result with FlugoFreeResult.
//
//export FlugoCallSecure
func FlugoCallSecure(namePtr *C.char, nameLen C.int, dataPtr *C.uint8_t, dataLen C.int) *C.char {
	name := C.GoStringN(namePtr, nameLen)
	var data []byte
	if dataLen > 0 && dataPtr != nil {
		data = C.GoBytes(unsafe.Pointer(dataPtr), dataLen)
	}
	result := DispatchSecure(name, data)
	return C.CString(string(result))
}

//export FlugoFreeResult
func FlugoFreeResult(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

//export FlugoGetBytes
func FlugoGetBytes(id C.int64_t, outLen *C.int) *C.char {
	byteStoreMu.Lock()
	data, ok := byteStore[int64(id)]
	if ok {
		delete(byteStore, int64(id))
	}
	byteStoreMu.Unlock()

	if !ok {
		*outLen = 0
		return nil
	}
	*outLen = C.int(len(data))
	cData := C.CBytes(data) // copies into C memory
	zeroBytes(data)         // wipe the Go copy now that it's handed off
	return (*C.char)(cData)
}
