package jsonrpc_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/rykroon/jsonrpc"
)

// ExampleServer registers a typed method and dispatches a request.
func ExampleServer() {
	s := jsonrpc.NewServer()
	s.Register("add", func(_ context.Context, p struct {
		A int `json:"a"`
		B int `json:"b"`
	}) (int, error) {
		return p.A + p.B, nil
	})

	req := &jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "add",
		Params:  jsontext.Value(`{"a":2,"b":3}`),
		ID:      jsontext.Value("1"),
	}
	resp := s.Serve(context.Background(), req)
	out, _ := json.Marshal(resp)
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","result":5,"id":1}
}

// greetParams is a struct because §4.2 requires structured params: a bare
// "world" is not a legal params value.
type greetParams struct {
	Name string `json:"name"`
}

func greet(_ context.Context, p greetParams) (string, error) {
	return "hello " + p.Name, nil
}

// ExampleClient_Call makes a one-line method call: params are marshaled,
// the id is generated, and the result is decoded into the target.
func ExampleClient_Call() {
	s := jsonrpc.NewServer()
	s.Register("greet", greet)

	c := jsonrpc.NewClient(s.Sender())

	var greeting string
	if err := c.Call(context.Background(), "greet", greetParams{Name: "world"}, &greeting); err != nil {
		fmt.Println("call failed:", err)
		return
	}
	fmt.Println(greeting)
	// Output: hello world
}

// ExampleClient_Send builds a Request with the constructors and sends it
// through an in-process Server.
func ExampleClient_Send() {
	s := jsonrpc.NewServer()
	s.Register("greet", greet)

	c := jsonrpc.NewClient(s.Sender())

	params, err := jsonrpc.NewParams(greetParams{Name: "world"})
	if err != nil {
		fmt.Println("bad params:", err)
		return
	}
	resp, err := c.Send(context.Background(), jsonrpc.NewRequest("greet", params, jsonrpc.MustNewID(1)))
	if err != nil {
		fmt.Println("transport error:", err)
		return
	}
	if resp.Error != nil {
		fmt.Println("rpc error:", resp.Error)
		return
	}
	var greeting string
	_ = resp.Decode(&greeting)
	fmt.Println(greeting)
	// Output: hello world
}

// ExampleMiddleware composes a cross-cutting concern with a typed handler. The
// middleware operates on raw params, so it never touches the typed pipeline.
func ExampleMiddleware() {
	// logging knows nothing about the handler's parameter or result types. The
	// returned func literal converts to RawHandler with no cast.
	logging := func(next jsonrpc.RawHandler) jsonrpc.RawHandler {
		return func(ctx context.Context, params jsontext.Value) (jsontext.Value, error) {
			fmt.Printf("calling with params: %s\n", params)
			return next(ctx, params)
		}
	}

	s := jsonrpc.NewServer()
	s.Use(logging) // applied to every method
	s.Register("add", func(_ context.Context, p struct {
		A int `json:"a"`
		B int `json:"b"`
	}) (int, error) {
		return p.A + p.B, nil
	})

	in := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":2,"b":3},"id":1}`)
	out, _ := s.ServeMessage(context.Background(), in)
	fmt.Println(string(out))
	// Output:
	// calling with params: {"a":2,"b":3}
	// {"jsonrpc":"2.0","result":5,"id":1}
}

// ExampleServer_ServeMessage shows the byte-level entry point used by
// transport adapters that work in raw messages.
func ExampleServer_ServeMessage() {
	s := jsonrpc.NewServer()
	// The spec allows params to be an object or an array only, so a typed
	// handler takes a struct or a slice, not a bare string.
	s.Register("echo", func(_ context.Context, p struct {
		Msg string `json:"msg"`
	}) (string, error) {
		return p.Msg, nil
	})

	in := []byte(`{"jsonrpc":"2.0","method":"echo","params":{"msg":"ping"},"id":1}`)
	out, _ := s.ServeMessage(context.Background(), in)
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","result":"ping","id":1}
}

// ExampleServer_SetOptions installs json/v2 options every typed method decodes
// and marshals with. Here a marshaler renders time.Time as Unix seconds without
// the result type carrying a MarshalJSONTo method of its own.
func ExampleServer_SetOptions() {
	s := jsonrpc.NewServer()
	s.SetOptions(json.WithMarshalers(
		json.MarshalToFunc(func(e *jsontext.Encoder, t time.Time) error {
			return e.WriteToken(jsontext.Int(t.Unix()))
		}),
	))
	s.Register("epoch", func(_ context.Context, _ struct{}) (struct {
		At time.Time `json:"at"`
	}, error) {
		return struct {
			At time.Time `json:"at"`
		}{At: time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)}, nil
	})

	out, _ := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"epoch","id":1}`))
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","result":{"at":1257894000},"id":1}
}

// ExampleClient_SetOptions gives the client the inverse of the wire form the
// server installed, so an in-process pair agrees on Unix seconds.
func ExampleClient_SetOptions() {
	type stamp struct {
		At time.Time `json:"at"`
	}
	epochSeconds := json.JoinOptions(
		json.WithMarshalers(json.MarshalToFunc(func(e *jsontext.Encoder, t time.Time) error {
			return e.WriteToken(jsontext.Int(t.Unix()))
		})),
		json.WithUnmarshalers(json.UnmarshalFromFunc(func(d *jsontext.Decoder, t *time.Time) error {
			var sec int64
			if err := json.UnmarshalDecode(d, &sec); err != nil {
				return err
			}
			*t = time.Unix(sec, 0).UTC()
			return nil
		})),
	)

	s := jsonrpc.NewServer()
	s.SetOptions(epochSeconds)
	s.Register("echo", func(_ context.Context, p stamp) (stamp, error) { return p, nil })

	c := jsonrpc.NewClient(s.Sender())
	c.SetOptions(epochSeconds) // without this the params go out as RFC 3339

	var got stamp
	if err := c.Call(context.Background(), "echo", stamp{At: time.Unix(1257894000, 0)}, &got); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(got.At.UTC())
	// Output: 2009-11-10 23:00:00 +0000 UTC
}

// ExampleServer_SetErrorHandler keeps an unclassified failure's detail off the
// wire. Errors the library classified arrive as *jsonrpc.Error and are already
// safe to send; anything else becomes a fixed message.
func ExampleServer_SetErrorHandler() {
	s := jsonrpc.NewServer()
	s.SetErrorHandler(func(_ context.Context, req *jsonrpc.Request, err error) *jsonrpc.Error {
		if e, ok := errors.AsType[*jsonrpc.Error](err); ok && e != nil {
			return e
		}
		fmt.Printf("log: rpc %s failed: %v\n", req.Method, err)
		return jsonrpc.NewError(jsonrpc.CodeInternalError, "internal error")
	})
	s.Register("lookup", func(_ context.Context, _ struct{}) (string, error) {
		return "", errors.New("dial postgres://user:hunter2@db: connection refused")
	})

	out, _ := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"lookup","id":1}`))
	fmt.Println(string(out))
	// Output:
	// log: rpc lookup failed: dial postgres://user:hunter2@db: connection refused
	// {"jsonrpc":"2.0","error":{"code":-32603,"message":"internal error"},"id":1}
}
