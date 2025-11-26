package jsonrpc

import "fmt"

const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
)

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func NewError(code int, message string, data any) error {
	return &Error{code, message, data}
}

func (e Error) Error() string {
	return fmt.Sprintf("jsonrpc.Error(Code=%d, Message=%s, Data=%v)", e.Code, e.Message, e.Data)
}

func ParseError(data any) error {
	return NewError(ErrorCodeParseError, "Parse Error", data)
}

func InvalidRequest(data any) error {
	return NewError(ErrorCodeInvalidRequest, "Invalid Request", data)
}

func MethodNotFound(data any) error {
	return NewError(ErrorCodeMethodNotFound, "Method Not Found", data)
}

func InvalidParams(data any) error {
	return NewError(ErrorCodeInvalidParams, "Invalid Params", data)
}

func InternalError(data any) error {
	return NewError(ErrorCodeInternalError, "Internal Error", data)
}
