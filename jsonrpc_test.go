package jsonrpc

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
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
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	s.Register("fail", func(_ context.Context, _ struct{}) (any, error) {
		return nil, NewError(-32001, "custom").MustSetData(map[string]int{"x": 1})
	})
	s.Register("boom", func(_ context.Context, _ struct{}) (any, error) {
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
	c := NewClient(s.Sender())

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
	c := NewClient(s.Sender())

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
	c := NewClient(s.Sender())

	req := NewRequest("boom", mustParams(t, struct{}{}), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInternalError, resp.Error.Code)
}

func TestMethodNotFound(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("missing", nil, NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeMethodNotFound, resp.Error.Code)
}

func TestNotificationProducesNoResponse(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
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
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	c := NewClient(s.Sender())

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
	s.Register("nilerr", func(_ context.Context, _ struct{}) (any, error) {
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
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})
	c := NewClient(s.Sender())

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
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
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
			require.Equal(t, "Invalid Request", resp.Error.Message)
			var detail struct {
				Details string `json:"details"`
			}
			require.NoError(t, resp.Error.UnmarshalData(&detail))
			require.Contains(t, detail.Details, "id")
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

	c := NewClient(s.Sender())

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
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	}, requirePositive)

	c := NewClient(s.Sender())

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
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		log = append(log, "handler")
		return addResult{Sum: p.A + p.B}, nil
	}, tagMiddleware("method1", &log), tagMiddleware("method2", &log))

	c := NewClient(s.Sender())
	resp, err := c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: 1, B: 1}), NewID(1)))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	// Server middleware wraps around per-method middleware; mw[0] is outermost.
	require.Equal(t, []string{"server1", "server2", "method1", "method2", "handler"}, log)
}

func TestUseAfterRegisterPanics(t *testing.T) {
	s := NewServer()
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	require.Panics(t, func() {
		s.Use(func(next Handler) Handler { return next })
	})
}

func TestProtocolErrorsUseCanonicalMessages(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(), []byte(`{not valid json`))
	require.NoError(t, err)
	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.Equal(t, CodeParseError, resp.Error.Code)
	require.Equal(t, "Parse error", resp.Error.Message)

	var detail struct {
		Details string `json:"details"`
	}
	require.NoError(t, resp.Error.UnmarshalData(&detail))
	require.NotEmpty(t, detail.Details)

	r := s.Serve(context.Background(), NewRequest("missing", nil, NewID(1)))
	require.Equal(t, CodeMethodNotFound, r.Error.Code)
	require.Equal(t, "Method not found", r.Error.Message)
	require.NoError(t, r.Error.UnmarshalData(&detail))
	require.Contains(t, detail.Details, "missing")
}

