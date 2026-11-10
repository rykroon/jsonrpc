package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"
)

// Handler is a typed method: decoded params P in, result R or an error out.
type Handler[P, R any] func(context.Context, P) (R, error)

// RawHandler is the dispatch contract: undecoded params (zero when absent) in,
// result bytes or an error out. A nil result encodes as `"result":null`.
//
// An Error is sent as-is; any other error goes to the ErrorHandler, so
// wrapping with %w is fine.
type RawHandler func(ctx context.Context, params Params) (jsontext.Value, error)

// Middleware wraps a RawHandler. mw[0] is outermost.
type Middleware func(next RawHandler) RawHandler

// ErrorHandler turns every error the server reports — a failed decode, a
// handler's error, a protocol failure Serve detects — into the Error sent
// back.
//
// err is often already an Error (Invalid params, Parse, Invalid Request);
// anything else is unclassified. req is nil when nothing could be decoded,
// and may be partial after a failed decode. For a notification the result is
// discarded, but the handler still runs so the failure can be logged.
// Changes to req do not reach the response.
type ErrorHandler func(ctx context.Context, req Request, err error) Error

// DefaultErrorHandler sends an Error verbatim and reports anything else as an
// Internal error carrying its message. Servers that must not leak internals
// install their own.
func DefaultErrorHandler(_ context.Context, _ Request, err error) Error {
	if e, ok := errors.AsType[Error](err); ok {
		return e
	}
	return NewError(CodeInternalError, err.Error())
}

// chain wraps h with mw, applying mw[0] outermost.
func chain(h RawHandler, mw []Middleware) RawHandler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Server is a registry of JSON-RPC methods that dispatches requests to them.
type Server struct {
	mu           sync.RWMutex
	methods      map[string]RawHandler
	middleware   []Middleware
	errorHandler ErrorHandler
	// opts governs every JSON operation. Nil means json/v2's defaults.
	opts json.Options
}

func NewServer() *Server {
	return &Server{
		methods:      map[string]RawHandler{},
		errorHandler: DefaultErrorHandler,
	}
}

// Use appends server-wide middleware, outside any per-method middleware.
// Panics once any method is registered.
func (s *Server) Use(mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: Use must be called before registering methods")
	}
	s.middleware = append(s.middleware, mw...)
}

// SetOptions installs json/v2 options for every JSON operation: the envelope,
// params into P, R, and responses. Later calls replace earlier ones. Panics
// once any method is registered; for one method use Raw(fn, opts) with
// RegisterRaw.
//
// The options are used as given, so an unmarshaler for *Request or a
// marshaler for *Response replaces the envelope's own wire form.
func (s *Server) SetOptions(opts ...json.Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: SetOptions must be called before registering methods")
	}
	s.opts = json.JoinOptions(opts...)
}

// SetErrorHandler replaces DefaultErrorHandler. Panics once any method is
// registered, or on nil.
func (s *Server) SetErrorHandler(h ErrorHandler) {
	if h == nil {
		panic("jsonrpc: SetErrorHandler requires a non-nil handler")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: SetErrorHandler must be called before registering methods")
	}
	s.errorHandler = h
}

// handleError runs the ErrorHandler.
func (s *Server) handleError(ctx context.Context, req Request, err error) Error {
	s.mu.RLock()
	h := s.errorHandler
	s.mu.RUnlock()
	return h(ctx, req, err)
}

// RegisterRaw installs h under name, wrapped with mw (mw[0] outermost) and then
// the server-wide middleware. Panics if name is taken.
func (s *Server) RegisterRaw(name string, h RawHandler, mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerRaw(name, h, mw)
}

// registerRaw is RegisterRaw with mu held.
func (s *Server) registerRaw(name string, h RawHandler, mw []Middleware) {
	// Serve rejects an empty method before the lookup, so such a handler could
	// never run.
	if name == "" {
		panic("jsonrpc: method name must not be empty")
	}
	if _, dup := s.methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q already registered", name))
	}
	s.methods[name] = chain(chain(h, mw), s.middleware)
}

// Register installs Raw(fn, s.opts) under name; see RegisterRaw.
func (s *Server) Register[P, R any](name string, fn Handler[P, R], mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerRaw(name, Raw(fn, s.opts), mw)
}

