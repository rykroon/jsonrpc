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
    return func(ctx context.Context, params jsontext.Value) (jsontext.Value, *jsonrpc.Error) {
        log.Printf("rpc params: %s", params)
        return next(ctx, params)
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

## What it gives you

- `Server` — a method registry. `Register` installs a `Handler`
  (`func(ctx, P) (R, error)`), the normal way to write a method;
  `RegisterRaw` installs a `RawHandler` working in raw bytes.
- `Middleware` / `Server.Use` — compose auth, logging, and validation
  around handlers (per-method or server-wide).
- `Client.Call` / `Client.Notify` — one-line method calls with params
- `Server.SetOptions` — install json/v2 options (e.g. `json.WithMarshalers`,
  `json.WithUnmarshalers`) that control how every method's params are
  decoded and results encoded
- `Server.SetRequestDecoder` — swap the request decoder to control the
  errors reported for a malformed envelope
  marshaling, id generation, and result decoding; server errors come back
  as `*Error`. `Client.Send` is the low-level escape hatch, round-tripping
  a `*Request` through a `Sender` (in-process, HTTP, WebSocket, etc.).
- `NewRequest` / `NewNotification` / `NewID` / `NewParams` — construct
  requests without touching `jsontext.Value` directly.
- `Response` — an interface over the spec's two response shapes,
  `*SuccessResponse` and `*ErrorResponse`. `Result`, `Error`, `ID`,
  `IsSuccess`, `IsError`, and `Decode` work on either; `Decode` unmarshals a
  successful result into a target. Both types are constructor-only, so the
  spec's invariants hold by construction.
- `DecodeResponse` / `DecodeResponses` — parse a response (or a batch reply)
  off the wire into the right concrete type; what a `Sender` implementation
  needs, since `Response` is an interface.
- `Server.ServeMessage` — byte-level entry point for transports that
  work in raw messages (stdio, WebSocket, TCP stream). Handles batch
  messages (JSON arrays) per the spec.
- `Raw`, `DecodeParams`, `MarshalResult` — building blocks for the typed
  pipeline. `Raw(fn, opts...)` turns a `Handler` into a `RawHandler` you can
  reuse, wrap in `Middleware` (e.g. JSON schema validation with structured
  `Error.Data`), or give its own options.
- `jsonrpchttp` subpackage — `http.Handler` and `Sender` for the common
  single-request HTTP transport.

## What it does not include

- Client-side batching (`Sender` is a single request/response seam;
  server-side batches are handled by `ServeMessage`).

The seams are designed so additional transports can be built on top
without changes to the core package.

## Docs

[pkg.go.dev/github.com/rykroon/jsonrpc](https://pkg.go.dev/github.com/rykroon/jsonrpc)
