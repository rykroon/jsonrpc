package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Handler is the low-level dispatch contract. It receives the raw params
// bytes (possibly empty) and returns either result bytes or an *Error.
// A nil result with a nil error is valid and encodes as `"result":null`.
//
// Implement Handler on a struct when the handler carries state (e.g. a
// compiled JSON schema, a rate limiter, a cache). For a plain function,
// use HandlerFunc.
type Handler interface {
	Handle(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error)
}

// HandlerFunc adapts a plain function into a Handler. The pattern mirrors
// net/http's Handler / HandlerFunc.
type HandlerFunc func(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error)

func (f HandlerFunc) Handle(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error) {
	return f(ctx, params)
}

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
	if !req.IsNotification() && !isValidID(req.ID) {
		return errorResponse(nil, NewError(CodeInvalidRequest, "id must be a string, number, or null"))
	}

	s.mu.RLock()
	h, ok := s.methods[req.Method]
	s.mu.RUnlock()
	if !ok {
		return errorResponse(req.ID, NewError(CodeMethodNotFound, "method not found: "+req.Method))
	}

	result, rpcErr := h.Handle(ctx, req.Params)
	if req.IsNotification() {
		return nil
	}
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr)
	}
	return &Response{JSONRPC: Version, Result: result, ID: req.ID}
}

// ServeMessage parses data as a JSON-RPC message, dispatches it via Serve,
// and returns the marshaled response bytes. Notifications produce (nil, nil)
// — there is no reply to send.
//
// Use ServeMessage from transports that work in raw JSON messages
// (WebSocket, stdio, TCP). HTTP adapters that prefer to surface parse
// failures as HTTP 400 should call Serve directly instead.
//
// Currently single requests only — batch messages (JSON arrays) are
// rejected with CodeInvalidRequest. Batch support is planned.
//
// JSON-RPC errors (parse errors, invalid request, etc.) are returned
// in-band as a marshaled error Response, not as the error return. The
// error return is reserved for response marshaling failures, which should
// not occur in normal operation.
func (s *Server) ServeMessage(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
	if isJSONArray(data) {
		return marshalMessageError(NewError(CodeInvalidRequest, "batch requests are not supported"))
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return marshalMessageError(NewError(CodeParseError, err.Error()))
		}
		return marshalMessageError(NewError(CodeInvalidRequest, err.Error()))
	}
	resp := s.Serve(ctx, &req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

func errorResponse(id json.RawMessage, e *Error) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: Version, Error: e, ID: id}
}

// isValidID reports whether id is a JSON string, number, or null. JSON
// bools, objects, and arrays are rejected. The spec discourages null and
// non-integer numbers but does not forbid them, so we allow both.
//
// Assumes id is well-formed JSON (which it is when sourced from json.Unmarshal).
// We're recognizing the token shape by its first byte, not parsing the value.
func isValidID(id json.RawMessage) bool {
	id = bytes.TrimSpace(id)
	if len(id) == 0 {
		return false
	}
	c := id[0]
	return c == '"' || c == '-' || c == 'n' || (c >= '0' && c <= '9')
}

func isJSONArray(data []byte) bool {
	for _, b := range data {
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		return b == '['
	}
	return false
}

func marshalMessageError(e *Error) (json.RawMessage, error) {
	return json.Marshal(&Response{
		JSONRPC: Version,
		Error:   e,
		ID:      json.RawMessage("null"),
	})
}
