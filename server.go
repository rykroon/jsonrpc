package jsonrpc

import (
	"encoding/json"
)

type JsonRpcServer interface {
	ServeJsonRpc(req Request) Response
	Register(string, any) error
}

type defaultServer struct {
	methods map[string]funcValue
}

func NewServer() *defaultServer {
	return &defaultServer{
		methods: make(map[string]funcValue),
	}
}

func (s *defaultServer) Register(method string, fn any) error {
	fv, err := newFuncValue(fn)
	if err != nil {
		return err
	}
	s.methods[method] = fv
	return nil

}

func (s *defaultServer) ServeJsonRpc(req Request) Response {
	respId := req.Id()
	if respId == nil {
		respId = NullId()
	}

	if req.Jsonrpc() != "2.0" {
		return NewErrorResponse(
			NewError(ErrorCodeInvalidRequest, "jsonrpc must be 2.0", nil),
			respId,
		)
	}

	if req.Method() == "" {
		return NewErrorResponse(
			NewError(ErrorCodeInvalidRequest, "missing method", nil),
			respId,
		)
	}

	fn, exists := s.methods[req.Method()]
	if !exists {
		return NewErrorResponse(
			NewError(ErrorCodeMethodNotFound, "Method not found", nil),
			respId,
		)
	}

	args := fn.NewArgs()
	err := req.Params().DecodeInto(args.Interface())
	if err != nil {
		return NewErrorResponse(
			NewError(ErrorCodeInvalidParams, "invalid params", nil),
			respId,
		)
	}

	if req.Id() == nil {
		go fn.Call(args.Elem())
		return nil
	}

	result, err := fn.Call(args.Elem())
	if err != nil {
		return NewErrorResponse(
			NewError(ErrorCodeInternalError, err.Error(), nil),
			respId,
		)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return NewErrorResponse(
			NewError(ErrorCodeInternalError, err.Error(), nil),
			respId,
		)
	}

	return NewSuccessResponse(data, respId)
}
