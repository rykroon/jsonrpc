package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
}

type HandlerFunc func(ctx context.Context, params *Params) (any, error)

type Server struct {
	methods map[string]HandlerFunc
}

func NewServer() *Server {
	return &Server{
		methods: make(map[string]HandlerFunc),
	}
}

func (s *Server) Register(method string, handler HandlerFunc) {
	s.methods[method] = handler
}

func (s *Server) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	handler, exists := s.methods[req.Method]
	if !exists {
		err := NewError(ErrorCodeMethodNotFound, "Method not found", nil).(*Error)
		return NewErrorResp(req.Id, err)
	}

	if req.IsNotification() {
		// consider a way to check for invalid params before running the notification.
		go handler(ctx, req.Params)
		return nil
	}

	result, err := handler(ctx, req.Params)
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
		err = fmt.Errorf("failed to marshal result: %w", err)
		jsonrpcErr := NewError(ErrorCodeInternalError, err.Error(), nil).(*Error)
		return NewErrorResp(req.Id, jsonrpcErr)
	}

	return NewSuccessResp(req.Id, data)
}
