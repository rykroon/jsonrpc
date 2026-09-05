// Package jsonrpc implements JSON-RPC 2.0 with a small, transport-agnostic API.
//
// # Pieces
//
// Server holds a registry of methods and dispatches a Request to one:
//
//   - Server.Register installs a typed function under a method name.
//   - Server.RegisterHandler installs a raw Handler under a method name.
//   - Server.Serve(ctx, *Request) Response dispatches a single decoded
//     Request; returns nil for notifications.
//
// Cross-cutting concerns (auth, logging, validation) are added as Middleware
// — func(Handler) Handler — passed per method to Server.Register /
// RegisterHandler or server-wide via Server.Use.
//
// Server.Register runs a typed pipeline (raw bytes → typed params → typed
// result → raw bytes) on top of RegisterHandler; use it for normal methods.
// Its building blocks — Typed, DecodeParams, and MarshalResult — are free
// functions. Typed adapts a typed function into a Handler you can hold,
// reuse, or wrap in Middleware — the way to run a pre-decode hook (e.g.
// JSON schema validation) is Middleware around Typed(fn).
//
// Server.ServeMessage is the byte-level entry point for transports that
// work in raw messages (WebSocket, stdio, TCP). It handles JSON parsing,
// the spec's in-band parse error reporting, and batch messages (JSON
// arrays), which are dispatched per element. Protocol errors carry the
// specific cause as their message and attach no Data. HTTP adapters that
// prefer to surface parse failures as HTTP 400 should skip ServeMessage and
// call Serve directly.
//
// # Decoding
//
// Turning a message into a Request is a seam: a RequestDecoder, which is
// func(*jsontext.Decoder, *Request) error — json.UnmarshalFromFunc's
// signature, so a decoder works on either side. The package default,
// DecodeRequest, walks the message token by token so that every rejection
// carries a message this package wrote rather than one from the JSON
// library. It is strict — duplicate object member names anywhere and
// unknown members on the request envelope are rejected as Invalid Request
// (unknown members inside params are tolerated), and per the spec params
// must be a structured value whenever the member is present, so a typed
// handler's P is a struct or a slice, never a bare scalar. "params":null is
// rejected like any other scalar — a request with no parameters omits the
// member, which is what NewParams(nil) and Client.Call with nil params do.
//
// Server.SetRequestDecoder installs a different one, which is how a caller
// takes control of the errors reported for a malformed or non-conforming
// envelope: returning an *Error from a decoder surfaces it verbatim, while
// any other error is classified as a Parse error or an Invalid Request. A
// custom decoder can delegate to DecodeRequest and adjust the result.
//
// A custom decoder is installed as json/v2's unmarshaler for a *Request, so
// it reads exactly one JSON value from the decoder it is handed — one that
// ignores the message still calls SkipValue — and json/v2 rejects anything
// following that value. Trailing data is therefore not a rule a custom
// decoder can forget.
//
// Whatever the decoder does, Serve independently validates every Request it
// dispatches — the id shape, the "2.0" version, and a non-empty method —
// because transports may build a Request without decoding one at all.
//
// Client wraps a Sender — a function that round-trips a Request to a
// Response across some transport. Server.Sender adapts a Server into a
// Sender for in-process use. The jsonrpchttp subpackage provides an HTTP
// adapter (both an http.Handler and a Sender); transport authors writing
// for other wires implement Sender themselves.
//
// Client.Call and Client.Notify are the convenience path: Call marshals
// params, generates an id, sends, and decodes the result, returning
// server-reported errors as *Error; Notify sends a notification. For full
// control, build a Request with NewRequest or NewNotification (with NewID
// and NewParams for the polymorphic fields), round-trip it with
// Client.Send, then check Response.IsError and decode the result with
// Response.Decode.
//
// # Responses
//
// Response is an interface with exactly two implementations, matching the
// two shapes the spec allows: *SuccessResponse carries a result and
// *ErrorResponse carries an error object. Serve returns one of them (or
// nil for a notification), and the interface's accessors — Result, Error,
// ID, IsSuccess, IsError, Decode — let callers work with either without a
// type switch.
//
// Both concrete types keep their fields unexported and write themselves to
// the wire with MarshalJSONTo, so a response can only be built by
// NewSuccessResponse, NewErrorResponse, or DecodeResponse. The spec's
// invariants — a result member on every success, an id member on both —
// therefore hold by construction, and a response that somehow lacks one
// fails to marshal rather than reaching the wire malformed.
//
// Because Response is an interface it cannot be unmarshaled into.
// DecodeResponse turns one response object into the right concrete type,
// and DecodeResponses does the same for a batch reply; a transport
// implementing Sender uses them to parse what comes back off the wire.
//
// # Polymorphic fields
//
// Request.Params, Request.ID, Response.Result, and Error.Data are stored
// as jsontext.Value because the spec leaves their types open. Decode them
// into concrete types at the point of use; the typed helpers
// (Server.Register, Typed, DecodeParams) do this for you.
//
// # Not included
//
// Client-side batching is not supported: Sender is a single
// request/response seam. (Server-side batch messages are handled by
// ServeMessage.) The seams — Sender on the client side, Server.Serve and
// Server.ServeMessage on the server side — are designed so users can
// build additional transports on top of the core package.
package jsonrpc
