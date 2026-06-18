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

// ExampleHandleMessage shows the byte-level entry point used by transport
// adapters that work in raw messages.
func ExampleHandleMessage() {
	s := jsonrpc.NewServer()
	jsonrpc.Register(s, "echo", func(_ context.Context, msg string) (string, error) {
		return msg, nil
	})

	in := []byte(`{"jsonrpc":"2.0","method":"echo","params":"ping","id":1}`)
	out, _ := jsonrpc.HandleMessage(context.Background(), in, s.Serve)
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","result":"ping","id":1}
}