func TestDuplicateMemberNamesRejected(t *testing.T) {
	s := newTestServer(t)

	t.Run("duplicate on envelope", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","method":"boom","id":1}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	})

	t.Run("duplicate inside params", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"a":2,"b":3},"id":1}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	})

	t.Run("duplicate params via Serve go through DecodeParams", func(t *testing.T) {
		// A transport calling Serve directly may not have tokenized params;
		// the typed pipeline still rejects duplicates, as Invalid params.
		resp := s.Serve(context.Background(), &Request{
			JSONRPC: Version,
			Method:  "add",
			Params:  json.RawMessage(`{"a":1,"a":2}`),
			ID:      NewID(1),
		})
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidParams, resp.Error.Code)
	})

	t.Run("duplicate in one batch element fails only that element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"add","method":"boom","id":2}
		]`))
		require.NoError(t, err)
		var resps []Response
		require.NoError(t, json.Unmarshal(out, &resps))
		require.Len(t, resps, 2)
		require.Nil(t, resps[0].Error)
		require.JSONEq(t, `{"sum":3}`, string(resps[0].Result))
		require.NotNil(t, resps[1].Error)
		require.Equal(t, CodeInvalidRequest, resps[1].Error.Code)
		// The offending element's id cannot be trusted, so the entry has id null.
		require.JSONEq(t, "null", string(resps[1].ID))
	})
}

func TestEnvelopeStrictness(t *testing.T) {
	s := newTestServer(t)

	t.Run("unknown envelope member rejected", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	})

	t.Run("unknown member inside params tolerated", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2,"ignored":9},"id":1}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	})
}

func TestParamsAsRawMessagePassThrough(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("add", json.RawMessage(`{"a":7,"b":8}`), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var got addResult
	require.NoError(t, resp.Decode(&got))
	require.Equal(t, 15, got.Sum)
}

// decodeDetails runs one message through ServeMessage and returns the single
// error response's code and its Data "details" string.
func decodeDetails(t *testing.T, s *Server, msg string) (int, string, json.RawMessage) {
	t.Helper()
	out, err := s.ServeMessage(context.Background(), []byte(msg))
	require.NoError(t, err)
	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.NotNil(t, resp.Error, "expected an error response for %s", msg)
	var detail struct {
		Details string `json:"details"`
	}
	require.NoError(t, resp.Error.UnmarshalData(&detail))
	return resp.Error.Code, detail.Details, resp.ID
}

func TestDefaultDecoderRejections(t *testing.T) {
	s := newTestServer(t)

	cases := []struct {
		name    string
		msg     string
		code    int
		details string
	}{
		{"number at top level", `5`, CodeInvalidRequest, "request must be a JSON object"},
		{"string at top level", `"hi"`, CodeInvalidRequest, "request must be a JSON object"},
		{"bool at top level", `true`, CodeInvalidRequest, "request must be a JSON object"},
		{"null at top level", `null`, CodeInvalidRequest, "request must be a JSON object"},
		{"non-string jsonrpc", `{"jsonrpc":2,"method":"add","id":1}`, CodeInvalidRequest, "jsonrpc must be a string"},
		{"non-string method", `{"jsonrpc":"2.0","method":5,"id":1}`, CodeInvalidRequest, "method must be a string"},
		{"unknown member", `{"jsonrpc":"2.0","method":"add","extra":true,"id":1}`, CodeInvalidRequest, "unknown member: extra"},
		{"scalar params", `{"jsonrpc":"2.0","method":"add","params":5,"id":1}`, CodeInvalidRequest, "params must be an object or array"},
		{"string params", `{"jsonrpc":"2.0","method":"add","params":"ping","id":1}`, CodeInvalidRequest, "params must be an object or array"},
		{"bool params", `{"jsonrpc":"2.0","method":"add","params":true,"id":1}`, CodeInvalidRequest, "params must be an object or array"},
		{"empty message", ``, CodeParseError, ""},
		{"truncated message", `{"jsonrpc":`, CodeParseError, ""},
		{"trailing value", `{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1} {"jsonrpc":"2.0"}`, CodeParseError, ""},
		{"trailing garbage", `{"jsonrpc":"2.0","method":"add","id":1} !`, CodeParseError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, details, _ := decodeDetails(t, s, tc.msg)
			require.Equal(t, tc.code, code)
			if tc.details != "" {
				require.Equal(t, tc.details, details)
			} else {
				require.NotEmpty(t, details)
			}
		})
	}
}

func TestDefaultDecoderAcceptsBothParamsShapes(t *testing.T) {
	s := NewServer()
	s.Register("byName", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	s.Register("byPosition", func(_ context.Context, p []int) (addResult, error) {
		return addResult{Sum: p[0] + p[1]}, nil
	})

	for _, msg := range []string{
		`{"jsonrpc":"2.0","method":"byName","params":{"a":1,"b":2},"id":1}`,
		`{"jsonrpc":"2.0","method":"byPosition","params":[1,2],"id":1}`,
	} {
		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	}
}

func TestDefaultDecoderTreatsNullParamsAsAbsent(t *testing.T) {
	s := NewServer()
	var seen json.RawMessage
	sawParams := false
	s.RegisterHandler("probe", func(_ context.Context, params json.RawMessage) (json.RawMessage, *Error) {
		seen, sawParams = params, true
		return json.RawMessage(`"ok"`), nil
	})

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"probe","params":null,"id":1}`))
	require.NoError(t, err)
	var resp Response
	require.NoError(t, json.Unmarshal(out, &resp))
	require.Nil(t, resp.Error)

	// An explicit null must reach the handler as absent params, not as the
	// four bytes "null" — that is what makes DecodeParams yield the zero P.
	require.True(t, sawParams)
	require.Nil(t, seen)
}

func TestDecodeErrorEchoesRecoveredID(t *testing.T) {
	s := newTestServer(t)

	t.Run("id read before the failure is echoed", func(t *testing.T) {
		// The id precedes the offending member, so the decoder has it in hand.
		_, _, id := decodeDetails(t, s, `{"jsonrpc":"2.0","id":7,"method":"add","params":5}`)
		require.JSONEq(t, "7", string(id))
	})

	t.Run("id after the failure is not recoverable", func(t *testing.T) {
		_, _, id := decodeDetails(t, s, `{"jsonrpc":"2.0","method":"add","params":5,"id":7}`)
		require.JSONEq(t, "null", string(id))
	})

	t.Run("unusable id is not echoed", func(t *testing.T) {
		_, _, id := decodeDetails(t, s, `{"jsonrpc":"2.0","id":{"bad":1},"method":"add","params":5}`)
		require.JSONEq(t, "null", string(id))
	})

	t.Run("wrong version still echoes the id", func(t *testing.T) {
		// The version verdict belongs to Serve precisely so the id survives.
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"1.0","method":"add","id":9}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
		require.JSONEq(t, "9", string(resp.ID))
	})
}

