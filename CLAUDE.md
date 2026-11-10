# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

Requires Go 1.27+ (generic methods, `encoding/json/v2` — no GOEXPERIMENT needed).

```sh
go vet ./...                              # CI runs vet + test
go test ./...
go test -run TestMethodNotFound .         # single test in the root package
go test -run TestName ./jsonrpchttp       # single test in the HTTP subpackage
```

Tests use `testify/require` and live in the package itself (`package jsonrpc`), so they can reach unexported helpers. `example_test.go` holds runnable godoc examples whose `// Output:` is checked by `go test`.

## Architecture

A transport-agnostic JSON-RPC 2.0 library. The root package has no I/O; transports sit on two seams:

- **Server side:** `Server.Serve(ctx, Request) Response` for one decoded request (zero `Response` for a notification), or `Server.ServeMessage(ctx, bytes)` which parses raw bytes (batches included) and returns response bytes.
- **Client side:** the `Sender` interface (`Send(ctx, Request) (Response, error)`). `Server.Sender()` is the in-process one; `jsonrpchttp.Sender` POSTs over HTTP. `Client` wraps a `Sender` with `Call`/`Notify`.

`jsonrpchttp` is the only transport in-repo: `Handler` (an `http.Handler` around `ServeMessage`; JSON-RPC errors in-band with 200, notifications 204) and `Sender`.

### Dispatch pipeline

Everything registered is stored as a `RawHandler` (`func(ctx, Params) (jsontext.Value, error)`). `Server.Register` takes a typed `Handler[P, R]` and converts it with `Raw(fn, s.opts)` (`typed.go`), which decodes params into `P` (failure → Invalid params) and marshals `R`. `Middleware` is `func(RawHandler) RawHandler`, so it works uniformly over typed and raw methods. Composition: server-wide middleware (`Use`) wraps per-method middleware, `mw[0]` outermost.

`Use`, `SetOptions`, and `SetErrorHandler` all panic once any method is registered — options are captured at registration time.

### Errors

Handlers return a plain `error`. An `Error` means "already classified, send as-is"; anything else goes to the `ErrorHandler`, which sees *every* failure (decode, protocol, handler, and notification failures whose result is discarded). `Error`, `Request`, and `Response` are immutable value types (unexported fields, accessors), so unwrap with `errors.AsType[Error]`.

### Wire format and validation

- All JSON uses `encoding/json/v2` and `jsontext`. `Result` and `Error.Data` are `jsontext.Value`; `Params` (`params.go`) wraps an object/array value, and `ID` (`id.go`) holds its JSON encoding in a `string` so IDs compare with `==`.
- `Request.UnmarshalJSONFrom` (`requests.go`) is a hand-written token walk that enforces the spec strictly: unknown envelope members, duplicate names, non-structured or `null` params are Invalid Request. Unknown members *inside* params are tolerated.
- `Response.MarshalJSONTo` / `UnmarshalJSONFrom` (`responses.go`) enforce "exactly one of result/error, id always present".
- Constructors enforce the spec on the sending side too: `NewParams` requires object/array; `StringID`/`IntID` cannot fail. A zero `ID` means notification, so it must never be produced silently.
- `Server.SetOptions` / `Client.SetOptions` options are passed through as-is, so a user-installed unmarshaler for `*Request` or marshaler for `*Response` replaces the envelope codec.

## Conventions

- Keep doc comments short — this repo has deliberately trimmed verbose comments. Write the comment, then cut it.
- `doc.go` and `README.md` describe the public API in prose; update them when behavior changes.
- `CHANGELOG.md` tracks the unreleased version; record breaking changes there (with a migration note for renames/removals).
