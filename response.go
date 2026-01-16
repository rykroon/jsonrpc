package jsonrpc

import (
	"encoding/json"
)

type Response struct {
	JsonRpc string          `json:"jsonrpc"`
	Id      Id              `json:"id"`
	Result  json.RawMessage `json:"result,omitzero"`
	Error   *Error          `json:"error,omitzero"`
}

func NewSuccessResp(id Id, result json.RawMessage) *Response {
	return &Response{"2.0", id, result, nil}
}

func NewErrorResp(id Id, err *Error) *Response {
	return &Response{"2.0", id, nil, err}
}

func (r *Response) IsSuccess() bool {
	return r.Result != nil
}

func (r *Response) IsError() bool {
	return r.Error != nil
}
