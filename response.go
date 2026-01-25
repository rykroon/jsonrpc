package jsonrpc

import (
	"encoding/json"
	"errors"
)

type Response interface {
	Jsonrpc() string
	Result() any
	Error() Error
	Id() Id
	json.Marshaler
	json.Unmarshaler
}

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

func (r serverResponse) MarshalJSON() ([]byte, error) {
	temp := map[string]any{
		"jsonrpc": r.Jsonrpc(),
		"id":      r.Id(),
	}
	if r.Error() != nil {
		temp["error"] = r.Error()
	} else {
		temp["result"] = r.Result()
	}

	return json.Marshal(temp)
}

func (r serverResponse) UnmarshalJSON(b []byte) error {
	return errors.New("not implemented")
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
