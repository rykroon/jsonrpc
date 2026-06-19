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
// Register, DecodeParams, MarshalResult, and Dispatch are free functions
// that build a typed pipeline (raw bytes → typed params → typed result →
// raw bytes) on top of RegisterHandler. Use Register for normal methods;
// drop to the lower-level helpers when you need a pre-decode hook (e.g.
// JSON schema validation) or post-call transformation.
//
// MessageServer wraps a Server and exposes a byte-level entry point —
// ServeMessage(ctx, json.RawMessage) — for transports that work in raw
// messages (WebSocket, stdio, TCP). It handles JSON parsing and the spec's
// in-band parse error reporting. HTTP adapters that prefer to surface
// parse failures as HTTP 400 should skip MessageServer and call
// Server.Serve directly.
//
// Client wraps a Sender — a function that round-trips a Request to a
// Response across some transport. InProcess(s) adapts a Server into a
// Sender for in-process use. The jsonrpchttp subpackage provides an HTTP
// adapter (both an http.Handler and a Sender); transport authors writing
// for other wires implement Sender themselves.
//
// Build a Request with NewRequest or NewNotification; use NewID and
// NewParams to encode the polymorphic id and params fields from Go
// values. Round-trip with Client.Send, then check Response.Error and
// decode Response.Result with Response.Decode.
//
// # Polymorphic fields
//
// Request.Params, Request.ID, Response.Result, and Error.Data are stored
// as json.RawMessage because the spec leaves their types open. Decode
// them into concrete types at the point of use; the typed helpers
// (Register, DecodeParams, Dispatch) do this for you.
//
// # Not included
//
// Batch requests are not supported. The seams — Sender on the client
// side, Server.Serve and MessageServer on the server side — are designed
// so users can build additional transports on top of the core package.
package jsonrpc
