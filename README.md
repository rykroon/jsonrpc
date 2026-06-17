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
    _ = c.Call(context.Background(), "add", AddParams{A: 2, B: 3}, &sum)
    fmt.Println(sum) // 5
}
```

## What it gives you

- `Server` — a method registry with raw (`RegisterHandler`) and typed
  (`Register`) registration APIs.
- `Client` — generates IDs, marshals params, decodes results; wraps any
  `Sender` (in-process, HTTP, WebSocket, etc.).
- `HandleMessage` — byte-level entry point for transport adapters that
  work in raw messages (stdio, WebSocket, TCP stream).
- `DecodeParams`, `MarshalResult`, `Dispatch` — small building blocks for
  escape-hatch handlers that need custom pre-decode or post-call logic
  (e.g. JSON schema validation with structured `Error.Data`).

## What it does not include

- Batch requests.
- Built-in transport adapters (HTTP, WebSocket, etc.).

The seams are designed so these can be built on top without changes to
the package.

## Docs

[pkg.go.dev/github.com/rykroon/jsonrpc](https://pkg.go.dev/github.com/rykroon/jsonrpc)
