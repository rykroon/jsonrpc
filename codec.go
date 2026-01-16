package jsonrpc

import (
	"encoding/json"
	"fmt"
	"net/rpc"
)

type NoParams struct{}

type serverCodec struct {
	req  *Request
	resp *Response
}

func newCodec(req *Request) *serverCodec {
	resp := &Response{
		JsonRpc: "2.0",
		Id:      req.Id,
	}
	return &serverCodec{
		req:  req,
		resp: resp,
	}
}

func (c *serverCodec) ReadRequestHeader(r *rpc.Request) error {
	r.ServiceMethod = c.req.Method
	r.Seq = 1 // placeholder
	return nil
}

func (c *serverCodec) ReadRequestBody(v any) error {
	_, ok := v.(*NoParams)
	if ok {
		return nil
	}

	if len(c.req.Params) == 0 {
		c.resp.Error = &Error{Code: ErrorCodeInvalidParams, Message: "missing params"}
		return fmt.Errorf("missing params")
	}

	err := json.Unmarshal(c.req.Params, v)
	if err != nil {
		c.resp.Error = &Error{Code: ErrorCodeInvalidParams, Message: err.Error()}
		return err
	}

	return nil
}

func (c *serverCodec) WriteResponse(r *rpc.Response, result any) error {
	if r.Error != "" {
		c.resp.Error = &Error{Code: -32000, Message: r.Error}
	} else {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		c.resp.Result = data
	}
	return nil
}

func (c *serverCodec) Close() error {
	return nil
}
