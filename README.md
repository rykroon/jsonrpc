# jsonrpc

A small, transport-agnostic JSON-RPC 2.0 toolkit for Go.

```
go get github.com/rykroon/jsonrpc
```

Requires Go 1.27 or later: the package uses `encoding/json/v2` and generic
methods.

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
    if _, err := c.Call(context.Background(), "add", AddParams{A: 2, B: 3}, &sum); err != nil {
        panic(err)
    }
    fmt.Println(sum) // 5
}
```

## Middleware

Cross-cutting concerns — auth, logging, validation — are written once as
`Middleware` (`func(RawHandler) RawHandler`) and composed with handlers.
Middleware sees the decoded `Request` with its `Params` still undecoded, so it
layers cleanly over typed handlers without touching the typed pipeline:

```go
// logging knows nothing about any handler's param or result types.
func logging(next jsonrpc.RawHandler) jsonrpc.RawHandler {
    return func(ctx context.Context, req jsonrpc.Request) (jsontext.Value, error) {
        log.Printf("rpc %s params: %s", req.Method(), req.Params())
        out, err := next(ctx, req)
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
The options cover the payload only; the JSON-RPC envelope always uses the
package's own encoding. Error data takes options at construction:
`NewError(code, msg).WithData(v, opts)`.

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

Everything that can fail returns a plain `error`, and every error a handler
or middleware returns goes to the server's `ErrorHandler`.

`jsonrpc.Error` is the error object as plain data — an immutable value,
compared by `Code()` rather than `==` — and does not implement `error`, much
like gRPC's `status.Status`.
`Err()` wraps it as one, and `jsonrpc.FromError` gets it back, looking through
`%w` wrapping. Returning an `Error`'s `Err()` says you classified the failure
yourself; anything else leaves that to the `ErrorHandler`:

```go
func getUser(ctx context.Context, p GetUserParams) (User, error) {
    u, ok := users[p.ID]
    if !ok {
        e, _ := jsonrpc.NewError(-32004, "user not found").WithData(p.ID)
        return User{}, e.Err()
    }
    return u, nil
}

s.SetErrorHandler(func(ctx context.Context, req jsonrpc.Request, err error) jsonrpc.Error {
    if e, ok := jsonrpc.FromError(err); ok {
        return e // already client-safe
    }
    log.Printf("rpc %s: %v", req.Method(), err) // keep the detail off the wire
    return jsonrpc.NewError(jsonrpc.CodeInternalError, "internal error")
})
```

The default sends a carried `Error` through untouched and reports anything
else as an Internal error carrying the error's message.

The spec's predefined error codes are the constants `CodeParseError`,
`CodeInvalidRequest`, `CodeMethodNotFound`, `CodeInvalidParams`, and
`CodeInternalError`. A caller tests what came back from `Client.Call` with
`FromError` and `Code`:

```go
_, err := c.Call(ctx, "add", params, &sum)
if e, ok := jsonrpc.FromError(err); ok && e.Code() == jsonrpc.CodeMethodNotFound {
    // the server has no such method
}
```

Errors made by `Err()` also match each other by code under `errors.Is`, so an
application error declared once — `var errNotFound = jsonrpc.NewError(-32004,
"not found")` — is recognized with `errors.Is(err, errNotFound.Err())`.

## What it gives you

- `Server` — a method registry. `Register` installs a `Handler`
  (`func(ctx, P) (R, error)`), the normal way to write a method;
  `RegisterRaw` installs a `RawHandler`, which receives the `Request` with
  its `Params` undecoded.
- `Middleware` / `Server.Use` — compose auth, logging, and validation
  around handlers (per-method or server-wide).
- `Client.Call` / `Client.Notify` — one-line method calls with params
  marshaling, id generation, and result decoding; a server error comes back
  as an `error` that `FromError` turns into an `Error`. `Call` returns the id it sent, and `Client.SetIDGenerator`
  replaces the default counter with your own id scheme. `Client.Send` is the
  low-level escape hatch, round-tripping a `Request` through a `Sender`
  (in-process, HTTP, WebSocket, etc.).
- `Server.SetErrorHandler` — one place to decide what every failure looks
  like on the wire: sanitize messages, remap codes, attach `Data`, log the
  cause. Also called for notification failures, which send no reply.
- `Server.SetOptions` — install json/v2 options (e.g. `json.WithMarshalers`,
  `json.WithUnmarshalers`) that control how every method's params are
  decoded and results encoded. They never reach the envelope, whose wire
  form the spec fixes. `Client.SetOptions` installs the same policy on the
  calling side.
- `NewRequest` / `NewNotification` — construct requests from a `Params` and
  an `ID`.
- `Params` / `NewParams` — a request's params. `NewParams` requires an object
  or an array (§4.2), so a scalar — or a typed nil, which marshals to `null` —
  fails locally instead of as an Invalid Request from the far end. `nil` gives
  zero `Params`, which sends none. `Bytes`, `Decode`, `Map`, and `Slice` read
  them.
- `ID` / `StringID` / `IntID` — a request id, held as its JSON encoding
  so ids compare with `==` and work as map keys. The constructors cannot fail;
  a zero `ID` is a notification. `String` returns the JSON form, so `"1"` and
  `1` stay distinct.
- `Request` — an immutable value read through `Method()`, `Params()`, and
  `ID()`. It decodes itself with `UnmarshalJSONFrom`, so the envelope
  check applies to any `json.Unmarshal` into one, not only the ones a `Server`
  drives.
- `Response` — an immutable value for both shapes the spec allows: exactly
  one of `Result()` and `Error()` is set, and `ID()` is always present.
  `e, ok := resp.Error()` tells the two apart, and `DecodeResult` unmarshals a
  successful result into a target. The zero `Response` means no response
  (`Serve` and `Sender` return it for a notification); check `IsZero()`.
  `NewSuccessResponse` / `NewErrorResponse` normalize an empty result or id to
  JSON null, and it writes itself in canonical form with `MarshalJSONTo` and
  reads itself with `UnmarshalJSONFrom`, so any `json.Unmarshal` into one
  checks the spec's invariants — what a `Sender` implementation reaches for.
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