func TestSetRequestDecoderErrorPassThrough(t *testing.T) {
	custom := NewError(-32050, "bad envelope").MustSetData(map[string]string{"hint": "read the docs"})

	t.Run("bespoke *Error surfaces verbatim", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(json.RawMessage, *Request) error { return custom })
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"add","id":1}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		// protocolError must not have rewritten any of it.
		require.Equal(t, -32050, resp.Error.Code)
		require.Equal(t, "bad envelope", resp.Error.Message)
		var hint struct {
			Hint string `json:"hint"`
		}
		require.NoError(t, resp.Error.UnmarshalData(&hint))
		require.Equal(t, "read the docs", hint.Hint)
	})

	t.Run("wrapped *Error is unwrapped", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(json.RawMessage, *Request) error {
			return fmt.Errorf("decoding envelope: %w", custom)
		})

		out, err := s.ServeMessage(context.Background(), []byte(`{}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Equal(t, -32050, resp.Error.Code)
		require.Equal(t, "bad envelope", resp.Error.Message)
	})

	t.Run("plain error is classified as Invalid Request", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(json.RawMessage, *Request) error { return errors.New("nope") })

		code, details, _ := decodeDetails(t, s, `{}`)
		require.Equal(t, CodeInvalidRequest, code)
		require.Equal(t, "nope", details)
	})

	t.Run("typed-nil *Error does not read as success", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(json.RawMessage, *Request) error {
			var e *Error
			return fmt.Errorf("wrapped: %w", e)
		})

		code, _, _ := decodeDetails(t, s, `{}`)
		require.Equal(t, CodeInvalidRequest, code)
	})
}

func TestSetRequestDecoderReplacesBehavior(t *testing.T) {
	// A decoder that delegates to DecodeRequest but relaxes one of its rules:
	// unknown envelope members are dropped instead of rejected.
	lenient := func(data json.RawMessage, req *Request) error {
		if err := DecodeRequest(data, req); err != nil {
			var stripped map[string]json.RawMessage
			if jsonv2.Unmarshal(data, &stripped) != nil {
				return err
			}
			for k := range stripped {
				switch k {
				case "jsonrpc", "method", "params", "id":
				default:
					delete(stripped, k)
				}
			}
			clean, mErr := json.Marshal(stripped)
			if mErr != nil {
				return err
			}
			return DecodeRequest(clean, req)
		}
		return nil
	}

	s := NewServer()
	s.SetRequestDecoder(lenient)
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	t.Run("single message", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	})

	t.Run("applies to every batch element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true},
			{"jsonrpc":"2.0","method":"add","params":{"a":3,"b":4},"id":2,"other":1}
		]`))
		require.NoError(t, err)
		var resps []Response
		require.NoError(t, json.Unmarshal(out, &resps))
		require.Len(t, resps, 2)
		require.Nil(t, resps[0].Error)
		require.Nil(t, resps[1].Error)
		require.JSONEq(t, `{"sum":3}`, string(resps[0].Result))
		require.JSONEq(t, `{"sum":7}`, string(resps[1].Result))
	})

	t.Run("Serve still owns the version verdict", func(t *testing.T) {
		// Even a decoder that accepts anything cannot smuggle a bad version
		// past Serve, which validates every Request however it was built.
		s := NewServer()
		s.SetRequestDecoder(func(_ json.RawMessage, req *Request) error {
			req.JSONRPC, req.Method, req.ID = "1.0", "add", json.RawMessage("1")
			return nil
		})
		out, err := s.ServeMessage(context.Background(), []byte(`{}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
		require.JSONEq(t, "1", string(resp.ID))
	})
}

func TestSetRequestDecoderPanics(t *testing.T) {
	t.Run("nil decoder", func(t *testing.T) {
		s := NewServer()
		require.Panics(t, func() { s.SetRequestDecoder(nil) })
	})

	t.Run("after a method is registered", func(t *testing.T) {
		s := newTestServer(t)
		require.Panics(t, func() {
			s.SetRequestDecoder(func(json.RawMessage, *Request) error { return nil })
		})
	})
}
