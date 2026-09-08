# jsonrpc

A small, transport-agnostic JSON-RPC 2.0 toolkit for Go.

```
go get github.com/rykroon/jsonrpc
```

## Quick start

```go
package main

import (
    "context"
    "fmt"

    "github.com/rykroon/jsonrpc"
)

type AddParams struct {
    A int `json:"a"`
    B int `json:"b"`
}

func main() {
    s := jsonrpc.NewServer()
    s.Register("add", func(_ context.Context, p AddParams) (int, error) {
        return p.A + p.B, nil
    })

    c := jsonrpc.NewClient(s.Sender())

    var sum int
    if err := c.Call(context.Background(), "add", AddParams{A: 2, B: 3}, &sum); err != nil {
        panic(err)
    }
    fmt.Println(sum) // 5
}
```

## Middleware

Cross-cutting concerns — auth, logging, validation — are written once as
`Middleware` (`func(RawHandler) RawHandler`) and composed with handlers.
Because middleware works on the raw params, it layers cleanly over typed
handlers without touching the typed pipeline:

```go
// logging knows nothing about any handler's param or result types.
func logging(next jsonrpc.RawHandler) jsonrpc.RawHandler {
    return func(ctx context.Context, params jsontext.Value) (jsontext.Value, error) {
        log.Printf("rpc params: %s", params)
        out, err := next(ctx, params)
        if err != nil {
            return nil, fmt.Errorf("rpc: %w", err) // wrapping survives to the ErrorHandler
        }
        return out, nil
    }
}

s := jsonrpc.NewServer()
s.Use(logging) // server-wide: applied to every method

// or per method (mw[0] is outermost), wrapped inside any server-wide middleware:
s.Register("add", add, requireAuth)
```

`Use` must be called before registering methods. The first middleware listed
is the outermost layer, and server-wide middleware wraps around per-method
middleware.

## Encoding and decoding

Params are decoded into `P` and results marshaled from `R` with
`encoding/json/v2`. `Server.SetOptions` installs json/v2 options for every
method, so a type can take a wire form it does not define itself:

```go
s := jsonrpc.NewServer()
s.SetOptions(json.WithMarshalers(
    json.MarshalToFunc(func(e *jsontext.Encoder, t time.Time) error {
        return e.WriteToken(jsontext.Int(t.Unix())) // time.Time results as Unix seconds
    }),
))
s.Register("epoch", epoch) // any time.Time in epoch's result uses the marshaler
```

`SetOptions` must be called before registering methods. To give one method
its own options, adapt it yourself: `s.RegisterRaw("m", jsonrpc.Raw(fn, opts))`.

`Client.SetOptions` is the mirror on the other side of the wire, covering the
params `Call` and `Notify` marshal and the result `Call` decodes, so a client
can speak the wire form its server installed:

```go
c := jsonrpc.NewClient(sender)
c.SetOptions(json.WithUnmarshalers(/* the marshaler's inverse */))

var at time.Time
c.Call(ctx, "epoch", nil, &at) // decodes the Unix seconds the server sent
```

It must be called before the first `Call` or `Notify`. `Client.Send` is
outside it — the `Request` is already built, and the envelope is the
`Sender`'s to encode.

## Errors

Everything that can fail returns a plain `error`. Returning a `*jsonrpc.Error`
says you classified the failure yourself; anything else leaves that to the
server, so handlers and middleware can wrap freely with `%w`:

```go
s.SetErrorHandler(func(ctx context.Context, req *jsonrpc.Request, err error) *jsonrpc.Error {
    if e, ok := errors.AsType[*jsonrpc.Error](err); ok && e != nil {
        return e // already client-safe
    }
    log.Printf("rpc %s: %v", req.Method, err) // keep the detail off the wire
    return jsonrpc.NewError(jsonrpc.CodeInternalError, "internal error")
})
```

The default sends `*Error` values through untouched and reports anything else
as an Internal error carrying the error's message.

## What it gives you

- `Server` — a method registry. `Register` installs a `Handler`
  (`func(ctx, P) (R, error)`), the normal way to write a method;
  `RegisterRaw` installs a `RawHandler` working in raw bytes.
- `Middleware` / `Server.Use` — compose auth, logging, and validation
  around handlers (per-method or server-wide).
- `Client.Call` / `Client.Notify` — one-line method calls with params
  marshaling, id generation, and result decoding; server errors come back
  as `*Error`. `Client.Send` is the low-level escape hatch, round-tripping
  a `*Request` through a `Sender` (in-process, HTTP, WebSocket, etc.).
- `Server.SetErrorHandler` — one place to decide what every failure looks
  like on the wire: sanitize messages, remap codes, attach `Data`, log the
  cause. Also called for notification failures, which send no reply.
- `Server.SetOptions` — install json/v2 options (e.g. `json.WithMarshalers`,
  `json.WithUnmarshalers`) that control how every method's params are
  decoded and results encoded. The options are used as given, so an
  unmarshaler for `*Request` or a marshaler for `*Response` takes over the
  envelope itself. `Client.SetOptions` installs the same policy on the
  calling side.
- `NewRequest` / `NewNotification` / `NewID` / `NewParams` — construct
  requests without touching `jsontext.Value` directly. `NewParams` enforces
  §4.2 on the way out: params must marshal to an object or an array, so a
  scalar (or a typed nil, which marshals to `null`) is an error locally
  rather than an Invalid Request from the far end.
- `Request` — a plain struct with `Params` and `ID` kept as raw JSON. It
  decodes itself with `UnmarshalJSONFrom`, so the strict envelope check applies
  to any `json.Unmarshal` into one, not only the ones a `Server` drives.
- `Response` — one struct for both shapes the spec allows: exactly one of
  `Result` and `Error` is set, and `ID` is always present. `resp.Error != nil`
  tells the two apart, and `Decode` unmarshals a successful result into a
  target. `NewSuccessResponse` / `NewErrorResponse` normalize an empty result
  or id to JSON null, and it writes itself in canonical form with
  `MarshalJSONTo` and reads itself with `UnmarshalJSONFrom`, so any
  `json.Unmarshal` into one checks the spec's invariants — what a `Sender`
  implementation reaches for.
- `DecodeResponses` — parse a batch reply off the wire, applying those same
  checks element by element.
- `Server.ServeMessage` — byte-level entry point for transports that
  work in raw messages (stdio, WebSocket, TCP stream). Handles batch
  messages (JSON arrays) per the spec.
- `Raw` — the typed pipeline as a free function. `Raw(fn, opts...)` turns a
  `Handler` into a `RawHandler` you can reuse, wrap in `Middleware` (e.g.
  JSON schema validation with structured `Error.Data`), or give its own
  options. Params that do not fit `P` are reported as Invalid params;
  whether the values are *acceptable* is the handler's call.
- `jsonrpchttp` subpackage — `http.Handler` and `Sender` for the common
  single-request HTTP transport.

## What it does not include

- Client-side batching (`Sender` is a single request/response seam;
  server-side batches are handled by `ServeMessage`).

The seams are designed so additional transports can be built on top
without changes to the core package.

## Docs

[pkg.go.dev/github.com/rykroon/jsonrpc](https://pkg.go.dev/github.com/rykroon/jsonrpc)
