// Package jsonrpc implements JSON-RPC 2.0 request/response types and an
// in-process dispatcher. Transport is left to the caller.
package jsonrpc

import "encoding/json"

const Version = "2.0"

// Request is a JSON-RPC 2.0 request or notification. When len(ID) == 0 the
// message is a notification (no response is expected).
//
// Params and ID are kept as raw JSON because the spec leaves their types open:
// Params may be an object or array; ID may be a string, number, or null.
// Decode them into concrete types at the point of use.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// Response is a JSON-RPC 2.0 response. Exactly one of Result or Error is set
// on a valid response. ID is always present; it is JSON null when the server
// could not determine the request ID (e.g. parse error).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}
