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

func mustParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	p, err := NewParams(v)
	require.NoError(t, err)
	return p
}

func TestClientSend(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	req := NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error)

	var got addResult
	require.NoError(t, resp.Decode(&got))
	require.Equal(t, addResult{Sum: 5}, got)
}

func TestClientSendRPCError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	req := NewRequest("fail", mustParams(t, struct{}{}), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32001, resp.Error.Code)
	require.Equal(t, "custom", resp.Error.Message)
	require.JSONEq(t, `{"x":1}`, string(resp.Error.Data))
}

func TestClientSendInternalError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	req := NewRequest("boom", mustParams(t, struct{}{}), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInternalError, resp.Error.Code)
}

func TestMethodNotFound(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	req := NewRequest("missing", nil, NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeMethodNotFound, resp.Error.Code)
}

func TestNotificationProducesNoResponse(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	Register(s, "ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})

	resp := s.Serve(context.Background(), NewNotification("ping", nil))
	require.Nil(t, resp)
	<-called
}

func TestClientNotify(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	Register(s, "ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	c := NewClient(InProcess(s))

	resp, err := c.Send(context.Background(), NewNotification("ping", nil))
	require.NoError(t, err)
	require.Nil(t, resp)
	<-called
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := NewServer()
	resp := s.Serve(context.Background(), &Request{
		JSONRPC: "1.0",
		Method:  "anything",
		ID:      NewID(1),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	require.JSONEq(t, "1", string(resp.ID))
}

func TestRequestRoundTripPreservesStringID(t *testing.T) {
	in := NewRequest("x", nil, NewID("abc"))
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out Request
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, `"abc"`, string(out.ID))
	require.False(t, out.IsNotification())
}

func TestRequestNotificationHasNoIDField(t *testing.T) {
	req := NewNotification("x", nil)
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

func TestMessageServerSingleRequest(t *testing.T) {
	m := &MessageServer{Server: newTestServer(t)}
	data := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`)
	out, err := m.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.Nil(t, resp.Error)
	require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	require.JSONEq(t, "1", string(resp.ID))
}

func TestMessageServerNotification(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	Register(s, "ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	m := &MessageServer{Server: s}
	data := []byte(`{"jsonrpc":"2.0","method":"ping"}`)
	out, err := m.ServeMessage(context.Background(), data)
	require.NoError(t, err)
	require.Nil(t, out)
	<-called
}

func TestMessageServerParseError(t *testing.T) {
	m := &MessageServer{Server: newTestServer(t)}
	data := []byte(`{not valid json`)
	out, err := m.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeParseError, resp.Error.Code)
	require.JSONEq(t, "null", string(resp.ID))
}

func TestMessageServerBatchRejected(t *testing.T) {
	m := &MessageServer{Server: newTestServer(t)}
	data := []byte(` [{"jsonrpc":"2.0","method":"add","id":1}]`)
	out, err := m.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	require.Contains(t, resp.Error.Message, "batch")
}

func TestMessageServerInvalidShape(t *testing.T) {
	m := &MessageServer{Server: newTestServer(t)}
	data := []byte(`12345`)
	out, err := m.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
}

func TestServerRejectsInvalidIDs(t *testing.T) {
	s := newTestServer(t)

	cases := []struct {
		name string
		id   string
	}{
		{"bool true", "true"},
		{"bool false", "false"},
		{"object", `{"x":1}`},
		{"array", "[1,2,3]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.Serve(context.Background(), &Request{
				JSONRPC: Version,
				Method:  "add",
				Params:  json.RawMessage(`{"a":1,"b":2}`),
				ID:      json.RawMessage(tc.id),
			})
			require.NotNil(t, resp)
			require.NotNil(t, resp.Error)
			require.Equal(t, CodeInvalidRequest, resp.Error.Code)
			require.Contains(t, resp.Error.Message, "id")
			require.JSONEq(t, "null", string(resp.ID))
		})
	}
}

func TestServerAcceptsValidIDs(t *testing.T) {
	s := newTestServer(t)

	cases := []struct {
		name string
		id   string
	}{
		{"positive int", "42"},
		{"negative int", "-7"},
		{"zero", "0"},
		{"string", `"abc"`},
		{"empty string", `""`},
		{"large uint64", "18446744073709551615"},
		{"float", "1.5"},
		{"exponential", "1e2"},
		{"null", "null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.Serve(context.Background(), &Request{
				JSONRPC: Version,
				Method:  "add",
				Params:  json.RawMessage(`{"a":1,"b":2}`),
				ID:      json.RawMessage(tc.id),
			})
			require.NotNil(t, resp)
			require.Nil(t, resp.Error)
			require.JSONEq(t, tc.id, string(resp.ID))
		})
	}
}

func TestSendPropagatesCallerID(t *testing.T) {
	s := newTestServer(t)
	var seen json.RawMessage
	c := NewClient(SenderFunc(func(ctx context.Context, req *Request) (*Response, error) {
		seen = append(seen[:0], req.ID...)
		return s.Serve(ctx, req), nil
	}))

	req := NewRequest("add", mustParams(t, addParams{A: 1, B: 2}), NewID("req-abc"))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.JSONEq(t, `"req-abc"`, string(seen))
	require.JSONEq(t, `"req-abc"`, string(resp.ID))
}

func TestNewIDIntegerForms(t *testing.T) {
	require.JSONEq(t, "42", string(NewID(42)))
	require.JSONEq(t, "-7", string(NewID(int64(-7))))
	require.JSONEq(t, "18446744073709551615", string(NewID(uint64(1<<64-1))))
	require.JSONEq(t, `"abc"`, string(NewID("abc")))
}

func TestNewParamsPassthrough(t *testing.T) {
	raw := json.RawMessage(`{"a":1}`)
	out, err := NewParams(raw)
	require.NoError(t, err)
	require.Equal(t, string(raw), string(out))

	out, err = NewParams(nil)
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestDispatchWithCustomValidator(t *testing.T) {
	s := NewServer()
	// Pre-decode validator owns the full *Error including structured Data.
	requirePositive := func(raw json.RawMessage) *Error {
		var p addParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return NewError(CodeInvalidParams, err.Error())
		}
		if p.A < 0 || p.B < 0 {
			return NewError(CodeInvalidParams, "operands must be non-negative").
				WithData(map[string]any{"a": p.A, "b": p.B})
		}
		return nil
	}
	s.RegisterHandler("add", HandlerFunc(func(ctx context.Context, raw json.RawMessage) (json.RawMessage, *Error) {
		if vErr := requirePositive(raw); vErr != nil {
			return nil, vErr
		}
		return Dispatch(ctx, raw, func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})
	}))

	c := NewClient(InProcess(s))

	resp, err := c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), NewID(1)))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var ok addResult
	require.NoError(t, resp.Decode(&ok))
	require.Equal(t, 5, ok.Sum)

	resp, err = c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: -1, B: 3}), NewID(2)))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidParams, resp.Error.Code)

	var detail map[string]int
	require.NoError(t, resp.Error.UnmarshalData(&detail))
	require.Equal(t, -1, detail["a"])
	require.Equal(t, 3, detail["b"])
}

func TestParamsAsRawMessagePassThrough(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	req := NewRequest("add", json.RawMessage(`{"a":7,"b":8}`), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var got addResult
	require.NoError(t, resp.Decode(&got))
	require.Equal(t, 15, got.Sum)
}
