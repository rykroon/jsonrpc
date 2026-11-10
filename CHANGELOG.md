# Changelog

All notable changes to this project are documented here. Versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the major
version is 0, minor releases may carry breaking changes.

## [v0.6.0] — unreleased

A breaking release. Request ids and params get their own types, `ID` and
`Params`, in place of `jsontext.Value`, and `Error`, `Request`, and
`Response` are used by value.

### Migrating from v0.5.0

| v0.5.0 | v0.6.0 |
| --- | --- |
| `Request.ID`, `Response.ID` (`jsontext.Value`) | `ID` |
| `Request.Params` (`jsontext.Value`) | `Params` |
| `NewID("abc")`, `MustNewID("abc")` | `StringID("abc")` |
| `NewID(n)`, `MustNewID(n)` | `IntID(n)` |
| `NewParams(v) (jsontext.Value, error)` | `NewParams(v) (Params, error)` |
| `RawHandler func(ctx, jsontext.Value) (jsontext.Value, error)` | `RawHandler func(ctx, Params) (jsontext.Value, error)` |
| `string(req.ID)`, `string(req.Params)` | `req.ID.String()`, `req.Params.String()` |
| `len(req.ID) == 0` | `req.ID.IsZero()` |
| `nil` params or id in a constructor | `Params{}` / `ID{}` |
| `e.SetData(v)` | `e, err = e.WithData(v)` |
| `e.MustSetData(v)` | `e, err = e.WithData(v)` (no panicking variant) |
| `NewError(...) *Error` | `NewError(...) Error` |
| `ErrorHandler func(...) *Error` | `ErrorHandler func(...) Error` |
| `errors.AsType[*jsonrpc.Error](err)` | `errors.AsType[jsonrpc.Error](err)` |
| `*Request` in `Serve`, `Sender`, `SenderFunc`, `ErrorHandler`, `Client.Send` | `Request` |
| `req.Method`, `req.Params`, `req.ID`, `req.JSONRPC` | `req.Method()`, `req.Params()`, `req.ID()`, `req.JSONRPC()` |
| `&Request{...}` | `NewRequest(...)` / `NewNotification(...)` |
| `*Response` in `Serve`, `Sender`, `SenderFunc`, `Client.Send`, `NewSuccessResponse`, `NewErrorResponse`, `DecodeResponses` | `Response` |
| `resp == nil` (notification) | `resp.IsZero()` |
| `resp.Result`, `resp.ID`, `resp.JSONRPC` | `resp.Result()`, `resp.ID()`, `resp.JSONRPC()` |
| `if resp.Error != nil { use(*resp.Error) }` | `if e, ok := resp.Error(); ok { use(e) }` |
| `&Response{...}` | `NewSuccessResponse(...)` / `NewErrorResponse(...)` |

`StringID` and `IntID` cannot fail, so the error handling around `NewID`
goes away. In a `RawHandler` or `Middleware`, read the bytes with
`params.String()`, decode with `params.DecodeInto`, and build replacement
params with `NewParams`; the result stays `jsontext.Value`. A custom
`*Request` unmarshaler builds the fields by decoding into them
(`json.Unmarshal(raw, &req.ID)`) or with `NewParams(jsontext.Value(raw))`.

### Changed

- **`Request` is a value type with unexported fields.** It is passed by value
  everywhere and read through accessors, so it is never nil and an
  `ErrorHandler` cannot alter it. Build one with `NewRequest` or
  `NewNotification`; an `ErrorHandler` given no request sees the zero value.
- **`Response` is an immutable value type.** Its fields are unexported and
  read through accessors; `Error()` returns a copy and whether one is set.
  `Serve` and `Sender` return the zero `Response`, not nil, for a
  notification; check `IsZero()`.
- **`Error` is a value type.** `NewError`, `WithData`, `ErrorHandler`, and
  `DefaultErrorHandler` use `Error` instead of `*Error`, and `Client.Call`
  returns server errors as `Error`. A handler can no longer return a typed-nil
  `*Error`, so the server's guards for it, and for an `ErrorHandler`
  returning nil, are gone. A handler returning a `*Error` is no longer recognized as
  classified and goes to the `ErrorHandler` as an unclassified error.
