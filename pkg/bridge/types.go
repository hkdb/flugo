// Package bridge provides the Go runtime for the Flugo FFI bridge.
// Apps import this package and call Bind() to expose Go structs to Flutter.
package bridge

// Request is the JSON envelope sent from Dart to Go.
type Request struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

// Response is the JSON envelope returned from Go to Dart.
type Response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// BytesResponse wraps a raw byte result with an ID for retrieval.
type BytesResponse struct {
	ID    int64  `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}
