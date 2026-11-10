package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientSend(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), IntID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.isError())

	var got addResult
	require.NoError(t, resp.DecodeResult(&got))
	require.Equal(t, addResult{Sum: 5}, got)
}

func TestClientSendRPCError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("fail", mustParams(t, struct{}{}), IntID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.isError())
	require.Equal(t, -32001, resp.error.Code())
	require.Equal(t, "custom", resp.error.Message())
	require.JSONEq(t, `{"x":1}`, string(resp.error.Data()))
}

func TestClientSendInternalError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("boom", mustParams(t, struct{}{}), IntID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.isError())
	require.Equal(t, CodeInternalError, resp.error.Code())
}

func TestClientSendNotification(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	c := NewClient(s.Sender())

	resp, err := c.Send(context.Background(), NewNotification("ping", Params{}))
	require.NoError(t, err)
	require.True(t, resp.IsZero())
	<-called
}

func TestClientCall(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	var got addResult
	require.NoError(t, c.Call(context.Background(), "add", addParams{A: 2, B: 3}, &got))
	require.Equal(t, addResult{Sum: 5}, got)

	// A nil result target skips decoding.
	require.NoError(t, c.Call(context.Background(), "add", addParams{A: 1, B: 1}, nil))
}

func TestClientCallServerError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	err := c.Call(context.Background(), "fail", struct{}{}, nil)
	require.Error(t, err)

	var rpcErr Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, -32001, rpcErr.Code())
	require.Equal(t, "custom", rpcErr.Message())

	var detail map[string]int
	require.NoError(t, rpcErr.DecodeData(&detail))
	require.Equal(t, 1, detail["x"])
}

func TestClientCallGeneratesUniqueIDs(t *testing.T) {
	s := newTestServer(t)
	var ids []string
	c := NewClient(SenderFunc(func(ctx context.Context, req Request) (Response, error) {
		ids = append(ids, req.ID().String())
		return s.Serve(ctx, req), nil
	}))

	require.NoError(t, c.Call(context.Background(), "add", addParams{}, nil))
	require.NoError(t, c.Call(context.Background(), "add", addParams{}, nil))
	require.Equal(t, []string{"1", "2"}, ids)
}

func TestClientNotify(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	c := NewClient(s.Sender())

	require.NoError(t, c.Notify(context.Background(), "ping", nil))
	<-called
}

// Call must not decode another request's response as its own.
func TestClientCallChecksResponseID(t *testing.T) {
	respond := func(resp Response) *Client {
		return NewClient(SenderFunc(func(context.Context, Request) (Response, error) {
			return resp, nil
		}))
	}

	t.Run("a mismatched success is rejected undecoded", func(t *testing.T) {
		got := addResult{Sum: -1}
		err := respond(NewSuccessResponse(jsontext.Value(`{"sum":5}`), IntID(99))).
			Call(context.Background(), "add", nil, &got)
		require.ErrorContains(t, err, "response id 99 does not match request id 1")
		require.Equal(t, addResult{Sum: -1}, got)
	})

	t.Run("a mismatched error is not reported as the server's", func(t *testing.T) {
		err := respond(NewErrorResponse(NewError(CodeInternalError, "theirs"), StringID("1"))).
			Call(context.Background(), "add", nil, nil)
		require.ErrorContains(t, err, `response id "1" does not match request id 1`)
		_, isRPC := errors.AsType[Error](err)
		require.False(t, isRPC)
	})

	t.Run("a null id is accepted on an error", func(t *testing.T) {
		err := respond(NewErrorResponse(NewError(CodeParseError, "bad"), ID{})).
			Call(context.Background(), "add", nil, nil)
		e, ok := errors.AsType[Error](err)
		require.True(t, ok)
		require.Equal(t, CodeParseError, e.Code())
	})

	t.Run("but not on a success", func(t *testing.T) {
		err := respond(NewSuccessResponse(jsontext.Value(`1`), ID{})).
			Call(context.Background(), "add", nil, nil)
		require.ErrorContains(t, err, "response id null does not match")
	})
}

func TestSendPropagatesCallerID(t *testing.T) {
	s := newTestServer(t)
	var seen ID
	c := NewClient(SenderFunc(func(ctx context.Context, req Request) (Response, error) {
		seen = req.ID()
		return s.Serve(ctx, req), nil
	}))

	req := NewRequest("add", mustParams(t, addParams{A: 1, B: 2}), StringID("req-abc"))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.isError())
	require.Equal(t, StringID("req-abc"), seen)
	require.JSONEq(t, `"req-abc"`, resp.id.String())
}

func TestClientSetOptions(t *testing.T) {
	// tempOptions is the wire form both sides must agree on: "21.5C", not 21.5.
	tempOptions := json.JoinOptions(
		json.WithUnmarshalers(tempUnmarshaler(nil)),
		json.WithMarshalers(tempMarshaler),
	)
	newServer := func() *Server {
		s := NewServer()
		s.SetOptions(tempOptions)
		s.Register("echo", echoTemp)
		return s
	}

	t.Run("a client without the options cannot reach the server", func(t *testing.T) {
		c := NewClient(newServer().Sender())
		var got tempResult
		err := c.Call(context.Background(), "echo", tempParams{T: 21.5}, &got)
		// The client marshals a bare number; the server's unmarshaler wants a string.
		var e Error
		require.ErrorAs(t, err, &e)
		require.Equal(t, CodeInvalidParams, e.Code())
	})

	t.Run("matching options round-trip params and result", func(t *testing.T) {
		c := NewClient(newServer().Sender())
		c.SetOptions(tempOptions)

		var got tempResult
		require.NoError(t, c.Call(context.Background(), "echo", tempParams{T: 21.5}, &got))
		require.Equal(t, temperature(21.5), got.T)
	})

	t.Run("the options reach the params Notify marshals", func(t *testing.T) {
		var seen Params
		s := NewServer()
		s.RegisterRaw("note", func(_ context.Context, params Params) (jsontext.Value, error) {
			seen = params
			return nil, nil
		})
		c := NewClient(s.Sender())
		c.SetOptions(tempOptions)

		require.NoError(t, c.Notify(context.Background(), "note", tempParams{T: 21.5}))
		require.JSONEq(t, `{"t":"21.5C"}`, seen.String())
	})
}

func TestClientSetOptionsPanicsAfterUse(t *testing.T) {
	tempOptions := json.WithMarshalers(tempMarshaler)
	newIdleClient := func() *Client {
		s := NewServer()
		s.Register("echo", echoTemp)
		return NewClient(s.Sender())
	}

	t.Run("after Call", func(t *testing.T) {
		c := newIdleClient()
		require.NoError(t, c.Call(context.Background(), "echo", tempParams{}, nil))
		require.Panics(t, func() { c.SetOptions(tempOptions) })
	})

	t.Run("after Notify", func(t *testing.T) {
		c := newIdleClient()
		require.NoError(t, c.Notify(context.Background(), "echo", tempParams{}))
		require.Panics(t, func() { c.SetOptions(tempOptions) })
	})

	t.Run("Send marshals nothing, so it leaves the options open", func(t *testing.T) {
		c := newIdleClient()
		_, err := c.Send(context.Background(), NewRequest("echo", Params{}, IntID(1)))
		require.NoError(t, err)
		require.NotPanics(t, func() { c.SetOptions(tempOptions) })
	})
}
