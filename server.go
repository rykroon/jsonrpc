package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Handler is the low-level dispatch signature. It receives the raw params
// bytes (possibly empty) and returns either result bytes or an *Error.
// A nil result with a nil error is valid and encodes as `"result":null`.
type Handler func(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error)

// Server is a registry of JSON-RPC methods that dispatches requests to them.
type Server struct {
	mu      sync.RWMutex
	methods map[string]Handler
}

func NewServer() *Server {
	return &Server{methods: map[string]Handler{}}
}

// RegisterHandler installs h under name. It panics if name is already taken.
func (s *Server) RegisterHandler(name string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q already registered", name))
	}
	s.methods[name] = h
}

// Serve dispatches a single request. For notifications the returned
// *Response is nil — the handler still runs, but no reply is produced.
//
// Serve does not recover from panics in registered handlers. If your
// transport requires recovery, wrap Serve.
func (s *Server) Serve(ctx context.Context, req *Request) *Response {
	if req.JSONRPC != Version {
		return errorResponse(req.ID, NewError(CodeInvalidRequest, `jsonrpc must be "2.0"`))
	}
	if req.Method == "" {
		return errorResponse(req.ID, NewError(CodeInvalidRequest, "missing method"))
	}

	s.mu.RLock()
	h, ok := s.methods[req.Method]
	s.mu.RUnlock()
	if !ok {
		return errorResponse(req.ID, NewError(CodeMethodNotFound, "method not found: "+req.Method))
	}

	result, rpcErr := h(ctx, req.Params)
	if req.IsNotification() {
		return nil
	}
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr)
	}
	return &Response{JSONRPC: Version, Result: result, ID: req.ID}
}

func errorResponse(id json.RawMessage, e *Error) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: Version, Error: e, ID: id}
}
