package jsonrpc

import "fmt"

const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
	ErrorCodeServerError    = -32000
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// returns a new error
func NewError(code int, message string, data any) error {
	return &Error{code, message, data}
}

func (e Error) Error() string {
	return fmt.Sprintf("jsonrpc.Error(Code=%d, Message=%s, Data=%v)", e.Code, e.Message, e.Data)
}
