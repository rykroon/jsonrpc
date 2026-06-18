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

// Decode unmarshals r.Result into into. It is a no-op when into is nil or
// Result is empty. Callers should check r.Error before calling Decode.
func (r *Response) Decode(into any) error {
	if into == nil || len(r.Result) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result, into)
}

// NewRequest assembles a Request with the given method, params, and id. To
// build a notification (no id, no response expected), use NewNotification.
// Params and id are raw JSON; use NewParams and NewID to build them from
// Go values.
func NewRequest(method string, params, id json.RawMessage) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params, ID: id}
}

// NewNotification assembles a Request without an id. The server dispatches
// the method but produces no response.
func NewNotification(method string, params json.RawMessage) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params}
}

// NewID returns the JSON encoding of v for use as Request.ID. The type
// constraint matches the spec-allowed id shapes (string or integer). For
// other types, marshal directly with json.Marshal.
func NewID[T ~string | ~int | ~int64 | ~uint64](v T) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// NewParams marshals v into JSON for use as Request.Params. A nil v returns
// nil. A json.RawMessage is returned unchanged.
func NewParams(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
