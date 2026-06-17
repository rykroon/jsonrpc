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
// HandleMessage is the byte-level entry point used by transport adapters
// that work in raw messages (WebSocket, stdio, TCP). It handles JSON
// parsing and the spec's in-band parse error reporting. HTTP adapters that
// prefer to surface parse failures as HTTP 400 should skip HandleMessage
// and call Server.Serve directly.
//
// Client wraps a Sender — a function that round-trips a Request to a
// Response across some transport. InProcess(s) adapts a Server into a
// Sender for in-process use. Transport authors (HTTP, WebSocket, etc.)
// write their own Sender.
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
// Batch requests and built-in transport adapters are not in this package.
// The seams — Sender on the client side, Server.Serve and HandleMessage
// on the server side — are designed so that users can build them on top.
package jsonrpc
