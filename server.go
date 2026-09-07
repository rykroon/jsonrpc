package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"
)

// Handler is a method implementation: it receives the decoded params P and
// returns a result R or an error. Server.Register installs one; Raw adapts one
// into a RawHandler.
type Handler[P, R any] func(context.Context, P) (R, error)

// RawHandler is the low-level dispatch contract: it takes the raw params bytes
// (possibly empty) and returns result bytes or an error. A nil result with a
// nil error encodes as `"result":null`. A method value satisfies it without a
// cast, so stateful methods need no wrapper.
//
// Returning an *Error classifies the failure and is sent as-is; any other error
// is left to the server's ErrorHandler, so handlers and Middleware can wrap
// with fmt.Errorf and %w.
type RawHandler func(ctx context.Context, params jsontext.Value) (jsontext.Value, error)

// Middleware wraps a RawHandler to add cross-cutting behavior. It operates on
// the raw layer, so it composes with typed and raw handlers alike. mw[0] is
// outermost.
type Middleware func(next RawHandler) RawHandler

// ErrorHandler has the last word on every error the server turns into a
// response: a failed decode, a handler's error, and the protocol failures
// Serve detects itself. It is the one place to sanitize messages, remap codes,
// attach Data, or log. Returning nil is a bug the server reports as an
// Internal error.
//
// err is often already an *Error, since components classify what they can
// (params that do not fit P are Invalid params, a bad envelope is a Parse or
// Invalid Request error); anything else is a failure no component claimed.
//
// req is the request being served, or nil when none could be decoded; it may
// be only partly populated when the decode failed. For a notification the
// returned *Error is discarded, but the handler still runs so the failure can
// be logged.
//
// The default is DefaultErrorHandler; install another with
// Server.SetErrorHandler.
type ErrorHandler func(ctx context.Context, req *Request, err error) *Error

// DefaultErrorHandler sends an *Error verbatim and turns anything else into an
// Internal error carrying the error's message. Since that reports an
// unclassified error's text to the client, servers that must not leak
// internals should install a handler that logs err and returns a fixed *Error.
func DefaultErrorHandler(_ context.Context, _ *Request, err error) *Error {
	if e, ok := errors.AsType[*Error](err); ok {
		// A typed-nil *Error must not read as success, and Error() on it panics.
		if e == nil {
			return NewError(CodeInternalError, "nil *jsonrpc.Error returned as an error")
		}
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
	// opts is the json.Options every JSON operation runs under: the envelope
	// decode, params, results, and responses. Nil means json/v2's defaults.
	opts json.Options
}

func NewServer() *Server {
	return &Server{
		methods:      map[string]RawHandler{},
		errorHandler: DefaultErrorHandler,
	}
}

// Use appends server-wide middleware applied to every handler, outside any
// per-method middleware, with mw[0] outermost. Middleware is baked into each
// handler at registration time, so Use panics once any method is registered.
func (s *Server) Use(mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: Use must be called before registering methods")
	}
	s.middleware = append(s.middleware, mw...)
}

// SetOptions installs json/v2 options applied to every JSON operation: the
// request envelope, params into a typed handler's P, its R, and responses. Use
// json.WithUnmarshalers and json.WithMarshalers to control how P and R appear
// on the wire; any other json or jsontext option is honored too. Later calls
// replace earlier ones.
//
// Options are captured into each handler at registration time, so like Use,
// SetOptions panics once any method is registered. For per-method options,
// adapt the handler with Raw and install it with RegisterRaw.
//
// The options are used as given. An unmarshaler for *Request or a marshaler
// for *Response replaces the type's own wire form, and is the way to take over
// the envelope; json/v2 holds it to the same one-value contract as any other.
func (s *Server) SetOptions(opts ...json.Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: SetOptions must be called before registering methods")
	}
	s.opts = json.JoinOptions(opts...)
}

// SetErrorHandler replaces the handler that turns an error into the *Error sent
// to the client. The default is DefaultErrorHandler. Like Use, it panics once
// any method is registered, or on a nil handler.
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

// handleError runs the ErrorHandler over err, substituting an *Error of its own
// if the handler returns nil.
func (s *Server) handleError(ctx context.Context, req *Request, err error) *Error {
	s.mu.RLock()
	h := s.errorHandler
	s.mu.RUnlock()
	if e := h(ctx, req, err); e != nil {
		return e
	}
	return NewError(CodeInternalError, "error handler returned a nil *jsonrpc.Error")
}

// RegisterRaw installs h under name, wrapped with the per-method middleware
// (mw[0] outermost) and then the server-wide middleware. It panics if name is
// taken.
func (s *Server) RegisterRaw(name string, h RawHandler, mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerRaw(name, h, mw)
}

// registerRaw is RegisterRaw with mu held.
func (s *Server) registerRaw(name string, h RawHandler, mw []Middleware) {
	if _, dup := s.methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q already registered", name))
	}
	s.methods[name] = chain(chain(h, mw), s.middleware)
}

// Register adapts fn with Raw, using the server's options, and installs it
// under name. Equivalent to s.RegisterRaw(name, Raw(fn, opts), mw...).
func (s *Server) Register[P, R any](name string, fn Handler[P, R], mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerRaw(name, Raw(fn, s.opts), mw)
}

