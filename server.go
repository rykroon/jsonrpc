package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Handler is the low-level dispatch contract: it takes the raw params bytes
// (possibly empty) and returns result bytes or an *Error. A nil result with a
// nil error is valid and encodes as `"result":null`. A method value —
// s.RegisterHandler(name, svc.Handle) — satisfies it without a cast, so
// stateful methods need no wrapper.
type Handler func(ctx context.Context, params jsontext.Value) (jsontext.Value, *Error)

type TypedHandler[P, R any] func(context.Context, P) (R, error)

// Middleware wraps a Handler to add cross-cutting behavior. It operates on
// the raw params, so it composes with typed and raw handlers alike. The first
// middleware in a chain is the outermost layer.
type Middleware func(next Handler) Handler

// RequestDecoder is the seam ServeMessage uses to turn bytes into a Request.
// Batch splitting happens above it, so a decoder always sees exactly one
// request object.
//
// Returning an *Error surfaces it to the client verbatim, giving the decoder
// full control of the code, message, and Data. Any other error is classified
// by the server — malformed JSON as a Parse error, everything else as an
// Invalid Request.
//
// DecodeRequest is the package default; install another with
// Server.SetRequestDecoder.
type RequestDecoder func(data jsontext.Value, req *Request) error

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
	decoder    RequestDecoder
}

func NewServer() *Server {
	return &Server{methods: map[string]Handler{}, decoder: DecodeRequest}
}

// Use appends server-wide middleware applied to every handler, outside any
// per-method middleware, with mw[0] outermost. Middleware is baked into each
// handler at registration time, so Use panics if any method is registered.
func (s *Server) Use(mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.methods) > 0 {
		panic("jsonrpc: Use must be called before registering methods")
	}
	s.middleware = append(s.middleware, mw...)
}

// SetRequestDecoder replaces the decoder ServeMessage uses to parse inbound
// messages, taking control of the errors reported for a malformed envelope.
// The default is DecodeRequest. Like Use, it must be called before any method
// is registered; it panics otherwise, or on a nil decoder.
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
}

// RegisterHandler installs h under name, wrapped with the per-method
// middleware (mw[0] outermost) and then the server-wide middleware. It panics
// if name is taken.
func (s *Server) RegisterHandler(name string, h Handler, mw ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.methods[name]; dup {
		panic(fmt.Sprintf("jsonrpc: method %q already registered", name))
	}
	s.methods[name] = chain(chain(h, mw), s.middleware)
}

// Register adapts fn with Typed and installs it under name. Equivalent to
// s.RegisterHandler(name, Typed(fn), mw...).
func (s *Server) Register[P, R any](name string, fn TypedHandler[P, R], mw ...Middleware) {
	s.RegisterHandler(name, Typed(fn), mw...)
}

// Serve dispatches a single request, returning nil for a notification — the
// handler still runs, but no reply is produced. Panics in handlers are not
// recovered; wrap Serve if your transport needs that.
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
		result = jsontext.Value("null")
	}
	return &Response{JSONRPC: Version, Result: result, ID: req.ID}
}

