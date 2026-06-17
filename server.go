package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
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

// Register adapts a typed function into a Handler and installs it on s.
// Params are JSON-unmarshaled into a fresh P; the result is JSON-marshaled.
// If fn returns an error that unwraps to *jsonrpc.Error it is returned as-is;
// any other error becomes CodeInternalError.
func Register[P, R any](s *Server, name string, fn func(context.Context, P) (R, error)) {
	s.RegisterHandler(name, func(ctx context.Context, raw json.RawMessage) (json.RawMessage, *Error) {
		var p P
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, NewError(CodeInvalidParams, err.Error())
			}
		}
		r, err := fn(ctx, p)
		if err != nil {
			var rpcErr *Error
			if errors.As(err, &rpcErr) {
				return nil, rpcErr
			}
			return nil, NewError(CodeInternalError, err.Error())
		}
		out, mErr := json.Marshal(r)
		if mErr != nil {
			return nil, NewError(CodeInternalError, mErr.Error())
		}
		return out, nil
	})
}
