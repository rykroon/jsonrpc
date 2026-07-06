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
// Write a plain function for a stateless method. For a method that carries
// state (a DB pool, a compiled JSON schema, a service object), register a
// method value — e.g. s.RegisterHandler(name, svc.Handle) — which captures
// the receiver and satisfies Handler without any cast.
type Handler func(ctx context.Context, params json.RawMessage) (json.RawMessage, *Error)

// Middleware wraps a Handler to add cross-cutting behavior (auth,
// logging, validation, etc.). It operates on the raw params, so it composes
// with both typed handlers (via Register) and raw handlers without touching
// the typed pipeline. The first middleware in a chain is the outermost layer.
type Middleware func(next Handler) Handler

// chain wraps h with mw, applying mw[0] outermost.
func chain(h Handler, mw []Middleware) Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Server is a registry of JSON-RPC methods that dispatches requests to them.
type Server struct {
	mu         sync.RWMutex
	methods    map[string]Handler
	middleware []Middleware
}

func NewServer() *Server {
	return &Server{methods: map[string]Handler{}}
}

// Use appends server-wide middleware applied to every handler, wrapping
// around any per-method middleware. The first middleware passed is the
// outermost layer.
//
// Use must be called before registering methods: middleware is baked into
// each handler at registration time, so Use has no effect on methods already
// registered. It panics if called after a method is registered.
func (s *Server) Use(mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: Use must be called before registering methods")
	}
	s.middleware = append(s.middleware, mw...)
}

// RegisterHandler installs h under name, wrapped with the given per-method
// middleware (mw[0] outermost) and then the server-wide middleware. It panics
// if name is already taken.
func (s *Server) RegisterHandler(name string, h Handler, mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q already registered", name))
	}
	s.methods[name] = chain(chain(h, mw), s.middleware)
}

// Serve dispatches a single request. For notifications the returned
// *Response is nil — the handler still runs, but no reply is produced.
//
// Serve does not recover from panics in registered handlers. If your
// transport requires recovery, wrap Serve.
func (s *Server) Serve(ctx context.Context, req *Request) *Response {
	// Validate the ID first so later error responses never echo an invalid ID.
	if !req.IsNotification() && !isValidID(req.ID) {
		return errorResponse(nil, NewError(CodeInvalidRequest, "id must be a string, number, or null"))
	}
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
		// The spec forbids replying to a notification, even when its method
		// is unknown.
		if req.IsNotification() {
			return nil
		}
		return errorResponse(req.ID, NewError(CodeMethodNotFound, "method not found: "+req.Method))
	}

	result, rpcErr := h(ctx, req.Params)
	if req.IsNotification() {
		return nil
	}
	if rpcErr != nil {
		return errorResponse(req.ID, rpcErr)
	}
	// A success response must carry a result member; omitempty would drop a
	// nil one, so encode it as JSON null.
	if len(result) == 0 {
		result = json.RawMessage("null")
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
