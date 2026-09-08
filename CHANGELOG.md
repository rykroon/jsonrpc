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

[v0.5.0]: https://github.com/rykroon/jsonrpc/compare/v0.4.0...HEAD
[v0.4.0]: https://github.com/rykroon/jsonrpc/releases/tag/v0.4.0
