package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
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

// RequestDecoder decodes one JSON-RPC request message into req. It is the
// seam ServeMessage uses to turn bytes into a Request; batch splitting happens
// above it, so a decoder always sees exactly one request object.
//
// Returning an *Error surfaces that error to the client verbatim, which is how
// a decoder takes full control of the codes, messages, and Data it reports.
// Returning any other error lets the server classify it — malformed JSON as a
// Parse error, everything else as an Invalid Request — so a decoder that only
// forwards an Unmarshal failure still behaves correctly.
//
// DecodeRequest is the package default. Install another with
// Server.SetRequestDecoder.
type RequestDecoder func(data json.RawMessage, req *Request) error

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

// SetRequestDecoder replaces the decoder ServeMessage uses to parse inbound
// messages, letting a caller control the errors reported for a malformed or
// non-conforming request envelope. The default is DecodeRequest.
//
// Like Use, it is setup-time configuration and must be called before any
// method is registered; it panics otherwise, and panics on a nil decoder.
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
// Each message is decoded by the server's RequestDecoder — DecodeRequest
// unless SetRequestDecoder installed another. The default is strict:
// messages with duplicate object member names anywhere, or with unrecognized
// members on the request envelope itself, are rejected as Invalid Request.
// Params content is not inspected here — unknown members inside params are
// the handler's concern.
func (s *Server) ServeMessage(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
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

// serveBatch dispatches a batch message sequentially, in order. Each element
// is decoded independently so one invalid element yields one error entry
// without failing the rest of the batch. The outer array split tolerates
// duplicate member names so that a duplicate inside one element surfaces as
// that element's error, not a whole-batch failure; each element then goes
// through the server's RequestDecoder.
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
func (s *Server) decode(data json.RawMessage, req *Request) error {
	s.mu.RLock()
	d := s.decoder
	s.mu.RUnlock()
	return d(data, req)
}

// DecodeRequest is the package's default RequestDecoder. It walks one request
// object token by token so that every rejection carries a message this package
// wrote rather than one from the JSON library: an unrecognized envelope member,
// a wrong "jsonrpc" version, a non-string method, or params that are not a
// structured value are all reported as Invalid Request, while malformed input
// is a Parse error.
//
// Decoding is strict in the ways ServeMessage promises. Duplicate member names
// are rejected anywhere in the message, including inside params — detection is
// tokenizer-level. Unrecognized envelope members are rejected. Params content
// is otherwise not validated; it lands in a RawMessage for the handler to
// decode, so unknown members inside it are tolerated.
//
// Per the spec params must be a structured value — an object or an array —
// whenever the member is present, so null is rejected along with every other
// scalar; a request with no parameters omits the member entirely. Required
// members are deliberately not checked here — Serve rejects a missing method
// or a wrong version on every path, including transports that build a Request
// themselves.
func DecodeRequest(data json.RawMessage, req *Request) error {
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
				req.Params = json.RawMessage(val.Clone())
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
			req.ID = json.RawMessage(val.Clone())

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
// member name is well-formed JSON that violates uniqueness (Invalid Request),
// while anything else — a syntax error, a truncated or empty message — is
// malformed input (Parse error).
func tokenError(err error) *Error {
	if errors.Is(err, jsontext.ErrDuplicateName) {
		return protocolError(CodeInvalidRequest, err.Error())
	}
	return protocolError(CodeParseError, err.Error())
}

// classifyDecodeError maps a decode failure to the spec's error codes. A
// decoder that already classified its own failure wins outright; otherwise
// malformed JSON is a Parse error, and everything else — wrong shape,
// duplicate member names (well-formed JSON that violates uniqueness),
// unknown envelope members — is an Invalid Request.
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

func errorResponse(id json.RawMessage, e *Error) *Response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &Response{JSONRPC: Version, Error: e, ID: id}
}

// recoveredID returns the id a failed decode managed to read, or nil when
// there is none to trust. The spec requires a null id only when the id could
// not be detected, so echoing one we did detect lets the client correlate the
// error with the call that caused it.
func recoveredID(req *Request) json.RawMessage {
	if isValidID(req.ID) {
		return req.ID
	}
	return nil
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