// Serve dispatches one request. A notification runs but returns the zero
// Response. Handler panics are not recovered.
func (s *Server) Serve(ctx context.Context, req Request) Response {
	// Read once, so an ErrorHandler that modifies req cannot change the reply's id.
	id := req.ID()
	if req.JSONRPC() != Version {
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeInvalidRequest, `jsonrpc must be "2.0"`)), id)
	}
	if req.Method() == "" {
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeInvalidRequest, "missing method")), id)
	}

	s.mu.RLock()
	h, ok := s.methods[req.Method()]
	s.mu.RUnlock()
	if !ok {
		// The spec forbids replying to a notification, even an unknown method.
		if req.IsNotification() {
			return Response{}
		}
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeMethodNotFound, "method not found: "+req.Method())), id)
	}

	result, err := h(ctx, req.Params())
	if req.IsNotification() {
		// No reply, but the ErrorHandler still runs so it can log.
		if err != nil {
			s.handleError(ctx, req, err)
		}
		return Response{}
	}
	if err != nil {
		return NewErrorResponse(s.handleError(ctx, req, err), id)
	}
	// A nil result becomes JSON null; the spec requires the member.
	return NewSuccessResponse(result, id)
}

// ServeMessage parses one JSON-RPC message, dispatches it, and returns the
// response bytes; a notification yields (nil, nil). Batches are dispatched
// element by element; one of only notifications yields (nil, nil), and an
// empty one is an Invalid Request. JSON-RPC errors are returned in-band; the
// error return is for response marshaling failures.
func (s *Server) ServeMessage(ctx context.Context, data jsontext.Value) (jsontext.Value, error) {
	if data.Kind() == '[' {
		return s.serveBatch(ctx, data)
	}
	var req Request
	if err := s.decode(data, &req); err != nil {
		id := req.ID()
		e := s.handleError(ctx, req, classifyDecodeError(err))
		// An id read before the failure lets the client correlate.
		return s.marshalMessageError(e, id)
	}
	resp := s.Serve(ctx, req)
	if resp.IsZero() {
		return nil, nil
	}
	return marshalResponse(resp, s.options())
}

// serveBatch decodes each element independently, so one bad element yields one
// error entry. The array split tolerates duplicate names so a duplicate inside
// an element is that element's error.
func (s *Server) serveBatch(ctx context.Context, data jsontext.Value) (jsontext.Value, error) {
	opts := s.options()
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems, json.JoinOptions(opts, jsontext.AllowDuplicateNames(true))); err != nil {
		return s.marshalMessageError(s.handleError(ctx, Request{}, NewError(CodeParseError, err.Error())), ID{})
	}
	if len(elems) == 0 {
		return s.marshalMessageError(s.handleError(ctx, Request{}, NewError(CodeInvalidRequest, "empty batch")), ID{})
	}
	responses := make([]Response, 0, len(elems))
	for _, elem := range elems {
		var req Request
		if err := s.decode(elem, &req); err != nil {
			id := req.ID()
			e := s.handleError(ctx, req, classifyDecodeError(err))
			responses = append(responses, NewErrorResponse(e, id))
			continue
		}
		if resp := s.Serve(ctx, req); !resp.IsZero() {
			responses = append(responses, resp)
		}
	}
	if len(responses) == 0 {
		return nil, nil // all notifications: no reply at all, not an empty array
	}
	return marshalResponse(responses, opts)
}

// options returns the server's current json.Options.
func (s *Server) options() json.Options {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.opts
}

// decode reads one message under the server's options.
func (s *Server) decode(data jsontext.Value, req *Request) error {
	return json.Unmarshal(data, req, s.options())
}

// classifyDecodeError: an Error wins; malformed JSON is a Parse error; the
// rest is Invalid Request.
func classifyDecodeError(err error) Error {
	if e, ok := errors.AsType[Error](err); ok {
		return e
	}
	var syntaxErr *jsontext.SyntacticError
	if errors.As(err, &syntaxErr) && !errors.Is(err, jsontext.ErrDuplicateName) {
		return NewError(CodeParseError, err.Error())
	}
	// Drop json/v2's *SemanticError framing; the client wants the cause.
	if se, ok := errors.AsType[*json.SemanticError](err); ok && se.Err != nil {
		err = se.Err
	}
	return NewError(CodeInvalidRequest, err.Error())
}

// marshalMessageError writes the error for a message that never reached Serve.
func (s *Server) marshalMessageError(e Error, id ID) (jsontext.Value, error) {
	return marshalResponse(NewErrorResponse(e, id), s.options())
}

// marshalResponse returns no bytes on failure rather than a partial buffer.
func marshalResponse(v any, opts json.Options) (jsontext.Value, error) {
	out, err := json.Marshal(v, opts)
	if err != nil {
		return nil, err
	}
	return out, nil
}