// ServeMessage parses data as a JSON-RPC message, dispatches it via Serve,
// and returns the marshaled response bytes; notifications produce (nil, nil).
// Use it from transports that work in raw JSON messages (WebSocket, stdio,
// TCP); HTTP adapters that prefer parse failures as HTTP 400 should call
// Serve directly.
//
// Batch messages (JSON arrays) are dispatched element by element, in order,
// and produce an array of responses in request order. A batch of only
// notifications produces (nil, nil), and an empty batch is an invalid request.
//
// JSON-RPC errors are returned in-band as a marshaled error Response; the
// error return is reserved for response marshaling failures.
//
// Each message is decoded by the server's RequestDecoder, DecodeRequest by
// default.
func (s *Server) ServeMessage(ctx context.Context, data jsontext.Value) (jsontext.Value, error) {
	if data.Kind() == '[' {
		return s.serveBatch(ctx, data)
	}
	var req Request
	if err := s.decode(data, &req); err != nil {
		return json.Marshal(errorResponse(recoveredID(&req), classifyDecodeError(err)))
	}
	resp := s.Serve(ctx, &req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

// serveBatch dispatches a batch sequentially, in order. Each element is
// decoded independently so one invalid element yields one error entry rather
// than failing the batch. The outer array split tolerates duplicate member
// names so a duplicate inside an element surfaces as that element's error.
func (s *Server) serveBatch(ctx context.Context, data jsontext.Value) (jsontext.Value, error) {
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems, jsontext.AllowDuplicateNames(true)); err != nil {
		return marshalMessageError(protocolError(CodeParseError, err.Error()))
	}
	if len(elems) == 0 {
		return marshalMessageError(protocolError(CodeInvalidRequest, "empty batch"))
	}
	responses := make([]*Response, 0, len(elems))
	for _, elem := range elems {
		var req Request
		if err := s.decode(elem, &req); err != nil {
			responses = append(responses, errorResponse(recoveredID(&req), classifyDecodeError(err)))
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

// decode runs the server's configured RequestDecoder.
func (s *Server) decode(data jsontext.Value, req *Request) error {
	s.mu.RLock()
	d := s.decoder
	s.mu.RUnlock()
	return d(data, req)
}

// DecodeRequest is the package's default RequestDecoder. It walks one request
// object token by token so every rejection carries a message this package
// wrote rather than one from the JSON library: an unrecognized envelope
// member, a non-string method, or non-structured params are Invalid Request,
// while malformed input is a Parse error.
//
// Duplicate member names are rejected anywhere, including inside params, since
// detection is tokenizer-level. Params content is otherwise not validated, so
// unknown members inside it are the handler's concern.
//
// Per the spec params must be an object or array whenever present, so null is
// rejected like any other scalar; a request with no parameters omits the
// member. Required members are not checked here — Serve rejects a missing
// method or wrong version on every path, including transports that build a
// Request themselves.
func DecodeRequest(data jsontext.Value, req *Request) error {
	d := jsontext.NewDecoder(bytes.NewReader(data))

	tok, err := d.ReadToken()
	if err != nil {
		return tokenError(err)
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return protocolError(CodeInvalidRequest, "request must be a JSON object")
	}

	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return tokenError(err)
		}
		// Token.String allocates a Go string, so the name stays valid across
		// the reads below; the Token itself does not.
		switch name := tok.String(); name {
		case "jsonrpc":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return protocolError(CodeInvalidRequest, "jsonrpc must be a string")
			}
			// Whether the version is "2.0" is Serve's verdict, not the
			// decoder's: failing here would discard an id we can read, and
			// Serve reaches the same conclusion with the id in hand.
			req.JSONRPC = tok.String()

		case "method":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return protocolError(CodeInvalidRequest, "method must be a string")
			}
			req.Method = tok.String()

		case "params":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			switch val.Kind() {
			case jsontext.KindBeginObject, jsontext.KindBeginArray:
				// ReadValue's buffer is only valid until the next read.
				req.Params = jsontext.Value(val.Clone())
			default:
				// Including null: the member is present, and null is not a
				// structured value. Omit params entirely to send none.
				return protocolError(CodeInvalidRequest, "params must be an object or array")
			}

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			req.ID = jsontext.Value(val.Clone())

		default:
			return protocolError(CodeInvalidRequest, "unknown member: "+name)
		}
	}

	// Consume the closing brace, then require the message to end there: a
	// Decoder reads a stream of top-level values, so a second one is trailing
	// garbage rather than a second request.
	if _, err := d.ReadToken(); err != nil {
		return tokenError(err)
	}
	if _, err := d.ReadToken(); !errors.Is(err, io.EOF) {
		if err != nil {
			return tokenError(err)
		}
		return protocolError(CodeParseError, "unexpected data after top-level value")
	}
	return nil
}

// tokenError maps a tokenizer failure to the spec's error codes: a duplicate
// member name is well-formed JSON violating uniqueness (Invalid Request),
// anything else is malformed input (Parse error).
func tokenError(err error) *Error {
	if errors.Is(err, jsontext.ErrDuplicateName) {
		return protocolError(CodeInvalidRequest, err.Error())
	}
	return protocolError(CodeParseError, err.Error())
}

// classifyDecodeError maps a decode failure to the spec's error codes. A
// decoder that classified its own failure wins outright; otherwise malformed
// JSON is a Parse error and everything else an Invalid Request.
func classifyDecodeError(err error) *Error {
	// A typed-nil *Error inside a non-nil error must not be surfaced as the
	// response error; fall through and classify it like any other failure.
	if e, ok := errors.AsType[*Error](err); ok && e != nil {
		return e
	}
	var syntaxErr *jsontext.SyntacticError
	if errors.As(err, &syntaxErr) && !errors.Is(err, jsontext.ErrDuplicateName) {
		return protocolError(CodeParseError, err.Error())
	}
	return protocolError(CodeInvalidRequest, err.Error())
}

func errorResponse(id jsontext.Value, e *Error) *Response {
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &Response{JSONRPC: Version, Error: e, ID: id}
}

// recoveredID returns the id a failed decode managed to read, or nil when
// there is none to trust. The spec requires a null id only when the id could
// not be detected, so echoing a detected one lets the client correlate.
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

func marshalMessageError(e *Error) (jsontext.Value, error) {
	return json.Marshal(&Response{
		JSONRPC: Version,
		Error:   e,
		ID:      jsontext.Value("null"),
	})
}
