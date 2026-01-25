package jsonrpc

import (
	"encoding/json"
)

type Response interface {
	Jsonrpc() string
	Result() any
	Error() Error
	Id() Id
}

type ResponseEncoder func(Response) ([]byte, error)
type ResponseDecoder func([]byte) (Response, error)

type serverResponse struct {
	id     Id
	result any
	error  Error
}

func NewErrorResponse(error Error, id Id) Response {
	return serverResponse{
		id:    id,
		error: error,
	}
}

func NewSuccessResponse(result any, id Id) Response {
	return serverResponse{
		id:     id,
		result: result,
	}
}

func (r serverResponse) Jsonrpc() string {
	return "2.0"
}

func (r serverResponse) Id() Id {
	return r.id
}

func (r serverResponse) Error() Error {
	return r.error
}

func (r serverResponse) Result() any {
	return r.result
}

type clientResponse struct {
	RawJsonrpc json.RawMessage
	RawResult  json.RawMessage
	RawError   json.RawMessage
	RawId      json.RawMessage
}

func (r clientResponse) Jsonrpc() string {
	s := ""
	err := json.Unmarshal(r.RawJsonrpc, &s)
	if err != nil {
		return ""
	}
	return s
}

func (r clientResponse) Result() any {
	if len(r.RawResult) == 0 {
		return nil
	}
	var result any
	err := json.Unmarshal(r.RawResult, &result)
	if err != nil {
		return nil
	}
	return result
}

func (r clientResponse) Error() Error {
	if len(r.RawError) == 0 {
		return nil
	}
	rpcError := clientError{}
	err := json.Unmarshal(r.RawError, &rpcError)
	if err != nil {
		return nil
	}
	return rpcError
}

func (r clientResponse) Id() Id {
	if len(r.RawId) == 0 {
		return nil
	}
	id := rawId(r.RawId)
	return &id
}

func DefaultResponseEncoder(resp Response) ([]byte, error) {
	temp := map[string]any{
		"jsonrpc": resp.Jsonrpc(),
		"id":      resp.Id(),
	}
	if resp.Error() != nil {
		temp["error"] = resp.Error()
	} else {
		temp["result"] = resp.Result()
	}
	return json.Marshal(temp)
}
