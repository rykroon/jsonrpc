# Changelog

All notable changes to this project are documented here. Versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the major
version is 0, minor releases may carry breaking changes.

## [v0.5.0] — unreleased

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

Request ids and params have their own types:

| v0.4.0 | v0.5.0 |
| --- | --- |
| `Request.ID`, `Response.ID` (`jsontext.Value`) | `ID` |
| `Request.Params` (`jsontext.Value`) | `Params` |
| `NewID("abc")` | `NewStringID("abc")` |
| `NewID(n)` | `NewIntID(n)` |
| `NewParams(v) (jsontext.Value, error)` | `NewParams(v) (Params, error)` |
| `string(req.ID)`, `string(req.Params)` | `req.ID.String()`, `req.Params.String()` |
| `len(req.ID) == 0` | `req.ID.IsZero()` |
| `nil` params or id in a constructor | `Params{}` / `ID{}` |

A custom `*Request` unmarshaler builds the fields by decoding into them
(`json.Unmarshal(raw, &req.ID)`) or with `NewParams(jsontext.Value(raw))`.
`RawHandler` and `Middleware` still receive params as `jsontext.Value`.

Removed, with no replacement needed:

- `RequestDecoder` and `Server.SetRequestDecoder` — install a `*Request`
  unmarshaler through `Server.SetOptions` instead.
- `DecodeRequest` — `Request` decodes itself; use `json.Unmarshal` into one.
- `DecodeParams` and `MarshalResult` — both only ever served `Raw`, which now
  does the work inline.

### Changed

- **`RawHandler` returns `error`, not `*Error`.** No public signature other
  than `Error`'s own methods traffics in `*Error` any more. Returning an `*Error`
  means "I classified this"; anything else, wrapped with `%w` or not, is left
  to the `ErrorHandler`.
- **`ID` replaces `jsontext.Value` ids, and `NewID` is removed.** `NewID`
  could fail and return an empty id — which is a *notification*, so a failed id
  silently turned a call into fire-and-forget. `NewStringID` and `NewIntID`
  cannot fail. `ID` holds its JSON encoding, so ids compare with `==`; decoded
  string ids are re-escaped minimally so differently escaped ids match, and
  numbers are kept verbatim.
- **`Params` replaces `jsontext.Value` params.** `NewParams` rejects params that
  are not an object or an array, as §4.2 requires — a scalar, a `null` (which
  is what a typed nil marshals to), or an empty value is now an error at the
  call site instead of an Invalid Request from the far end. A `jsontext.Value`
  argument is now validated rather than passed through unchecked.
- **A request whose id is not a string, number, or null fails to decode** as
  Invalid Request, rather than decoding and being rejected by `Serve`. The
  response is the same: Invalid Request with a null id.
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
- **`ID`** (`NewStringID`, `NewIntID`, `IsZero`, `IsNull`, `String`) and
  **`Params`** (`NewParams`, `IsObject`, `IsArray`, `IsZero`, `String`,
  `DecodeInto`, `Map`, `Slice`) — see above.

## [v0.4.0] — 2026-08-27

Earlier releases predate this changelog; see the git history.

[v0.5.0]: https://github.com/rykroon/jsonrpc/compare/v0.4.0...HEAD
[v0.4.0]: https://github.com/rykroon/jsonrpc/releases/tag/v0.4.0
