package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
)

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
}

type MethodHandler interface {
	HandleMethod(context.Context, Params) (any, error)
}

type HandlerFunc func(context.Context, Params) (any, error)

func (f HandlerFunc) HandleMethod(c context.Context, p Params) (any, error) {
	return f(c, p)
}

type Server struct {
	methods map[string]MethodHandler
}

func NewServer() *Server {
	return &Server{
		methods: make(map[string]MethodHandler),
	}
}

func (s *Server) Register(method string, handler MethodHandler) {
	s.methods[method] = handler
}

func (s *Server) RegisterFunc(method string, handler func(context.Context, Params) (any, error)) {
	s.Register(method, HandlerFunc(handler))
}

func (s *Server) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	handler, exists := s.methods[req.Method]
	if !exists {
		err := NewError(ErrorCodeMethodNotFound, "Method not found", nil).(*Error)
		return NewErrorResp(req.Id, err)
	}

	if req.IsNotification() {
		// consider a way to check for invalid params before running the notification.
		go handler.HandleMethod(ctx, req.Params)
		return nil
	}

	result, err := handler.HandleMethod(ctx, req.Params)
	if err != nil {
		var e *Error
		if ok := errors.As(err, e); ok {
			return NewErrorResp(req.Id, e)
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
