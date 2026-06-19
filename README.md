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

    params, _ := jsonrpc.NewParams(AddParams{A: 2, B: 3})
    resp, _ := c.Send(context.Background(), jsonrpc.NewRequest("add", params, jsonrpc.NewID(1)))
    var sum int
    _ = resp.Decode(&sum)
    fmt.Println(sum) // 5
}
```

## What it gives you

- `Server` — a method registry with raw (`RegisterHandler`) and typed
  (`Register`) registration APIs.
- `Client.Send` — round-trips a `*Request` through a `Sender`
  (in-process, HTTP, WebSocket, etc.).
- `NewRequest` / `NewNotification` / `NewID` / `NewParams` — construct
  requests without touching `json.RawMessage` directly.
- `Response.Decode` — unmarshal a successful result into a target.
- `Server.ServeMessage` — byte-level entry point for transports that
  work in raw messages (stdio, WebSocket, TCP stream).
- `DecodeParams`, `MarshalResult`, `Dispatch` — small building blocks for
  escape-hatch handlers that need custom pre-decode or post-call logic
  (e.g. JSON schema validation with structured `Error.Data`).
- `jsonrpchttp` subpackage — `http.Handler` and `Sender` for the common
  single-request HTTP transport.

## What it does not include

- Batch requests.

The seams are designed so additional transports can be built on top
without changes to the core package.

## Docs

[pkg.go.dev/github.com/rykroon/jsonrpc](https://pkg.go.dev/github.com/rykroon/jsonrpc)
