// Package jsonrpc implements JSON-RPC 2.0 with a small, transport-agnostic API.
//
// # Pieces
//
// Server is a registry of methods:
//
//   - Server.Register installs a Handler — func(context.Context, P) (R, error)
//     — under a method name. This is the normal way to write a method.
//   - Server.RegisterRaw installs a RawHandler, working in raw bytes.
//   - Server.SetErrorHandler decides what every failure looks like on the wire.
//   - Server.Serve dispatches one decoded Request. Server.ServeMessage parses
//     raw bytes first, batches included, for transports like WebSocket or
//     stdio; HTTP adapters that want parse failures as HTTP 400 call Serve.
//
// Middleware — func(RawHandler) RawHandler — is passed per method or
// server-wide via Server.Use, and works on the raw layer so one middleware
// serves typed and raw methods alike. Raw is Register's typed pipeline as a
// free function, so a pre-decode hook such as schema validation is Middleware
// around Raw(fn).
//
// The decode owns structural failure only: params that do not fit P are
// Invalid params. Whether the values are acceptable is the Handler's
// judgment, so validation lives there, not in middleware that decodes twice.
//
// # Decoding and encoding
//
// Request and Response carry their own wire forms in UnmarshalJSONFrom and
// MarshalJSONTo, so every json.Unmarshal or json.Marshal of them, under v1 or
// v2, gets the spec's checks.
//
// The request decode is strict: duplicate member names anywhere and unknown
// envelope members are Invalid Request (unknown members inside params are
// tolerated). Per the spec params must be structured when present, so P is a
// struct or slice, never a scalar, and "params":null is rejected; a request
// with no parameters omits the member, as NewParams(nil) does.
//
// The response encode writes the canonical object — version, result or
// error, id — and refuses one holding both members or neither.
//
// # Options
//
// Server.SetOptions installs one json.Options for all of the server's JSON
// work: the envelope, params into P, R, and responses. json.WithUnmarshalers
// and json.WithMarshalers give a method's types a wire form they do not
// define themselves. Options are captured at registration, so SetOptions runs
// before any Register; for one method use Raw(fn, opts) with RegisterRaw.
//
// The options are used as given, and json/v2 consults them before a type's
// methods, so an unmarshaler for *Request or a marshaler for *Response takes
// over the envelope and can delegate to the type's own method. An *Error from
// such an unmarshaler is sent as-is; any other error is a Parse error or an
// Invalid Request, and Serve still validates every Request it dispatches. A
// marshaler's error has no response left to carry it, so it surfaces as
// ServeMessage's error return.
//
// The envelope decode walks tokens, so only jsontext-level options reach it.
// Omitted params yield the zero P without consulting any unmarshaler.
//
// # Errors
//
// Handler and RawHandler return a plain error. An *Error names the code,
// message, and Data; anything else, wrapped or not, is left to the
// ErrorHandler installed with Server.SetErrorHandler, which sees every failure
// the server reports, notifications included.
//
// DefaultErrorHandler sends an *Error verbatim and reports anything else as an
// Internal error carrying its message. A server that must not leak internals
// installs its own:
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
// Client wraps a Sender, which round-trips a Request to a Response over some
// transport: Server.Sender in-process, jsonrpchttp over HTTP, or your own.
// Client.Call marshals params, sends, and decodes the result, returning
// server-reported errors as *Error; Client.Notify sends a notification. For
// full control build a Request with NewRequest or NewNotification and send it
// with Client.Send.
//
// # Responses
//
// Response is one struct for both shapes: exactly one of Result and Error is
// set, and ID is always present. NewSuccessResponse and NewErrorResponse
// normalize an empty result or id to JSON null. Marshal and unmarshal both
// refuse a response holding both members or neither.
//
// # Polymorphic fields
//
// Request.Params, Request.ID, Response.Result, and Error.Data are
// jsontext.Value because the spec leaves their types open; Register and Raw
// decode params and results for you.
//
// # Not included
//
// Client-side batching: Sender is a single request/response seam.
package jsonrpc
