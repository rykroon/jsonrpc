package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// CodeServerError and CodeServerErrorMin bound the server-defined range,
	// reserved for application errors. CodeServerError is the conventional
	// default.
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

// WithData returns a copy of e with data marshaled into Data; e is unchanged,
// so shared errors stay safe to reuse.
func (e *Error) WithData(data any) (*Error, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	c := *e
	c.Data = b
	return &c, nil
}

// MustWithData is WithData for data known to be marshalable. It panics on failure.
func (e *Error) MustWithData(data any) *Error {
	c, err := e.WithData(data)
	if err != nil {
		panic(fmt.Sprintf("jsonrpc: marshal error data: %v", err))
	}
	return c
}

// UnmarshalData decodes the Data field into into. Empty Data is a no-op.
func (e *Error) UnmarshalData(into any) error {
	if len(e.Data) == 0 {
		return nil
	}
	return json.Unmarshal(e.Data, into)
}
