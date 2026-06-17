package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type addParams struct {
	A int `json:"a"`
	B int `json:"b"`
}

type addResult struct {
	Sum int `json:"sum"`
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s := NewServer()
	Register(s, "add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	Register(s, "fail", func(_ context.Context, _ struct{}) (any, error) {
		return nil, NewError(-32001, "custom").WithData(map[string]int{"x": 1})
	})
	Register(s, "boom", func(_ context.Context, _ struct{}) (any, error) {
		return nil, errors.New("internal boom")
	})
	return s
}

func TestClientCall(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	var got addResult
	err := c.Call(context.Background(), "add", addParams{A: 2, B: 3}, &got)
	require.NoError(t, err)
	require.Equal(t, addResult{Sum: 5}, got)
}

func TestClientCallRPCError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	err := c.Call(context.Background(), "fail", struct{}{}, nil)
	var rpcErr *Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, -32001, rpcErr.Code)
	require.Equal(t, "custom", rpcErr.Message)
	require.JSONEq(t, `{"x":1}`, string(rpcErr.Data))
}

func TestClientCallInternalError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	err := c.Call(context.Background(), "boom", struct{}{}, nil)
	var rpcErr *Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, CodeInternalError, rpcErr.Code)
}

func TestMethodNotFound(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	err := c.Call(context.Background(), "missing", nil, nil)
	var rpcErr *Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, CodeMethodNotFound, rpcErr.Code)
}

func TestNotificationProducesNoResponse(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	Register(s, "ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})

	resp := s.Serve(context.Background(), &Request{
		JSONRPC: Version,
		Method:  "ping",
	})
	require.Nil(t, resp)
	<-called
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := NewServer()
	resp := s.Serve(context.Background(), &Request{
		JSONRPC: "1.0",
		Method:  "anything",
		ID:      json.RawMessage("1"),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	require.JSONEq(t, "1", string(resp.ID))
}

func TestRequestRoundTripPreservesStringID(t *testing.T) {
	in := &Request{
		JSONRPC: Version,
		Method:  "x",
		ID:      json.RawMessage(`"abc"`),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out Request
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, `"abc"`, string(out.ID))
	require.False(t, out.IsNotification())
}

func TestRequestNotificationHasNoIDField(t *testing.T) {
	req := &Request{JSONRPC: Version, Method: "x"}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"id"`)
	require.True(t, req.IsNotification())
}

func TestResponseAlwaysHasID(t *testing.T) {
	resp := &Response{JSONRPC: Version, ID: json.RawMessage("null"), Error: NewError(CodeParseError, "bad")}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)
}

func TestParamsAsRawMessagePassThrough(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	var got addResult
	err := c.Call(context.Background(), "add", json.RawMessage(`{"a":7,"b":8}`), &got)
	require.NoError(t, err)
	require.Equal(t, 15, got.Sum)
}