- **`errors.Is` matches `Error` by code.** `Error.Is` compares `Code` only,
  so a sentinel such as `ErrNotFound` matches after `WithData`, `%w`
  wrapping, or a round trip through `Client.Call`. Previously it matched
  only the identical `*Error`.
- **`Error.WithData` replaces `SetData` and `MustSetData`.** It returns a
  copy instead of mutating the receiver, so a shared error such as
  `var ErrNotFound = jsonrpc.NewError(...)` can't be changed by one request
  and leak into another. There is no panicking variant; handle the marshal
  error.
- **`ID` replaces `jsontext.Value` ids; `NewID` and `MustNewID` are removed.**
  `ID` holds its JSON encoding, so ids compare with `==` and work as map keys.
  Decoded string ids are re-escaped minimally so differently escaped ids
  match; numbers are kept verbatim. `String` returns the JSON form, so `"1"`
  and `1` stay distinct.
- **`Params` replaces `jsontext.Value` params**, in `Request` and in
  `RawHandler`, so middleware sees params already checked to be an object or
  array, or absent. A `jsontext.Value` passed to `NewParams` is now validated
  rather than passed through unchecked.
- **`Client.Call` checks the response id.** A response whose id differs from
  the request's is an error rather than a result, so a misbehaving `Sender` or
  server cannot hand one call another's reply. A null id is still accepted on
  an error response, since that is how a server says it could not read the id.
- **A request whose id is not a string, number, or null fails to decode** as
  Invalid Request, rather than decoding and being rejected by `Serve`. The
  response is the same: Invalid Request with a null id.
- `Client.Call` and `Notify` no longer double-prefix a params error: it reads
  `jsonrpc: params must be…` rather than `jsonrpc: marshal params: jsonrpc:
  params must be…`.

### Added

- **`ID`** — `StringID`, `IntID`, `IsZero`, `IsNull`, `String`, and its own
  `MarshalJSONTo` / `UnmarshalJSONFrom`.
- **`Params`** — `IsObject`, `IsArray`, `IsZero`, `String`, `DecodeInto`,
  `Map` and `Slice` (members left undecoded, so numbers are not rounded), and
  its own `MarshalJSONTo` / `UnmarshalJSONFrom`.

### Fixed

- **An `ErrorHandler` can no longer change a response's id.** `Serve` and
  `ServeMessage` read the request id before calling it, so modifying `req`
  does not reach the reply.

## [v0.5.0] — 2026-09-08

A breaking release. The API is now typed-first, every failure routes through
one `ErrorHandler`, and JSON policy on both sides of the wire is set with
`json/v2` options.

### Migrating from v0.4.0

The typed handler is now the unadorned name, since it is the one most methods
use:

| v0.4.0 | v0.5.0 |
| --- | --- |
| `TypedHandler[P, R]` | `Handler[P, R]` |
| `Handler` (raw params/result) | `RawHandler` |
| `Middleware func(Handler) Handler` | `Middleware func(RawHandler) RawHandler` |
| `Server.RegisterHandler(name, h, mw...)` | `Server.RegisterRaw(name, h, mw...)` |
| `Typed(fn)` | `Raw(fn, opts...)` |

`Server.Register` is unchanged in name and now takes a `Handler[P, R]`.

Removed, with no replacement needed:

- `RequestDecoder` and `Server.SetRequestDecoder` — install a `*Request`
  unmarshaler through `Server.SetOptions` instead.
- `DecodeRequest` — `Request` decodes itself; use `json.Unmarshal` into one.
- `DecodeParams` and `MarshalResult` — both only ever served `Raw`, which now
  does the work inline.

### Changed

- **`Request` is a value type with unexported fields.** It is passed by value
  everywhere and read through accessors, so it is never nil and an
  `ErrorHandler` cannot alter it. Build one with `NewRequest` or
  `NewNotification`; an `ErrorHandler` given no request sees the zero value.
