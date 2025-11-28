package jsonrpc

import (
	"context"
	"encoding/json"
)

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
}

type HandlerFunc func(ctx context.Context, params *Params) (any, error)

type jsonRpcServer struct {
	methods map[string]HandlerFunc
}

func NewServer() JsonRpcServer {
	return &jsonRpcServer{
		methods: make(map[string]HandlerFunc),
	}
}

func (s *jsonRpcServer) Register(method string, handler HandlerFunc) {
	s.methods[method] = handler
}

func (s *jsonRpcServer) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	handler, exists := s.methods[req.Method]
	if !exists {
		return NewErrorResp(req.Id, NewErrorTyped(ErrorCodeMethodNotFound, "Method not found", nil))
	}

	if req.IsNotification() {
		// consider a way to check for invalid params before running the notification.
		go handler(ctx, req.Params)
		return nil
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		switch e := err.(type) {
		case *Error:
			return NewErrorResp(req.Id, e)
		default:
			return NewErrorResp(req.Id, NewErrorTyped(ErrorCodeInternalError, err.Error(), nil))
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return NewErrorResp(req.Id, NewErrorTyped(ErrorCodeInternalError, err.Error(), nil))
	}

	return NewSuccessResp(req.Id, data)
}
