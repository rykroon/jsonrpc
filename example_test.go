package jsonrpc_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rykroon/jsonrpc"
)

// ExampleServer registers a typed method and dispatches a request.
func ExampleServer() {
	s := jsonrpc.NewServer()
	jsonrpc.Register(s, "add", func(_ context.Context, p struct {
		A int `json:"a"`
		B int `json:"b"`
	}) (int, error) {
		return p.A + p.B, nil
	})

	req := &jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		Method:  "add",
		Params:  json.RawMessage(`{"a":2,"b":3}`),
		ID:      json.RawMessage("1"),
	}
	resp := s.Serve(context.Background(), req)
	out, _ := json.Marshal(resp)
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","result":5,"id":1}
}

// ExampleClient_Send builds a Request with the constructors and sends it
// through an in-process Server.
func ExampleClient_Send() {
	s := jsonrpc.NewServer()
	jsonrpc.Register(s, "greet", func(_ context.Context, name string) (string, error) {
		return "hello " + name, nil
	})

	c := jsonrpc.NewClient(jsonrpc.InProcess(s))

	params, _ := jsonrpc.NewParams("world")
	resp, err := c.Send(context.Background(), jsonrpc.NewRequest("greet", params, jsonrpc.NewID(1)))
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

// ExampleMiddleware shows a cross-cutting concern composed with a typed
// handler. The middleware operates on raw params, so it works without
// touching the typed pipeline or hand-wiring Dispatch.
func ExampleMiddleware() {
	// logging is reusable middleware: it knows nothing about the handler's
	// parameter or result types. The returned func literal converts to
	// HandlerFunc automatically — no cast needed.
	logging := func(next jsonrpc.HandlerFunc) jsonrpc.HandlerFunc {
		return func(ctx context.Context, params json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
			fmt.Printf("calling with params: %s\n", params)
			return next(ctx, params)
		}
	}

	s := jsonrpc.NewServer()
	s.Use(logging) // applied to every method
	jsonrpc.Register(s, "add", func(_ context.Context, p struct {
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
	jsonrpc.Register(s, "echo", func(_ context.Context, msg string) (string, error) {
		return msg, nil
	})

	in := []byte(`{"jsonrpc":"2.0","method":"echo","params":"ping","id":1}`)
	out, _ := s.ServeMessage(context.Background(), in)
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","result":"ping","id":1}
}
