package jsonrpc

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
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

type TypedHandler[P, R any] func(context.Context, P) (R, error)

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

// Register adapts a typed function into a Handler with Typed and installs
// it on the server, wrapped with the given per-method middleware (mw[0]
// outermost) followed by the server-wide middleware.
//
// Equivalent to s.RegisterHandler(name, Typed(fn), mw...).
func (s *Server) Register[P, R any](name string, fn TypedHandler[P, R], mw ...Middleware) {
	s.RegisterHandler(name, Typed(fn), mw...)
}

// Serve dispatches a single request. For notifications the returned
// *Response is nil — the handler still runs, but no reply is produced.
//
// Serve does not recover from panics in registered handlers. If your
// transport requires recovery, wrap Serve.
func (s *Server) Serve(ctx context.Context, req *Request) *Response {
	// Validate the ID first so later error responses never echo an invalid ID.
	if !req.IsNotification() && !isValidID(req.ID) {
		return errorResponse(nil, protocolError(CodeInvalidRequest, "id must be a string, number, or null"))
	}
	if req.JSONRPC != Version {
		return errorResponse(req.ID, protocolError(CodeInvalidRequest, `jsonrpc must be "2.0"`))
	}
	if req.Method == "" {
		return errorResponse(req.ID, protocolError(CodeInvalidRequest, "missing method"))
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
		return errorResponse(req.ID, protocolError(CodeMethodNotFound, "method not found: "+req.Method))
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
// Batch messages (JSON arrays) are dispatched element by element, in
// order, and produce a JSON array of the responses in request order. A
// batch of only notifications produces (nil, nil), and an empty batch is
// an invalid request.
//
// JSON-RPC errors (parse errors, invalid request, etc.) are returned
// in-band as a marshaled error Response, not as the error return. The
// error return is reserved for response marshaling failures, which should
// not occur in normal operation.
//
// Decoding is strict: messages with duplicate object member names anywhere,
// or with unrecognized members on the request envelope itself, are rejected
// as Invalid Request. Params content is not inspected here — unknown members
// inside params are the handler's concern.
func (s *Server) ServeMessage(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
	if data.Kind() == '[' {
		return s.serveBatch(ctx, data)
	}
	var req Request
	if err := decodeRequest(data, &req); err != nil {
		return marshalMessageError(classifyDecodeError(err))
	}
	resp := s.Serve(ctx, &req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

// serveBatch dispatches a batch message sequentially, in order. Each element
// is unmarshaled independently so one invalid element yields one error entry
// without failing the rest of the batch. The outer array split tolerates
// duplicate member names so that a duplicate inside one element surfaces as
// that element's error, not a whole-batch failure; each element is then
// decoded strictly.
func (s *Server) serveBatch(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
	var elems []json.RawMessage
	if err := jsonv2.Unmarshal(data, &elems, jsontext.AllowDuplicateNames(true)); err != nil {
		return marshalMessageError(protocolError(CodeParseError, err.Error()))
	}
	if len(elems) == 0 {
		return marshalMessageError(protocolError(CodeInvalidRequest, "empty batch"))
	}
	responses := make([]*Response, 0, len(elems))
	for _, elem := range elems {
		var req Request
		if err := decodeRequest(elem, &req); err != nil {
			responses = append(responses, errorResponse(nil, classifyDecodeError(err)))
			continue
		}
		if resp := s.Serve(ctx, &req); resp != nil {
			responses = append(responses, resp)
		}
	}
	if len(responses) == 0 {
		return nil, nil // all notifications: no reply at all, not an empty array
	}
	return json.Marshal(responses)
}

// decodeRequest unmarshals one request object with the inbound strictness
// ServeMessage promises: duplicate member names are rejected (including
// inside params — detection is tokenizer-level), and unknown envelope
// members are rejected. Params content lands in a RawMessage, so its
// members are otherwise not validated here.
func decodeRequest(data json.RawMessage, req *Request) error {
	return jsonv2.Unmarshal(data, req, jsonv2.RejectUnknownMembers(true))
}

// classifyDecodeError maps a decode failure to the spec's error codes:
// malformed JSON is a Parse error, while everything else — wrong shape,
// duplicate member names (well-formed JSON that violates uniqueness),
// unknown envelope members — is an Invalid Request.
func classifyDecodeError(err error) *Error {
	var syntaxErr *jsontext.SyntacticError
	if errors.As(err, &syntaxErr) && !errors.Is(err, jsontext.ErrDuplicateName) {
		return protocolError(CodeParseError, err.Error())
	}
	return protocolError(CodeInvalidRequest, err.Error())
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
func isValidID(id json.RawMessage) bool {
	switch id.Kind() {
	case '"', '0', 'n': // string, any number, null
		return true
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
