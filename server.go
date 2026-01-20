package jsonrpc

import (
	"context"
	"encoding/json"
)

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req Request) Response
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

func (s *Server) ServeJsonRpc(ctx context.Context, req Request) Response {
	fn, exists := s.methods[req.Method()]
	if !exists {
		err := NewError(ErrorCodeMethodNotFound, "Method not found", nil)
		return NewErrorResponse(err, req.Id())
	}

	args := fn.NewArgs()
	err := req.Params().DecodeInto(args.Interface())
	if err != nil {
		err := NewError(ErrorCodeInvalidParams, "invalid params", nil)
		return NewErrorResponse(err, req.Id())
	}

	if req.IsNotification() {
		go fn.Call(args.Elem())
		return nil
	}

	result, err := fn.Call(args.Elem())
	if err != nil {
		e := NewError(ErrorCodeInternalError, err.Error(), nil)
		return NewErrorResponse(e, req.Id())
	}

	data, err := json.Marshal(result)
	if err != nil {
		jsonRpcErr := NewError(ErrorCodeInternalError, err.Error(), nil)
		return NewErrorResponse(jsonRpcErr, req.Id())
	}

	return NewSuccessResponse(data, req.Id())
}
