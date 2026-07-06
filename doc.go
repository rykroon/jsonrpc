// Package jsonrpc implements JSON-RPC 2.0 with a small, transport-agnostic API.
//
// # Pieces
//
// Server holds a registry of methods and dispatches a Request to one:
//
//   - Server.RegisterHandler installs a raw Handler under a method name.
//   - Server.Serve(ctx, *Request) *Response dispatches a single decoded
//     Request; returns nil for notifications.
//
// Cross-cutting concerns (auth, logging, validation) are added as Middleware
// — func(Handler) Handler — passed per method to Register /
// RegisterHandler or server-wide via Server.Use.
//
// Register, Typed, DecodeParams, and MarshalResult are free functions that
// build a typed pipeline (raw bytes → typed params → typed result → raw
// bytes) on top of RegisterHandler. Use Register for normal methods. Typed
// adapts a typed function into a Handler you can hold, reuse, or wrap in
// Middleware — the way to run a pre-decode hook (e.g. JSON schema
// validation) is Middleware around Typed(fn).
//
// Server.ServeMessage is the byte-level entry point for transports that
// work in raw messages (WebSocket, stdio, TCP). It handles JSON parsing,
// the spec's in-band parse error reporting, and batch messages (JSON
// arrays), which are dispatched per element. HTTP adapters that prefer
// to surface parse failures as HTTP 400 should skip ServeMessage and call
// Serve directly.
//
// Client wraps a Sender — a function that round-trips a Request to a
// Response across some transport. InProcess(s) adapts a Server into a
// Sender for in-process use. The jsonrpchttp subpackage provides an HTTP
// adapter (both an http.Handler and a Sender); transport authors writing
// for other wires implement Sender themselves.
//
// Client.Call and Client.Notify are the convenience path: Call marshals
// params, generates an id, sends, and decodes the result, returning
// server-reported errors as *Error; Notify sends a notification. For full
// control, build a Request with NewRequest or NewNotification (with NewID
// and NewParams for the polymorphic fields), round-trip it with
// Client.Send, then check Response.Error and decode Response.Result with
// Response.Decode.
//
// # Polymorphic fields
//
// Request.Params, Request.ID, Response.Result, and Error.Data are stored
// as json.RawMessage because the spec leaves their types open. Decode
// them into concrete types at the point of use; the typed helpers
// (Register, Typed, DecodeParams) do this for you.
//
// # Not included
//
// Client-side batching is not supported: Sender is a single
// request/response seam. (Server-side batch messages are handled by
// ServeMessage.) The seams — Sender on the client side, Server.Serve and
// Server.ServeMessage on the server side — are designed so users can
// build additional transports on top of the core package.
package jsonrpc
