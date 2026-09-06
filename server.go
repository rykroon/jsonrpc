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

// RequestDecoder overrides Request.UnmarshalJSONFrom for one server, and can
// delegate to it. Batch splitting happens above it, so a decoder always sees
// exactly one request object.
//
// The signature is json.UnmarshalFromFunc's, and SetRequestDecoder installs a
// decoder as exactly that: json/v2's unmarshaler for a *Request. A decoder must
// therefore read exactly one JSON value from d — one that ignores the input
// still calls d.SkipValue — and in return inherits json/v2's machinery,
// including the rejection of trailing data.
//
// Returning an *Error gives the decoder full control of the code, message, and
// Data. Any other error is classified by the server: malformed JSON as a Parse
// error, everything else as an Invalid Request.
type RequestDecoder func(d *jsontext.Decoder, req *Request) error

// ResponseEncoder is the counterpart to RequestDecoder: it overrides
// Response.MarshalJSONTo for one server, deciding the wire form of every
// response sent, and can delegate to it. The signature is json.MarshalToFunc's,
// so an encoder must write exactly one JSON value to enc.
//
// An error from an encoder is not a JSON-RPC error — there is no response left
// to report it in — so it surfaces as ServeMessage's error return.
type ResponseEncoder func(enc *jsontext.Encoder, resp *Response) error

// chain wraps h with mw, applying mw[0] outermost.
func chain(h RawHandler, mw []Middleware) RawHandler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Server is a registry of JSON-RPC methods that dispatches requests to them.
type Server struct {
	mu         sync.RWMutex
	methods    map[string]RawHandler
	middleware []Middleware
	// decoder, encoder and userOpts are the inputs to opts, kept so any setter
	// can rebuild it without disturbing the others. Nil means none was
	// installed.
	decoder      RequestDecoder
	encoder      ResponseEncoder
	errorHandler ErrorHandler
	userOpts     json.Options
	// opts is the single json.Options used for every JSON operation: a
	// RequestDecoder and ResponseEncoder ride in it as the unmarshaler for a
	// *Request and the marshaler for a *Response, alongside whatever SetOptions
	// installed.
	opts json.Options
}

func NewServer() *Server {
	s := &Server{
		methods:      map[string]RawHandler{},
		errorHandler: DefaultErrorHandler,
	}
	s.rebuildOptions()
	return s
}

// rebuildOptions derives opts from decoder, encoder and userOpts, keeping
// construction off the request path. The caller holds mu. With neither
// installed, opts is exactly what SetOptions was given, leaving Request and
// Response to decode and encode themselves.
//
// json.JoinOptions lets a later WithUnmarshalers replace an earlier one, so a
// decoder is joined with the user's unmarshalers rather than layered over them,
// and the same for an encoder. The setter's function comes first, so it wins
// for its type.
func (s *Server) rebuildOptions() {
	opts := []json.Options{}
	if s.userOpts != nil {
		opts = append(opts, s.userOpts)
	}
	if s.decoder != nil {
		us := json.UnmarshalFromFunc(s.decoder)
		if u, ok := json.GetOption(s.userOpts, json.WithUnmarshalers); ok && u != nil {
			us = json.JoinUnmarshalers(us, u)
		}
		opts = append(opts, json.WithUnmarshalers(us))
	}
	if s.encoder != nil {
		ms := json.MarshalToFunc(s.encoder)
		if m, ok := json.GetOption(s.userOpts, json.WithMarshalers); ok && m != nil {
			ms = json.JoinMarshalers(ms, m)
		}
		opts = append(opts, json.WithMarshalers(ms))
	}
	s.opts = json.JoinOptions(opts...)
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
// A RequestDecoder or ResponseEncoder rides in the same options, outranking
// anything set here for *Request or *Response.
func (s *Server) SetOptions(opts ...json.Options) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: SetOptions must be called before registering methods")
	}
	s.userOpts = json.JoinOptions(opts...)
	s.rebuildOptions()
}

// SetRequestDecoder installs a decoder for inbound messages, taking control of
// the errors reported for a malformed envelope. Absent one, a Request decodes
// itself. Like Use, it panics once any method is registered, or on a nil
// decoder.
//
// Shorthand for SetOptions(json.WithUnmarshalers(json.UnmarshalFromFunc(d))),
// except that the decoder outranks any *Request unmarshaler set there.
func (s *Server) SetRequestDecoder(d RequestDecoder) {
	if d == nil {
		panic("jsonrpc: SetRequestDecoder requires a non-nil decoder")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: SetRequestDecoder must be called before registering methods")
	}
	s.decoder = d
	s.rebuildOptions()
}

// SetResponseEncoder installs an encoder for outbound responses, taking control
// of their wire form. Absent one, a Response writes itself. Like Use, it panics
// once any method is registered, or on a nil encoder.
//
// Shorthand for SetOptions(json.WithMarshalers(json.MarshalToFunc(e))), except
// that the encoder outranks any *Response marshaler set there.
func (s *Server) SetResponseEncoder(e ResponseEncoder) {
	if e == nil {
		panic("jsonrpc: SetResponseEncoder requires a non-nil encoder")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: SetResponseEncoder must be called before registering methods")
	}
	s.encoder = e
	s.rebuildOptions()
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

// decode runs the server's RequestDecoder over one message. json/v2 drives it,
// so the one-value contract and the trailing-data check are enforced by the
// same code that enforces them for any other unmarshaler.
func (s *Server) decode(data jsontext.Value, req *Request) error {
	return json.Unmarshal(data, req, s.options())
}

// classifyDecodeError maps a decode failure to the spec's codes. A decoder that
// classified its own failure wins outright; otherwise malformed JSON is a Parse
// error and everything else an Invalid Request. The ErrorHandler still has the
// final say.
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
	// type. That framing is noise to a client, so report what the decoder said.
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
