package jsonrpc

import (
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
)

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// CodeServerError and CodeServerErrorMin bound the server-defined error
	// range, reserved for application errors and free of the protocol codes
	// above. CodeServerError is the conventional default.
	CodeServerError    = -32000 // first (highest) server-defined code
	CodeServerErrorMin = -32099 // last (lowest) server-defined code
)

// Error is the JSON-RPC error object. Data is optional and holds raw JSON for
// the caller to decode.
type Error struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    jsontext.Value `json:"data,omitzero"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc: %d %s", e.Code, e.Message)
}

func NewError(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// SetData marshals data into the Data field, leaving Data unchanged on
// marshal failure.
func (e *Error) SetData(data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	e.Data = b
	return nil
}

// MustSetData is SetData for data known to be marshalable. It panics on
// failure and returns e so it can be chained.
func (e *Error) MustSetData(data any) *Error {
	if err := e.SetData(data); err != nil {
		panic(fmt.Sprintf("jsonrpc: marshal error data: %v", err))
	}
	return e
}

// UnmarshalData decodes the Data field into into. Empty Data is a no-op.
func (e *Error) UnmarshalData(into any) error {
	if len(e.Data) == 0 {
		return nil
	}
	return json.Unmarshal(e.Data, into)
}

// canonicalMessage returns the spec's message for a reserved protocol code.
func canonicalMessage(code int) string {
	switch code {
	case CodeParseError:
		return "Parse error"
	case CodeInvalidRequest:
		return "Invalid Request"
	case CodeMethodNotFound:
		return "Method not found"
	case CodeInvalidParams:
		return "Invalid params"
	default:
		return "Internal error"
	}
}

// protocolError builds a library-generated protocol error: the spec's
// canonical message, with the cause in Data as {"details": ...}. Errors from
// handlers are never rewritten this way.
func protocolError(code int, details string) *Error {
	e := NewError(code, canonicalMessage(code))
	e.Data, _ = json.Marshal(struct {
		Details string `json:"details"`
	}{details})
	return e
}
