package jsonrpc

import (
	"encoding/json"
	"fmt"
)

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// CodeServerError and CodeServerErrorMin bound the JSON-RPC 2.0
	// server-defined error range (-32000 to -32099), reserved for
	// application-defined errors. Choose codes within this range to avoid
	// colliding with the reserved protocol codes above; CodeServerError is
	// the conventional default.
	CodeServerError    = -32000 // first (highest) server-defined code
	CodeServerErrorMin = -32099 // last (lowest) server-defined code
)

// Error is the JSON-RPC error object. Data is optional; when present it holds
// raw JSON so the caller can decode it into whatever shape they expect.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc: %d %s", e.Code, e.Message)
}

func NewError(code int, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithData attaches data as the Error's Data field. It panics if data cannot
// be JSON-marshaled, since that indicates a programmer error at the call site.
func (e *Error) WithData(data any) *Error {
	b, err := json.Marshal(data)
	if err != nil {
		panic(fmt.Sprintf("jsonrpc: marshal error data: %v", err))
	}
	e.Data = b
	return e
}

// UnmarshalData decodes the Error's Data field into into. If Data is empty
// (no data attached) it is a no-op and returns nil.
func (e *Error) UnmarshalData(into any) error {
	if len(e.Data) == 0 {
		return nil
	}
	return json.Unmarshal(e.Data, into)
}
