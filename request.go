package jsonrpc

type Request struct {
	JsonRpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  Params `json:"params,omitzero"`
	Id      Id     `json:"id,omitzero"`
}

func NewRequest(method string, params *Params, id Id) *Request {
	if params == nil {
		params = &Params{}
	}
	return &Request{
		JsonRpc: "2.0",
		Method:  method,
		Params:  *params,
		Id:      id,
	}
}

func NewNotification(method string, params *Params) *Request {
	return NewRequest(method, params, Id{})
}

func (r *Request) IsNotification() bool {
	return r.Id.IsAbsent()
}
