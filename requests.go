package jsonrpc

import (
	"encoding/json"
	"encoding/json/jsontext"
)

const Version = "2.0"

// Request is a JSON-RPC 2.0 request, or a notification when len(ID) == 0.
// Params and ID stay raw because the spec leaves their types open; decode
// them at the point of use.
type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  jsontext.Value `json:"params,omitzero"`
	ID      jsontext.Value `json:"id,omitzero"`
}

func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// NewRequest assembles a Request. Params and id are raw JSON — build them
// with NewParams and NewID. For a notification, use NewNotification.
func NewRequest(method string, params, id jsontext.Value) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params, ID: id}
}

// NewNotification assembles a Request without an id, so the server produces
// no response.
func NewNotification(method string, params jsontext.Value) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params}
}

// NewID returns the JSON encoding of v for use as Request.ID. The constraint
// matches the spec-allowed id shapes; marshal other types directly.
func NewID[T ~string | ~int | ~int64 | ~uint64](v T) jsontext.Value {
	b, _ := json.Marshal(v)
	return b
}

// NewParams marshals v for use as Request.Params. A nil v returns nil; a
// jsontext.Value passes through unchanged.
func NewParams(v any) (jsontext.Value, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(jsontext.Value); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
