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
		return nil, NewError(-32001, "custom").MustSetData(map[string]int{"x": 1})
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

func TestClientSendNotification(t *testing.T) {
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

func TestNotificationUnknownMethodProducesNoResponse(t *testing.T) {
	s := newTestServer(t)

	resp := s.Serve(context.Background(), NewNotification("missing", nil))
	require.Nil(t, resp)

	out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"missing"}`))
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestNilResultEncodesAsNull(t *testing.T) {
	s := NewServer()
	s.RegisterHandler("void", func(_ context.Context, _ json.RawMessage) (json.RawMessage, *Error) {
		return nil, nil
	})

	out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"void","id":1}`))
	require.NoError(t, err)
	require.Contains(t, string(out), `"result":null`)
}

func TestTypedNilErrorBecomesInternalError(t *testing.T) {
	s := NewServer()
	Register(s, "nilerr", func(_ context.Context, _ struct{}) (any, error) {
		return nil, (*Error)(nil)
	})

	resp := s.Serve(context.Background(), NewRequest("nilerr", nil, NewID(1)))
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInternalError, resp.Error.Code)
}

func TestInvalidIDNotEchoedOnVersionError(t *testing.T) {
	s := newTestServer(t)
	resp := s.Serve(context.Background(), &Request{
		JSONRPC: "1.0",
		Method:  "add",
		ID:      json.RawMessage(`{"x":1}`),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	require.JSONEq(t, "null", string(resp.ID))
}

func TestClientCall(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	var got addResult
	require.NoError(t, c.Call(context.Background(), "add", addParams{A: 2, B: 3}, &got))
	require.Equal(t, addResult{Sum: 5}, got)

	// A nil result target skips decoding.
	require.NoError(t, c.Call(context.Background(), "add", addParams{A: 1, B: 1}, nil))
}

func TestClientCallServerError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(InProcess(s))

	err := c.Call(context.Background(), "fail", struct{}{}, nil)
	require.Error(t, err)

	var rpcErr *Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, -32001, rpcErr.Code)
	require.Equal(t, "custom", rpcErr.Message)

	var detail map[string]int
	require.NoError(t, rpcErr.UnmarshalData(&detail))
	require.Equal(t, 1, detail["x"])
}

func TestErrorSetData(t *testing.T) {
	e := NewError(-32001, "custom")
	require.NoError(t, e.SetData(map[string]int{"x": 1}))
	require.JSONEq(t, `{"x":1}`, string(e.Data))

	// On marshal failure Data keeps its previous value.
	require.Error(t, e.SetData(make(chan int)))
	require.JSONEq(t, `{"x":1}`, string(e.Data))
}

func TestErrorMustSetData(t *testing.T) {
	e := NewError(-32001, "custom").MustSetData([]int{1, 2})
	require.JSONEq(t, `[1,2]`, string(e.Data))

	require.Panics(t, func() {
		NewError(-32001, "custom").MustSetData(make(chan int))
	})
}

func TestClientCallGeneratesUniqueIDs(t *testing.T) {
	s := newTestServer(t)
	var ids []string
	c := NewClient(SenderFunc(func(ctx context.Context, req *Request) (*Response, error) {
		ids = append(ids, string(req.ID))
		return s.Serve(ctx, req), nil
	}))

	require.NoError(t, c.Call(context.Background(), "add", addParams{}, nil))
	require.NoError(t, c.Call(context.Background(), "add", addParams{}, nil))
	require.Equal(t, []string{"1", "2"}, ids)
}

func TestClientNotify(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	Register(s, "ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	c := NewClient(InProcess(s))

	require.NoError(t, c.Notify(context.Background(), "ping", nil))
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
	s := newTestServer(t)
	data := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`)
	out, err := s.ServeMessage(context.Background(), data)
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
	data := []byte(`{"jsonrpc":"2.0","method":"ping"}`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)
	require.Nil(t, out)
	<-called
}

func TestMessageServerParseError(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`{not valid json`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeParseError, resp.Error.Code)
	require.JSONEq(t, "null", string(resp.ID))
}

func TestBatchTwoCalls(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[
		{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
		{"jsonrpc":"2.0","method":"add","params":{"a":10,"b":20},"id":2}
	]`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resps []Response
	require.NoError(t, json.Unmarshal(out, &resps))
	require.Len(t, resps, 2)
	require.Nil(t, resps[0].Error)
	require.JSONEq(t, "1", string(resps[0].ID))
	require.JSONEq(t, `{"sum":3}`, string(resps[0].Result))
	require.Nil(t, resps[1].Error)
	require.JSONEq(t, "2", string(resps[1].ID))
	require.JSONEq(t, `{"sum":30}`, string(resps[1].Result))
}

func TestBatchMixedCallsAndNotifications(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[
		{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2}},
		{"jsonrpc":"2.0","method":"add","params":{"a":2,"b":3},"id":7}
	]`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resps []Response
	require.NoError(t, json.Unmarshal(out, &resps))
	require.Len(t, resps, 1)
	require.JSONEq(t, "7", string(resps[0].ID))
	require.JSONEq(t, `{"sum":5}`, string(resps[0].Result))
}

func TestBatchAllNotificationsProducesNoReply(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[
		{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2}},
		{"jsonrpc":"2.0","method":"missing"}
	]`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestBatchEmptyIsSingleError(t *testing.T) {
	s := newTestServer(t)
	out, err := s.ServeMessage(context.Background(), []byte(`[]`))
	require.NoError(t, err)

	// The spec answers an empty batch with one Response object, not an array.
	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	require.JSONEq(t, "null", string(resp.ID))
}

func TestBatchInvalidElements(t *testing.T) {
	s := newTestServer(t)

	t.Run("single invalid element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1]`))
		require.NoError(t, err)

		var resps []Response
		require.NoError(t, json.Unmarshal(out, &resps))
		require.Len(t, resps, 1)
		require.NotNil(t, resps[0].Error)
		require.Equal(t, CodeInvalidRequest, resps[0].Error.Code)
		require.JSONEq(t, "null", string(resps[0].ID))
	})

	t.Run("three invalid elements", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1,2,3]`))
		require.NoError(t, err)

		var resps []Response
		require.NoError(t, json.Unmarshal(out, &resps))
		require.Len(t, resps, 3)
		for _, r := range resps {
			require.NotNil(t, r.Error)
			require.Equal(t, CodeInvalidRequest, r.Error.Code)
		}
	})
}

func TestBatchMalformedJSONIsSingleParseError(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[{"jsonrpc":"2.0","method":"add","id":1},{"jsonrpc":`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeParseError, resp.Error.Code)
	require.JSONEq(t, "null", string(resp.ID))
}

