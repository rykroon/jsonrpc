package jsonrpc

import (
	"encoding/json"
	"io"
)

type Request interface {
	Jsonrpc() string
	Method() string
	Params() Params
	Id() Id
}

type RequestEncoder func(Request, io.Writer) error

func DefaultRequestEncoder(req Request, w io.Writer) error {
	temp := struct {
		Jsonrpc string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  Params `json:"params,omitzero"`
		Id      Id     `json:"id,omitzero"`
	}{
		Jsonrpc: req.Jsonrpc(),
		Method:  req.Method(),
		Params:  req.Params(),
		Id:      req.Id(),
	}
	enc := json.NewEncoder(w)
	return enc.Encode(temp)
}

type RequestDecoder func(io.Reader) (Request, error)

func DefaultRequestdecoder(r io.Reader) (Request, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	req := serverRequest{}
	err := dec.Decode(&req)
	if err != nil {
		return nil, err
	}
	return req, nil
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

type serverRequest struct {
	RawJsonrpc json.RawMessage `json:"jsonrpc"`
	RawMethod  json.RawMessage `json:"method"`
	RawId      json.RawMessage `json:"id"`
	RawParams  json.RawMessage `json:"params"`
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
	if len(r.RawId) == 0 {
		return nil
	}
	id := idRaw(r.RawId)
	return &id
}

func (r serverRequest) Params() Params {
	if len(r.RawParams) == 0 {
		return nil
	}
	params := paramsRaw(r.RawParams)
	return &params
}
