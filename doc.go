// Package jsonrpc implements JSON-RPC 2.0 with a small, transport-agnostic API.
//
// # Pieces
//
// Server holds a registry of methods and dispatches requests to them:
//
//   - Server.Register installs a Handler — func(context.Context, P) (R, error)
//     — under a method name. This is the normal way to write a method.
//   - Server.RegisterRaw installs a RawHandler, which works in raw params and
//     result bytes.
//   - Server.SetErrorHandler decides what every failure looks like on the wire.
//   - Server.Serve dispatches one decoded Request, returning nil for a
//     notification.
//
// Cross-cutting concerns (auth, logging, validation) are added as Middleware
// — func(RawHandler) RawHandler — passed per method to Server.Register /
// RegisterRaw or server-wide via Server.Use. Middleware works on the raw
// layer, so one middleware serves typed and raw methods alike.
//
// Register runs a typed pipeline (raw bytes → P → R → raw bytes) on top of
// RegisterRaw. Raw is that pipeline as a free function, so a pre-decode hook
// (e.g. JSON schema validation) is Middleware wrapped around Raw(fn).
//
// The decode owns structural failure only: params that do not fit P are
// Invalid params, and whether the decoded values are acceptable is the
// Handler's judgment. Validation therefore lives in the Handler rather than in
// a middleware that decodes the params a second time.
//
// Server.ServeMessage is the byte-level entry point for transports that work
// in raw messages (WebSocket, stdio, TCP). It handles JSON parsing, the spec's
// in-band parse error reporting, and batch messages (JSON arrays), which are
// dispatched per element. HTTP adapters that prefer parse failures as HTTP 400
// should call Serve directly instead.
//
// # Decoding and encoding
//
// Turning a message into a Request is a seam: a RequestDecoder, which has
// json.UnmarshalFromFunc's signature. The default, DecodeRequest, walks the
// message token by token so every rejection carries a message this package
// wrote. It is strict — duplicate member names anywhere and unknown members on
// the envelope are Invalid Request (unknown members inside params are
// tolerated) — and per the spec params must be structured whenever present, so
// a typed handler's P is a struct or a slice, never a bare scalar.
// "params":null is rejected like any other scalar; a request with no
// parameters omits the member, as NewParams(nil) and Client.Call with nil
// params do.
//
// Server.SetRequestDecoder installs a different one, taking control of the
// errors reported for a malformed envelope: an *Error from a decoder chooses
// the code, message, and Data, while any other error is classified as a Parse
// error or an Invalid Request. Either way the ErrorHandler has the final say.
// A custom decoder can delegate to DecodeRequest and adjust the result.
//
// A custom decoder is installed as json/v2's unmarshaler for a *Request, so it
// must read exactly one JSON value — one that ignores the message still calls
// SkipValue — and json/v2 rejects anything following that value.
//
// Whatever the decoder does, Serve independently validates every Request it
// dispatches (id shape, "2.0" version, non-empty method), since transports may
// build a Request without decoding one.
//
// Writing a Response is the mirror seam: a ResponseEncoder, which has
// json.MarshalToFunc's signature and is installed with
// Server.SetResponseEncoder. The default, EncodeResponse, writes the canonical
// object — the version from Version, then result or error, then id — and
// reports a response holding both members or neither rather than sending it. A
// custom encoder decides the wire form of every response the server sends, and
// can delegate to EncodeResponse. Its errors are not JSON-RPC errors: there is
// no response left to carry one, so they surface as ServeMessage's error
// return.
//
// # Options
//
// The server does all of its JSON work under one json.Options value installed
// by Server.SetOptions: the envelope decode, params into a Handler's P,
// marshaling its R, and writing responses. Any json/v2 option is accepted; the
// ones to reach for are json.WithUnmarshalers and json.WithMarshalers, which
// let a method's types take a wire form the types themselves do not define.
// Options are captured into each handler at registration time, so SetOptions,
// like Use, must run before any method is registered; for a single method, use
// Raw(fn, opts) with RegisterRaw.
//
// The RequestDecoder and ResponseEncoder ride in the same options, as the
// unmarshaler for *Request and the marshaler for *Response, outranking anything
// set for those types through SetOptions. Sharing one policy has two
// consequences: jsontext-level options such as
// jsontext.AllowDuplicateNames govern the envelope decode while json-level
// ones do not (DecodeRequest walks tokens and stores params raw), and omitted
// params yield the zero P without consulting any unmarshaler.
//
// # Errors
//
// Handler, RawHandler, and RequestDecoder all return a plain error. Returning
// an *Error names the code, message, and Data; anything else leaves that to
// the server, and can be wrapped with fmt.Errorf and %w without losing context.
//
// Server.SetErrorHandler installs the ErrorHandler that turns each of those
// errors into the *Error sent to the client. It sees every failure the server
// reports — a failed decode, a handler's error, the protocol failures Serve
// detects — including for notifications, where its *Error is discarded but the
// failure can still be logged.
//
// The default, DefaultErrorHandler, sends an *Error verbatim and turns
// anything else into an Internal error carrying the error's message. That
// reports an unclassified failure's text to the client, so a server that must
// not leak internals installs its own:
//
//	s.SetErrorHandler(func(ctx context.Context, req *Request, err error) *Error {
//		if e, ok := errors.AsType[*jsonrpc.Error](err); ok && e != nil {
//			return e // a classified failure is already client-safe
//		}
//		log.Printf("rpc %s: %v", req.Method, err)
//		return jsonrpc.NewError(jsonrpc.CodeInternalError, "internal error")
//	})
//
// # Client
//
// Client wraps a Sender, which round-trips a Request to a Response across some
// transport. Server.Sender adapts a Server for in-process use, and the
// jsonrpchttp subpackage provides an HTTP adapter; other wires implement
// Sender themselves.
//
// Client.Call marshals params, generates an id, sends, and decodes the result,
// returning server-reported errors as *Error; Client.Notify sends a
// notification. For full control, build a Request with NewRequest or
// NewNotification (with NewID and NewParams for the polymorphic fields),
// round-trip it with Client.Send, then check Response.Error and decode with
// Response.Decode.
//
// # Responses
//
// Response is one struct covering both shapes the spec allows: exactly one of
// Result and Error is set, and ID is always present. Which shape a response has
// is the Error field — nil on a success — so callers read fields rather than
// ask an interface.
//
// NewSuccessResponse and NewErrorResponse build one, normalizing an empty
// result or id to JSON null. A Response written by hand can still hold a shape
// the spec forbids, so its MarshalJSONTo — EncodeResponse under another name —
// reports one holding both members or neither instead of sending it. That check
// runs under both json packages, and a Server applies its ResponseEncoder in
// place of it.
//
// DecodeResponse parses one response object and checks the same invariants
// coming the other way; DecodeResponses does it for a batch reply. Plain
// json.Unmarshal into a Response works too, unchecked.
//
// # Polymorphic fields
//
// Request.Params, Request.ID, Response.Result, and Error.Data are stored as
// jsontext.Value because the spec leaves their types open. Decode them at the
// point of use; Server.Register and Raw do it for you.
//
// # Not included
//
// Client-side batching: Sender is a single request/response seam. (Server-side
// batches are handled by ServeMessage.) The seams — Sender on the client side,
// Serve and ServeMessage on the server side — let users build additional
// transports on top of the core package.
package jsonrpc