// Serve dispatches a single request, returning nil for a notification — the
// handler still runs, but no reply is produced. Panics in handlers are not
// recovered; wrap Serve if your transport needs that.
func (s *Server) Serve(ctx context.Context, req *Request) *Response {
	// Validate the id first so later error responses never echo an invalid one.
	if !req.IsNotification() && !isValidID(req.ID) {
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeInvalidRequest, "id must be a string, number, or null")), nil)
	}
	if req.JSONRPC != Version {
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeInvalidRequest, `jsonrpc must be "2.0"`)), req.ID)
	}
	if req.Method == "" {
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeInvalidRequest, "missing method")), req.ID)
	}

	s.mu.RLock()
	h, ok := s.methods[req.Method]
	s.mu.RUnlock()
	if !ok {
		// The spec forbids replying to a notification, even an unknown method.
		if req.IsNotification() {
			return nil
		}
		return NewErrorResponse(s.handleError(ctx, req, NewError(CodeMethodNotFound, "method not found: "+req.Method)), req.ID)
	}

	result, err := h(ctx, req.Params)
	if req.IsNotification() {
		// No reply is allowed, but the ErrorHandler still runs so the failure
		// can be logged; its *Error is discarded.
		if err != nil {
			s.handleError(ctx, req, err)
		}
		return nil
	}
	if err != nil {
		return NewErrorResponse(s.handleError(ctx, req, err), req.ID)
	}
	// NewSuccessResponse turns a nil result into JSON null: the spec requires
	// the member on every success response.
	return NewSuccessResponse(result, req.ID)
}

// ServeMessage parses data as a JSON-RPC message, dispatches it via Serve, and
// returns the marshaled response bytes; notifications produce (nil, nil). Use
// it from transports that work in raw JSON messages (WebSocket, stdio, TCP);
// HTTP adapters that prefer parse failures as HTTP 400 should call Serve
// directly.
//
// Batch messages (JSON arrays) are dispatched element by element, in order. A
// batch of only notifications produces (nil, nil); an empty batch is an invalid
// request.
//
// JSON-RPC errors are returned in-band as a marshaled error Response; the error
// return is reserved for response marshaling failures.
func (s *Server) ServeMessage(ctx context.Context, data jsontext.Value) (jsontext.Value, error) {
	if data.Kind() == '[' {
		return s.serveBatch(ctx, data)
	}
	var req Request
	if err := s.decode(data, &req); err != nil {
		e := s.handleError(ctx, &req, classifyDecodeError(err))
		return s.marshalMessageError(e, recoveredID(&req))
	}
	resp := s.Serve(ctx, &req)
	if resp == nil {
		return nil, nil
	}
	return marshalResponse(resp, s.options())
}

// serveBatch dispatches a batch sequentially. Each element is decoded
// independently so one invalid element yields one error entry rather than
// failing the batch. The outer array split tolerates duplicate member names so
// a duplicate inside an element surfaces as that element's error.
func (s *Server) serveBatch(ctx context.Context, data jsontext.Value) (jsontext.Value, error) {
	opts := s.options()
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems, json.JoinOptions(opts, jsontext.AllowDuplicateNames(true))); err != nil {
		return s.marshalMessageError(s.handleError(ctx, nil, NewError(CodeParseError, err.Error())), nil)
	}
	if len(elems) == 0 {
		return s.marshalMessageError(s.handleError(ctx, nil, NewError(CodeInvalidRequest, "empty batch")), nil)
	}
	responses := make([]*Response, 0, len(elems))
	for _, elem := range elems {
		var req Request
		if err := s.decode(elem, &req); err != nil {
			e := s.handleError(ctx, &req, classifyDecodeError(err))
			responses = append(responses, NewErrorResponse(e, recoveredID(&req)))
			continue
		}
		if resp := s.Serve(ctx, &req); resp != nil {
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

// classifyDecodeError maps a decode failure to the spec's codes. An *Error from
// an unmarshaler wins outright; otherwise malformed JSON is a Parse error and
// everything else an Invalid Request. The ErrorHandler still has the final say.
func classifyDecodeError(err error) *Error {
	if e, ok := errors.AsType[*Error](err); ok {
		// A typed-nil *Error must not be surfaced, and Error() on it panics.
		if e == nil {
			return NewError(CodeInvalidRequest, "nil *jsonrpc.Error returned as an error")
		}
		return e
	}
	var syntaxErr *jsontext.SyntacticError
	if errors.As(err, &syntaxErr) && !errors.Is(err, jsontext.ErrDuplicateName) {
		return NewError(CodeParseError, err.Error())
	}
	// json/v2 wraps an unmarshaler's error in a *SemanticError naming the Go
	// type. That framing is noise to a client, so report the underlying error.
	if se, ok := errors.AsType[*json.SemanticError](err); ok && se.Err != nil {
		err = se.Err
	}
	return NewError(CodeInvalidRequest, err.Error())
}

// recoveredID returns the id a failed decode managed to read, or nil when there
// is none to trust. The spec requires a null id only when none was detected, so
// echoing a detected one lets the client correlate.
func recoveredID(req *Request) jsontext.Value {
	if isValidID(req.ID) {
		return req.ID
	}
	return nil
}

// isValidID reports whether id is a JSON string, number, or null. The spec
// discourages null and non-integer numbers but does not forbid them.
func isValidID(id jsontext.Value) bool {
	switch id.Kind() {
	case '"', '0', 'n': // string, any number, null
		return true
	}
	return false
}

// marshalMessageError writes the error response ServeMessage produces when a
// message never reaches Serve.
func (s *Server) marshalMessageError(e *Error, id jsontext.Value) (jsontext.Value, error) {
	return marshalResponse(NewErrorResponse(e, id), s.options())
}

// marshalResponse writes one response, or a batch of them, under opts. A
// failure yields no bytes at all rather than json.Marshal's partial buffer:
// there is nothing usable to send, and the caller reports the error instead.
func marshalResponse(v any, opts json.Options) (jsontext.Value, error) {
	out, err := json.Marshal(v, opts)
	if err != nil {
		return nil, err
	}
	return out, nil
}
