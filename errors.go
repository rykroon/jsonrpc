package jsonrpc

import (
	"encoding/json"
	"fmt"
)

const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
	ErrorCodeServerError    = -32000
)

type Error interface {
	error
	Code() int
	Message() string
	Data() any
}

type serverError struct {
	code    int
	message string
	data    any
}

// returns a new error
func NewError(code int, message string, data any) Error {
	return serverError{code, message, data}
}

func (e serverError) Code() int {
	return e.code
}

func (e serverError) Message() string {
	return e.message
}

func (e serverError) Data() any {
	return e.data
}

func (e serverError) Error() string {
	return fmt.Sprintf("Code=%d, Message=%s, Data=%v", e.Code(), e.Message(), e.Data())
}

type clientError struct {
	RawCode    json.RawMessage `json:"code"`
	RawMessage json.RawMessage `json:"message"`
	RawData    json.RawMessage `json:"data"`
}

func (e clientError) Code() int {
	code := 0
	err := json.Unmarshal(e.RawCode, &code)
	if err != nil {
		return 0
	}
	return code
}

func (e clientError) Message() string {
	message := ""
	err := json.Unmarshal(e.RawCode, &message)
	if err != nil {
		return ""
	}
	return message
}

func (e clientError) Data() any {
	var data any
	err := json.Unmarshal(e.RawCode, &data) // will this work?
	if err != nil {
		return 0
	}
	return data
}

func (e clientError) Error() string {
	return fmt.Sprintf("Code=%d, Message=%s, Data=%v", e.Code(), e.Message(), e.Data())
}
