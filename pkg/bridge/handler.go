package bridge

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
)

// methodInfo holds the reflection data needed to invoke a bound method.
type methodInfo struct {
	receiver reflect.Value
	method   reflect.Method
}

// registry maps "TypeName.MethodName" to its methodInfo. It is guarded by
// registryMu because dispatch calls arrive on Dart-owned OS threads while
// Bind/Register may run concurrently (or lazily) on the Go side.
var (
	registryMu sync.RWMutex
	registry   = map[string]*methodInfo{}
)

// Register adds a single method to the registry.
func Register(name string, receiver reflect.Value, method reflect.Method) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = &methodInfo{receiver: receiver, method: method}
}

// lookup returns the registered method, safely under the read lock.
func lookup(method string) (*methodInfo, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	info, ok := registry[method]
	return info, ok
}

// callMethod invokes a bound method, recovering any panic so it cannot unwind
// across the cgo //export boundary and abort the whole host process. A
// recovered panic is returned as an error for the caller to encode as a normal
// error response.
func callMethod(info *methodInfo, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in %s: %v", info.method.Name, r)
		}
	}()
	return info.method.Func.Call(args), nil
}

// DispatchSecure invokes the named method with raw bytes that get sealed
// into a memguard.Enclave-backed *Secret on intake. The named method must
// have exactly one user-visible parameter, of type *bridge.Secret. The
// input bytes are wiped during the seal — do not reuse the slice after
// calling.
//
// Designed for sensitive intake (passphrases, key material) that must
// avoid materializing as Go strings or JSON-encoded payloads. For all
// other methods, use Dispatch.
func DispatchSecure(method string, data []byte) []byte {
	info, ok := lookup(method)
	if !ok {
		return errorResponse(fmt.Sprintf("unknown method: %s", method))
	}

	methodType := info.method.Type
	numIn := methodType.NumIn() - 1 // exclude receiver
	if numIn != 1 {
		return errorResponse(fmt.Sprintf("%s: secure dispatch requires exactly 1 user param, got %d", method, numIn))
	}

	paramType := methodType.In(1)
	if paramType != reflect.TypeOf((*Secret)(nil)) {
		return errorResponse(fmt.Sprintf("%s: secure dispatch requires *bridge.Secret param, got %s", method, paramType))
	}

	if len(data) == 0 {
		return errorResponse(fmt.Sprintf("%s: secure dispatch received empty payload", method))
	}

	secret, err := NewSecret(data)
	if err != nil {
		return errorResponse(fmt.Sprintf("%s: sealing secret: %v", method, err))
	}

	args := []reflect.Value{info.receiver, reflect.ValueOf(secret)}
	results, err := callMethod(info, args)
	if err != nil {
		return errorResponse(err.Error())
	}
	return encodeResults(results)
}

// Dispatch invokes the named method with the given JSON params and returns a JSON response.
func Dispatch(method string, paramsJSON []byte) []byte {
	info, ok := lookup(method)
	if !ok {
		return errorResponse(fmt.Sprintf("unknown method: %s", method))
	}

	methodType := info.method.Type
	// First param is always the receiver, so user-visible params start at index 1.
	numIn := methodType.NumIn() - 1

	// Decode params array.
	var rawParams []json.RawMessage
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &rawParams); err != nil {
			return errorResponse(fmt.Sprintf("invalid params JSON: %v", err))
		}
	}

	if len(rawParams) != numIn {
		return errorResponse(fmt.Sprintf("%s expects %d params, got %d", method, numIn, len(rawParams)))
	}

	// Build argument list.
	args := make([]reflect.Value, numIn+1)
	args[0] = info.receiver
	for i := 0; i < numIn; i++ {
		paramType := methodType.In(i + 1)
		paramPtr := reflect.New(paramType)
		if err := json.Unmarshal(rawParams[i], paramPtr.Interface()); err != nil {
			return errorResponse(fmt.Sprintf("param %d: %v", i, err))
		}
		args[i+1] = paramPtr.Elem()
	}

	// Call the method (panics are recovered so they can't abort the process).
	results, err := callMethod(info, args)
	if err != nil {
		return errorResponse(err.Error())
	}

	return encodeResults(results)
}

// encodeResults converts method return values into a JSON Response.
// Supports: (), (T), (error), (T, error).
func encodeResults(results []reflect.Value) []byte {
	switch len(results) {
	case 0:
		return successResponse(nil)
	case 1:
		v := results[0]
		if isErrorType(v.Type()) {
			if !v.IsNil() {
				return errorResponse(v.Interface().(error).Error())
			}
			return successResponse(nil)
		}
		return successResponse(v.Interface())
	case 2:
		errVal := results[1]
		if isErrorType(errVal.Type()) && !errVal.IsNil() {
			return errorResponse(errVal.Interface().(error).Error())
		}
		return successResponse(results[0].Interface())
	default:
		// Pack multiple return values as an array.
		vals := make([]any, len(results))
		for i, r := range results {
			vals[i] = r.Interface()
		}
		return successResponse(vals)
	}
}

var errorInterface = reflect.TypeOf((*error)(nil)).Elem()

func isErrorType(t reflect.Type) bool {
	return t.Implements(errorInterface)
}

func successResponse(result any) []byte {
	resp := Response{Result: result}
	data, err := json.Marshal(resp)
	if err != nil {
		return errorResponse(fmt.Sprintf("marshal error: %v", err))
	}
	return data
}

func errorResponse(msg string) []byte {
	resp := Response{Error: msg}
	data, _ := json.Marshal(resp)
	return data
}
