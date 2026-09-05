package jsonrpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"

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

// ExampleClient_Call makes a one-line method call: params are marshaled,
// the id is generated, and the result is decoded into the target.
func ExampleClient_Call() {
	s := jsonrpc.NewServer()
	s.Register("greet", func(_ context.Context, name string) (string, error) {
		return "hello " + name, nil
	})

	c := jsonrpc.NewClient(s.Sender())

	var greeting string
	if err := c.Call(context.Background(), "greet", "world", &greeting); err != nil {
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
	s.Register("greet", func(_ context.Context, name string) (string, error) {
		return "hello " + name, nil
	})

	c := jsonrpc.NewClient(s.Sender())

	params, _ := jsonrpc.NewParams("world")
	resp, err := c.Send(context.Background(), jsonrpc.NewRequest("greet", params, jsonrpc.NewID(1)))
	if err != nil {
		fmt.Println("transport error:", err)
		return
	}
	if resp.IsError() {
		fmt.Println("rpc error:", resp.Error())
		return
	}
	var greeting string
	_ = resp.Decode(&greeting)
	fmt.Println(greeting)
	// Output: hello world
}

// ExampleMiddleware shows a cross-cutting concern composed with a typed
// handler. The middleware operates on raw params, so it works without
// touching the typed pipeline.
func ExampleMiddleware() {
	// logging is reusable middleware: it knows nothing about the handler's
	// parameter or result types. The returned func literal converts to
	// Handler automatically — no cast needed.
	logging := func(next jsonrpc.Handler) jsonrpc.Handler {
		return func(ctx context.Context, params jsontext.Value) (jsontext.Value, *jsonrpc.Error) {
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
	// params arrives as an object or an array — the spec allows no other
	// shape — so a typed handler takes a struct or a slice, not a bare string.
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

// ExampleServer_SetRequestDecoder replaces the request decoder to control the
// errors reported for a bad envelope. This one delegates to the package
// default and then enriches its error, keeping the code and message the
// default chose while adding the offending message to Data.
func ExampleServer_SetRequestDecoder() {
	type errorData struct {
		Got string `json:"got"`
	}

	s := jsonrpc.NewServer()
	s.SetRequestDecoder(func(d *jsontext.Decoder, req *jsonrpc.Request) error {
		// One ReadValue consumes the decoder's one value and hands back the
		// raw message, which the error below quotes.
		raw, err := d.ReadValue()
		if err != nil {
			return err
		}
		raw = raw.Clone()
		err = jsonrpc.DecodeRequest(jsontext.NewDecoder(bytes.NewReader(raw)), req)
		e, ok := errors.AsType[*jsonrpc.Error](err)
		if !ok {
			// Not a classified error: let the server classify it.
			return err
		}
		// Returning an *Error hands the client exactly this object.
		return jsonrpc.NewError(e.Code, e.Message).MustSetData(errorData{Got: string(raw)})
	})
	s.Register("add", func(_ context.Context, p struct {
		A, B int
	}) (int, error) {
		return p.A + p.B, nil
	})

	out, _ := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","surprise":true,"id":1}`))
	fmt.Println(string(out))
	// Output: {"jsonrpc":"2.0","error":{"code":-32600,"message":"unknown member: surprise","data":{"got":"{\"jsonrpc\":\"2.0\",\"method\":\"add\",\"surprise\":true,\"id\":1}"}},"id":null}
}
