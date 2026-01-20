package jsonrpc

import "encoding/json"

type Response interface {
	Jsonrpc() string
	Result() any
	Error() Error
	Id() Id
}

type serverResponse struct {
	id     Id
	result any
	error  Error
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

type serverSuccessResponse struct {
	serverResponse
	result any
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

type clientResponse struct {
	RawJsonrpc json.RawMessage
	RawResult  json.RawMessage
	RawError   json.RawMessage
	RawId      idBytes
}

func (r clientResponse) Jsonrpc() string {
	s := ""
	err := json.Unmarshal(r.RawJsonrpc, &s)
	if err != nil {
		return ""
	}
	return s
}

func (r clientResponse) Id() Id {
	return r.RawId
}
