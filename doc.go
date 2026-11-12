// Package jsonrpc implements JSON-RPC 2.0 with a small, transport-agnostic API.
//
// # Pieces
//
// Server is a registry of methods:
//
//   - Server.Register installs a Handler — func(context.Context, P) (R, error)
//     — under a method name. This is the normal way to write a method.
//   - Server.RegisterRaw installs a RawHandler, working on undecoded Params.
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
// The decode enforces what the spec requires and no more: members the spec
// does not define are ignored, in the envelope and inside params alike.
// Duplicate member names anywhere are Invalid Request under json/v2's
// defaults, which the Server uses; encoding/json v1 accepts them. Per the spec
// params must be structured when present, so P is a struct or slice, never a
// scalar, and "params":null is rejected; a request with no parameters omits
// the member, as NewParams(nil) does. NewParams holds the sending side to the
// same rule, so a request the far end would reject is not built in the first
// place.
//
// The response encode writes the canonical object — version, result or
// error, id — and refuses the zero Response, which holds neither.
//
// # Options
//
// Server.SetOptions installs one json.Options for the payload: params into P
// and R into the result. json.WithUnmarshalers and json.WithMarshalers give a
// method's types a wire form they do not define themselves. Options are
// captured at registration, so SetOptions runs before any Register; for one
// method use Raw(fn, opts) with RegisterRaw.
//
// The options never reach the envelope. Its wire form is fixed by the spec,
// so the server always encodes Request and Response its own way, and a
// marshaler or unmarshaler for either is ignored there. Error data is
// payload too: marshal it with Error.WithData's options.
//
// Omitted params yield the zero P without consulting any unmarshaler.
//
// Client.SetOptions is the mirror on the other side of the wire, covering the
// params Client.Call and Client.Notify marshal and the result Call decodes, so
// a wire form a server installs is one a client can speak. It must be called
// before either, and Client.Send is outside it: the Request is already built,
// and the envelope is the Sender's to encode.
//
// # Errors
//
// Handler and RawHandler return a plain error. Every failure the server
// reports, notifications included, goes to the ErrorHandler installed with
// Server.SetErrorHandler, which decides the Error the client sees.
//
// Error is the error object as data, and does not implement error. To report
// one from a handler, return its Err; FromError recovers it, through any %w
// wrapping, on either side of the wire:
//
//	return nil, jsonrpc.NewError(-32004, "user not found").Err()
//
// DefaultErrorHandler sends an error carrying an Error verbatim, so returning
// one names the code, message, and Data yourself, and reports anything else as
// an Internal error carrying its message. A server that must not leak
// internals installs its own:
//
//	s.SetErrorHandler(func(ctx context.Context, req jsonrpc.Request, err error) jsonrpc.Error {
//		if e, ok := jsonrpc.FromError(err); ok {
//			return e // a classified failure is already client-safe
//		}
//		log.Printf("rpc %s: %v", req.Method(), err)
//		return jsonrpc.NewError(jsonrpc.CodeInternalError, "internal error")
//	})
//
// # Client
//
// Client wraps a Sender, which round-trips a Request to a Response over some
// transport: Server.Sender in-process, jsonrpchttp over HTTP, or your own.
// Client.Call marshals params, sends, and decodes the result, returning
// a server-reported error as an error FromError recovers the Error from;
// Client.Notify sends a notification.
// Client.SetOptions gives both the same json/v2 options the server uses. For
// full control build a Request with NewRequest or NewNotification and send it
// with Client.Send.
//
// # Responses
//
// Response is an immutable value for both shapes: exactly one of Result and
// Error is set, and ID is always present. NewSuccessResponse and
// NewErrorResponse normalize an empty result or id to JSON null. Unmarshal
// refuses a response holding both members or neither, and marshal refuses the
// zero Response. The zero Response means no response: Serve returns it for a
// notification, and IsZero detects it.
//
// # Polymorphic fields
//
// Response.Result and Error.Data are jsontext.Value because the spec leaves
// their types open; both return copies, so the bytes a Response or Error holds
// cannot be altered. Error stores its data as a string, so Errors compare with
// ==. Register and Raw decode params and results for you.
//
// The two the spec does constrain have their own types. Params holds an
// object or array, or is zero when absent; NewParams enforces the shape. ID
// holds a string, number, or null as its JSON encoding, so ids compare with ==
// and work as map keys; StringID and IntID cannot fail. A zero ID is
// how IsNotification decides.
//
// # Not included
//
// Client-side batching: Sender is a single request/response seam.
package jsonrpc
