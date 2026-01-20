package jsonrpc

import "encoding/json"

type Request interface {
	Jsonrpc() string
	Method() string
	Params() Params
	Id() Id
	IsNotification() bool
}

type clientRequest struct {
	method string
	id     Id
	params Params
}

func NewRequest(method string, params Params, id Id) Request {
	return clientRequest{
		method: method,
		id:     id,
		params: params,
	}
}

func NewNotification(method string, params Params) Request {
	return NewRequest(method, params, nil)
}

func (r clientRequest) Jsonrpc() string {
	return "2.0"
}

func (r clientRequest) Method() string {
	return r.method
}

func (r clientRequest) Id() Id {
	return r.id
}

func (r clientRequest) Params() Params {
	return r.params
}

func (r clientRequest) IsNotification() bool {
	return r.id == nil
}

type serverRequest struct {
	RawJsonrpc json.RawMessage `json:"jsonrpc"`
	RawMethod  json.RawMessage `json:"method"`
	RawId      idBytes         `json:"id"`
	RawParams  paramBytes      `json:"params"`
}

func (r serverRequest) Jsonrpc() string {
	s := ""
	err := json.Unmarshal(r.RawJsonrpc, &s)
	if err != nil {
		return ""
	}
	return s
}

func (r serverRequest) Method() string {
	s := ""
	err := json.Unmarshal(r.RawMethod, &s)
	if err != nil {
		return ""
	}
	return s
}

func (r serverRequest) Id() Id {
	return r.RawId
}

func (r serverRequest) Params() Params {
	return r.RawParams
}

func (r serverRequest) IsNotification() bool {
	return len(r.RawId) == 0
}
