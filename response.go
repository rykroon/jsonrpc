package jsonrpc

import (
	"encoding/json"
)

type Response struct {
	JsonRpc string          `json:"jsonrpc"`
	Id      Id              `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func NewSuccessResp(id Id, result json.RawMessage) *Response {
	return &Response{"2.0", id, result, nil}
}

func NewErrorResp(id Id, e error) *Response {
	switch concreteError := e.(type) {
	case *Error:
		return &Response{"2.0", id, nil, concreteError}
	default:
		newError, _ := InternalError(e.Error()).(*Error)
		return &Response{"2.0", id, nil, newError}
	}
}
