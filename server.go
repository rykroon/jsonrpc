package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
)

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
	Register(string, any) error
}

type Server struct {
	methods map[string]funcValue
}

func NewServer() *Server {
	return &Server{
		methods: make(map[string]funcValue),
	}
}

func (s *Server) Register(method string, fn any) error {
	fv, err := newFuncValue(fn)
	if err != nil {
		return err
	}
	s.methods[method] = fv
	return nil

}

func (s *Server) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	fn, exists := s.methods[req.Method]
	if !exists {
		err := NewError(ErrorCodeMethodNotFound, "Method not found", nil).(*Error)
		return NewErrorResp(req.Id, err)
	}

	args := fn.NewArgs()
	err := req.Params.Decode(args.Interface())
	if err != nil {
		err := NewError(ErrorCodeInvalidParams, "invalid params", nil).(*Error)
		return NewErrorResp(req.Id, err)
	}

	if req.IsNotification() {
		go fn.Call(args.Elem())
		return nil
	}

	result, err := fn.Call(args.Elem())
	if err != nil {
		jsonRpcErr := &Error{}
		if ok := errors.As(err, jsonRpcErr); ok {
			return NewErrorResp(req.Id, jsonRpcErr)
		} else {
			e := NewError(ErrorCodeInternalError, err.Error(), nil).(*Error)
			return NewErrorResp(req.Id, e)
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		jsonRpcErr := NewError(ErrorCodeInternalError, err.Error(), nil).(*Error)
		return NewErrorResp(req.Id, jsonRpcErr)
	}

	return NewSuccessResp(req.Id, data)
}
