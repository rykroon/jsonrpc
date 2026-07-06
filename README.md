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
    jsonrpc.Register(s, "add", func(_ context.Context, p AddParams) (int, error) {
        return p.A + p.B, nil
    })

    c := jsonrpc.NewClient(jsonrpc.InProcess(s))

    var sum int
    if err := c.Call(context.Background(), "add", AddParams{A: 2, B: 3}, &sum); err != nil {
        panic(err)
    }
    fmt.Println(sum) // 5
}
```

## Middleware

Cross-cutting concerns — auth, logging, validation — are written once as
`Middleware` (`func(Handler) Handler`) and composed with handlers.
Because middleware works on the raw params, it layers cleanly over typed
handlers without touching the typed pipeline:

```go
// logging knows nothing about any handler's param or result types.
func logging(next jsonrpc.Handler) jsonrpc.Handler {
    return func(ctx context.Context, params json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
        log.Printf("rpc params: %s", params)
        return next(ctx, params)
    }
}

s := jsonrpc.NewServer()
s.Use(logging) // server-wide: applied to every method

// or per method (mw[0] is outermost), wrapped inside any server-wide middleware:
jsonrpc.Register(s, "add", add, requireAuth)
```

`Use` must be called before registering methods. The first middleware listed
is the outermost layer, and server-wide middleware wraps around per-method
middleware.

## What it gives you

- `Server` — a method registry with raw (`RegisterHandler`) and typed
  (`Register`) registration APIs.
- `Middleware` / `Server.Use` — compose auth, logging, and validation
  around handlers (per-method or server-wide).
- `Client.Call` / `Client.Notify` — one-line method calls with params
  marshaling, id generation, and result decoding; server errors come back
  as `*Error`. `Client.Send` is the low-level escape hatch, round-tripping
  a `*Request` through a `Sender` (in-process, HTTP, WebSocket, etc.).
- `NewRequest` / `NewNotification` / `NewID` / `NewParams` — construct
  requests without touching `json.RawMessage` directly.
- `Response.Decode` — unmarshal a successful result into a target.
- `Server.ServeMessage` — byte-level entry point for transports that
  work in raw messages (stdio, WebSocket, TCP stream).
- `Typed`, `DecodeParams`, `MarshalResult` — building blocks for the typed
  pipeline. `Typed(fn)` turns a typed function into a `Handler` you can
  reuse or wrap in `Middleware` (e.g. JSON schema validation with structured
  `Error.Data`).
- `jsonrpchttp` subpackage — `http.Handler` and `Sender` for the common
  single-request HTTP transport.

## What it does not include

- Batch requests.

The seams are designed so additional transports can be built on top
without changes to the core package.

## Docs

[pkg.go.dev/github.com/rykroon/jsonrpc](https://pkg.go.dev/github.com/rykroon/jsonrpc)