func TestBatchMixedValidAndInvalid(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[
		{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":"ok"},
		"garbage",
		{"jsonrpc":"2.0","method":"add","params":{"a":0,"b":0}},
		{"jsonrpc":"1.0","method":"add","id":9}
	]`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	var resps []Response
	require.NoError(t, json.Unmarshal(out, &resps))
	// The notification is omitted; the valid call, the garbage element, and
	// the bad-version request each produce an entry, in request order.
	require.Len(t, resps, 3)

	require.Nil(t, resps[0].Error)
	require.JSONEq(t, `"ok"`, string(resps[0].ID))
	require.JSONEq(t, `{"sum":3}`, string(resps[0].Result))

	require.NotNil(t, resps[1].Error)
	require.Equal(t, CodeInvalidRequest, resps[1].Error.Code)
	require.JSONEq(t, "null", string(resps[1].ID))

	require.NotNil(t, resps[2].Error)
	require.Equal(t, CodeInvalidRequest, resps[2].Error.Code)
	require.JSONEq(t, "9", string(resps[2].ID))
}

func TestMessageServerInvalidShape(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`12345`)
	out, err := s.ServeMessage(context.Background(), data)
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

func TestTypedWithValidationMiddleware(t *testing.T) {
	s := NewServer()
	// Pre-decode validation middleware owns the full *Error including
	// structured Data, then delegates to the typed handler.
	requirePositive := func(next Handler) Handler {
		return func(ctx context.Context, raw json.RawMessage) (json.RawMessage, *Error) {
			var p addParams
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, NewError(CodeInvalidParams, err.Error())
			}
			if p.A < 0 || p.B < 0 {
				return nil, NewError(CodeInvalidParams, "operands must be non-negative").
					MustSetData(map[string]any{"a": p.A, "b": p.B})
			}
			return next(ctx, raw)
		}
	}
	add := Typed(func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	s.RegisterHandler("add", add, requirePositive)

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

// tagMiddleware appends its name to *log when the request passes through,
// letting tests assert ordering of the wrapping.
func tagMiddleware(name string, log *[]string) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, raw json.RawMessage) (json.RawMessage, *Error) {
			*log = append(*log, name)
			return next(ctx, raw)
		}
	}
}

func TestRegisterMiddlewareValidatesBeforeDecode(t *testing.T) {
	s := NewServer()
	// A raw middleware that rejects without ever decoding into the typed P.
	requirePositive := func(next Handler) Handler {
		return func(ctx context.Context, raw json.RawMessage) (json.RawMessage, *Error) {
			var p addParams
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, NewError(CodeInvalidParams, err.Error())
			}
			if p.A < 0 || p.B < 0 {
				return nil, NewError(CodeInvalidParams, "operands must be non-negative")
			}
			return next(ctx, raw)
		}
	}
	Register(s, "add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	}, requirePositive)

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
}

func TestMiddlewareOrdering(t *testing.T) {
	var log []string
	s := NewServer()
	s.Use(tagMiddleware("server1", &log), tagMiddleware("server2", &log))
	Register(s, "add", func(_ context.Context, p addParams) (addResult, error) {
		log = append(log, "handler")
		return addResult{Sum: p.A + p.B}, nil
	}, tagMiddleware("method1", &log), tagMiddleware("method2", &log))

	c := NewClient(InProcess(s))
	resp, err := c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: 1, B: 1}), NewID(1)))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	// Server middleware wraps around per-method middleware; mw[0] is outermost.
	require.Equal(t, []string{"server1", "server2", "method1", "method2", "handler"}, log)
}

func TestUseAfterRegisterPanics(t *testing.T) {
	s := NewServer()
	Register(s, "add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	require.Panics(t, func() {
		s.Use(func(next Handler) Handler { return next })
	})
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
