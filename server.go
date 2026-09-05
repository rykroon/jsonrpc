package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
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

// RequestDecoder is the seam ServeMessage uses to turn a message into a
// Request. Batch splitting happens above it, so a decoder always sees exactly
// one request object.
//
// The signature is json.UnmarshalFromFunc's, and the server installs a
// decoder as exactly that: the unmarshaler json/v2 uses for a *Request. So a
// decoder must read exactly one JSON value from d, the contract json/v2
// states for UnmarshalerFrom — one that ignores the input still calls
// d.SkipValue — and in return it inherits that machinery, including the
// rejection of anything following the request object.
//
// Returning an *Error surfaces it to the client verbatim, giving the decoder
// full control of the code, message, and Data. Any other error is classified
// by the server — malformed JSON as a Parse error, everything else as an
// Invalid Request.
//
// DecodeRequest is the package default; install another with
// Server.SetRequestDecoder.
type RequestDecoder func(d *jsontext.Decoder, req *Request) error

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
	// opts carries the RequestDecoder, as json/v2's unmarshaler for a
	// *Request; there is no separate copy of the decoder to keep in sync.
	opts json.Options
}

func NewServer() *Server {
	return &Server{
		methods: map[string]Handler{},
		opts:    decodeOptions(DecodeRequest),
	}
}

// decodeOptions hands d to json/v2 as the unmarshaler for *Request. The
// options are built once per decoder rather than per message, which keeps
// their construction off the request path.
func decodeOptions(d RequestDecoder) json.Options {
	return json.WithUnmarshalers(json.UnmarshalFromFunc(d))
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
	s.opts = decodeOptions(d)
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
func (s *Server) Serve(ctx context.Context, req *Request) Response {
	// Validate the ID first so later error responses never echo an invalid ID.
	if !req.IsNotification() && !isValidID(req.ID) {
		return NewErrorResponse(NewError(CodeInvalidRequest, "id must be a string, number, or null"), nil)
	}
	if req.JSONRPC != Version {
		return NewErrorResponse(NewError(CodeInvalidRequest, `jsonrpc must be "2.0"`), req.ID)
	}
	if req.Method == "" {
		return NewErrorResponse(NewError(CodeInvalidRequest, "missing method"), req.ID)
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
		return NewErrorResponse(NewError(CodeMethodNotFound, "method not found: "+req.Method), req.ID)
	}

	result, rpcErr := h(ctx, req.Params)
	if req.IsNotification() {
		return nil
	}
	if rpcErr != nil {
		return NewErrorResponse(rpcErr, req.ID)
	}
	// A nil result becomes JSON null in NewSuccessResponse: the spec requires
	// the member to be present on every success response.
	return NewSuccessResponse(result, req.ID)
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
		return json.Marshal(
			NewErrorResponse(classifyDecodeError(err), recoveredID(&req)),
		)
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
		return marshalMessageError(NewError(CodeParseError, err.Error()))
	}
	if len(elems) == 0 {
		return marshalMessageError(NewError(CodeInvalidRequest, "empty batch"))
	}
	responses := make([]Response, 0, len(elems))
	for _, elem := range elems {
		var req Request
		if err := s.decode(elem, &req); err != nil {
			responses = append(responses, NewErrorResponse(classifyDecodeError(err), recoveredID(&req)))
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

// decode runs the server's configured RequestDecoder over one message.
// json/v2 drives the decoder, so the one-value contract and the rejection of
// anything following that value are enforced by the same code that enforces
// them for any other unmarshaler.
func (s *Server) decode(data jsontext.Value, req *Request) error {
	s.mu.RLock()
	opts := s.opts
	s.mu.RUnlock()
	return json.Unmarshal(data, req, opts)
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
//
// It reads exactly one value from d, so a custom decoder can delegate to it
// and adjust the result.
func DecodeRequest(d *jsontext.Decoder, req *Request) error {
	tok, err := d.ReadToken()
	if err != nil {
		return tokenError(err)
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return NewError(CodeInvalidRequest, "request must be a JSON object")
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
				return NewError(CodeInvalidRequest, "jsonrpc must be a string")
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
				return NewError(CodeInvalidRequest, "method must be a string")
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
				return NewError(CodeInvalidRequest, "params must be an object or array")
			}

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			req.ID = jsontext.Value(val.Clone())

		default:
			return NewError(CodeInvalidRequest, "unknown member: "+name)
		}
	}

	// Consume the closing brace, completing the one value this decoder reads.
	// Whether anything follows it is json/v2's check, not ours.
	if _, err := d.ReadToken(); err != nil {
		return tokenError(err)
	}
	return nil
}

// tokenError maps a tokenizer failure to the spec's error codes: a duplicate
// member name is well-formed JSON violating uniqueness (Invalid Request),
// anything else is malformed input (Parse error).
func tokenError(err error) *Error {
	if errors.Is(err, jsontext.ErrDuplicateName) {
		return NewError(CodeInvalidRequest, err.Error())
	}
	return NewError(CodeParseError, err.Error())
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
		return NewError(CodeParseError, err.Error())
	}
	// json/v2 wraps an unmarshaler's error in a *SemanticError naming the Go
	// type it was decoding into. That framing is noise to a JSON-RPC client,
	// so report what the decoder itself said.
	if se, ok := errors.AsType[*json.SemanticError](err); ok && se.Err != nil {
		err = se.Err
	}
	return NewError(CodeInvalidRequest, err.Error())
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
	return json.Marshal(NewErrorResponse(e, nil))
}