- **`RawHandler` returns `error`, not `*Error`.** No public signature other
  than `Error`'s own methods traffics in `*Error` any more. Returning an `*Error`
  means "I classified this"; anything else, wrapped with `%w` or not, is left
  to the `ErrorHandler`.
- **`NewID` returns `(jsontext.Value, error)`.** It previously discarded
  `json.Marshal`'s error and returned an empty id — and an empty id is a
  *notification*, so a failed id silently turned a call into fire-and-forget.
  It now also rejects an encoding that is not a JSON string or number, which
  a marshaler on a named type could otherwise produce. `MustNewID` is the
  panicking form for literals and counters; its constraint omits `~`, so a
  named type carrying a marshaler cannot reach it at all and the only
  remaining panic is a string holding invalid UTF-8. Convert to reach it —
  `MustNewID(string(id))` encodes the underlying string and bypasses any
  marshaler — or use `NewID` to have the marshaler honored.
- **`NewParams` rejects params that are not an object or an array**, as §4.2
  requires — a scalar, a `null` (which is what a typed nil marshals to), or an
  empty value is now an error at the call site instead of an Invalid Request
  from the far end.
- **The request decode is strict.** An unknown envelope member, a duplicate
  member name anywhere, a non-string `method` or `jsonrpc`, and non-structured
  `params` (including `"params":null`) are all Invalid Request. Unknown members
  *inside* `params` are still tolerated.
- **`Error.Data` is no longer HTML-escaped.** `SetData`/`MustSetData` moved
  from `encoding/json` to `encoding/json/v2`, so `{"q":"<b>&</b>"}` now goes out
  as written rather than as `{"q":"\u003cb\u003e\u0026\u003c/b\u003e"}`.
  `UnmarshalData` picks up v2's stricter reads, including rejecting duplicate
  member names.
- **`Server.Register` and `Server.RegisterRaw` panic on an empty method name.**
  `Serve` rejects `""` as a missing method before the lookup, so such a handler
  could never have run.
- `NewParams` and `Response.Decode` take trailing `...json.Options`. Existing
  calls are unaffected.

### Added

- **`Server.SetOptions(...json.Options)`** — one JSON policy for all of the
  server's work: the envelope, params into `P`, `R`, and responses. Because the
  options are used as given and json/v2 consults them before a type's own
  methods, an unmarshaler for `*Request` or a marshaler for `*Response` takes
  over the envelope itself. Must be called before any `Register`; for a single
  method use `Raw(fn, opts)` with `RegisterRaw`.
- **`Client.SetOptions(...json.Options)`** — the mirror on the calling side,
  covering the params `Call` and `Notify` marshal and the result `Call`
  decodes, so a client can speak the wire form its server installed. Must be
  called before the first `Call` or `Notify`; `Send` is outside it.
- **`Server.SetErrorHandler(ErrorHandler)`**, with `ErrorHandler` and
  `DefaultErrorHandler` — one seam that turns every error the server reports
  into the client's error object: decode failures, handler errors, and protocol
  failures `Serve` detects. It is also called for notification failures, which
  previously vanished silently.
- **`Request.UnmarshalJSONFrom`, `Response.MarshalJSONTo`, and
  `Response.UnmarshalJSONFrom`** — both types own their wire form, so any
  `json.Marshal` or `json.Unmarshal` of one gets the spec's checks, under v1 or
  v2. A response holding both `result` and `error`, or neither, is refused in
  both directions.
- **`NewSuccessResponse` / `NewErrorResponse`** — normalize an empty result or
  id to JSON null.
- **`DecodeResponses`** — parse a batch reply, applying those checks element by
  element.
- **`MustNewID`** — see above.

## [v0.4.0] — 2026-08-27

Earlier releases predate this changelog; see the git history.

[v0.6.0]: https://github.com/rykroon/jsonrpc/compare/v0.5.0...HEAD
[v0.5.0]: https://github.com/rykroon/jsonrpc/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/rykroon/jsonrpc/releases/tag/v0.4.0
