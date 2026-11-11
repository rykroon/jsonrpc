package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMethodNotFound(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("missing", Params{}, IntID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.isError())
	require.Equal(t, CodeMethodNotFound, resp.error.Code())
}

func TestNotificationProducesNoResponse(t *testing.T) {
	s := NewServer()
	called := make(chan struct{}, 1)
	s.Register("ping", func(_ context.Context, _ struct{}) (struct{}, error) {
		called <- struct{}{}
		return struct{}{}, nil
	})

	resp := s.Serve(context.Background(), NewNotification("ping", Params{}))
	require.True(t, resp.IsZero())
	<-called
}

func TestNotificationUnknownMethodProducesNoResponse(t *testing.T) {
	s := newTestServer(t)

	resp := s.Serve(context.Background(), NewNotification("missing", Params{}))
	require.True(t, resp.IsZero())

	out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"missing"}`))
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestNilResultEncodesAsNull(t *testing.T) {
	s := NewServer()
	s.RegisterRaw("void", func(_ context.Context, _ Params) (jsontext.Value, error) {
		return nil, nil
	})

	out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"void","id":1}`))
	require.NoError(t, err)
	require.Contains(t, string(out), `"result":null`)
}

func TestInvalidIDNotEchoedOnVersionError(t *testing.T) {
	s := newTestServer(t)
	code, _, id := decodeError(t, s, `{"jsonrpc":"1.0","method":"add","id":{"x":1}}`)
	require.Equal(t, CodeInvalidRequest, code)
	require.True(t, id.IsNull())
}

func TestInvalidJSONRPCVersion(t *testing.T) {
	s := NewServer()
	resp := s.Serve(context.Background(), Request{
		jsonrpc: "1.0",
		method:  "anything",
		id:      IntID(1),
	})
	require.False(t, resp.IsZero())
	require.True(t, resp.isError())
	require.Equal(t, CodeInvalidRequest, resp.error.Code())
	require.JSONEq(t, "1", resp.id.String())
}

func TestServeReturnsResponseShapes(t *testing.T) {
	s := newTestServer(t)

	ok := s.Serve(context.Background(), NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), IntID(1)))
	require.False(t, ok.isError())
	require.JSONEq(t, `{"sum":5}`, string(ok.result))

	bad := s.Serve(context.Background(), NewRequest("missing", Params{}, IntID(1)))
	require.True(t, bad.isError())
	require.Nil(t, bad.result, "an error response carries no result")
	require.NoError(t, bad.DecodeResult(new(addResult)), "there is no result to decode")
}

func TestMessageServerSingleRequest(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.False(t, resp.isError())
	require.JSONEq(t, `{"sum":3}`, string(resp.result))
	require.JSONEq(t, "1", resp.id.String())
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

	resp := decodeResponse(t, out)
	require.True(t, resp.isError())
	require.Equal(t, CodeParseError, resp.error.Code())
	require.JSONEq(t, "null", resp.id.String())
}

func TestBatchTwoCalls(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[
		{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
		{"jsonrpc":"2.0","method":"add","params":{"a":10,"b":20},"id":2}
	]`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resps := decodeResponses(t, out)
	require.Len(t, resps, 2)
	require.False(t, resps[0].isError())
	require.JSONEq(t, "1", resps[0].id.String())
	require.JSONEq(t, `{"sum":3}`, string(resps[0].result))
	require.False(t, resps[1].isError())
	require.JSONEq(t, "2", resps[1].id.String())
	require.JSONEq(t, `{"sum":30}`, string(resps[1].result))
}

func TestBatchMixedCallsAndNotifications(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[
		{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2}},
		{"jsonrpc":"2.0","method":"add","params":{"a":2,"b":3},"id":7}
	]`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resps := decodeResponses(t, out)
	require.Len(t, resps, 1)
	require.JSONEq(t, "7", resps[0].id.String())
	require.JSONEq(t, `{"sum":5}`, string(resps[0].result))
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
	resp := decodeResponse(t, out)
	require.True(t, resp.isError())
	require.Equal(t, CodeInvalidRequest, resp.error.Code())
	require.JSONEq(t, "null", resp.id.String())
}

func TestBatchInvalidElements(t *testing.T) {
	s := newTestServer(t)

	t.Run("single invalid element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1]`))
		require.NoError(t, err)

		resps := decodeResponses(t, out)
		require.Len(t, resps, 1)
		require.True(t, resps[0].isError())
		require.Equal(t, CodeInvalidRequest, resps[0].error.Code())
		require.JSONEq(t, "null", resps[0].id.String())
	})

	t.Run("three invalid elements", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1,2,3]`))
		require.NoError(t, err)

		resps := decodeResponses(t, out)
		require.Len(t, resps, 3)
		for _, r := range resps {
			require.True(t, r.isError())
			require.Equal(t, CodeInvalidRequest, r.error.Code())
		}
	})
}

func TestBatchMalformedJSONIsSingleParseError(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[{"jsonrpc":"2.0","method":"add","id":1},{"jsonrpc":`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.True(t, resp.isError())
	require.Equal(t, CodeParseError, resp.error.Code())
	require.JSONEq(t, "null", resp.id.String())
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

	resps := decodeResponses(t, out)
	// The notification is omitted; the valid call, the garbage element, and
	// the bad-version request each produce an entry, in request order.
	require.Len(t, resps, 3)

	require.False(t, resps[0].isError())
	require.JSONEq(t, `"ok"`, resps[0].id.String())
	require.JSONEq(t, `{"sum":3}`, string(resps[0].result))

	require.True(t, resps[1].isError())
	require.Equal(t, CodeInvalidRequest, resps[1].error.Code())
	require.JSONEq(t, "null", resps[1].id.String())

	require.True(t, resps[2].isError())
	require.Equal(t, CodeInvalidRequest, resps[2].error.Code())
	require.JSONEq(t, "9", resps[2].id.String())
}

func TestMessageServerInvalidShape(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`12345`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.True(t, resp.isError())
	require.Equal(t, CodeInvalidRequest, resp.error.Code())
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
			code, msg, id := decodeError(t, s, `{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":`+tc.id+`}`)
			require.Equal(t, CodeInvalidRequest, code)
			require.Contains(t, msg, "id")
			require.True(t, id.IsNull())
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
			out, err := s.ServeMessage(context.Background(),
				[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":`+tc.id+`}`))
			require.NoError(t, err)
			resp := decodeResponse(t, out)
			require.False(t, resp.isError())
			require.JSONEq(t, tc.id, resp.id.String())
		})
	}
}

func TestRawWithHandlerValidation(t *testing.T) {
	s := NewServer()
	// Validation lives in the handler, next to the code that depends on it,
	// and owns the full Error including structured Data. The decode above it
	// only reports params that do not fit P.
	add := Raw(func(_ context.Context, p addParams) (addResult, error) {
		if p.A < 0 || p.B < 0 {
			return addResult{}, withData(NewError(CodeInvalidParams, "operands must be non-negative"),
				map[string]any{"a": p.A, "b": p.B})
		}
		return addResult{Sum: p.A + p.B}, nil
	})
	// A Raw value installs like any other RawHandler, per-method middleware
	// included.
	var log []string
	s.RegisterRaw("add", add, tagMiddleware("mw", &log))

	c := NewClient(s.Sender())

	resp, err := c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), IntID(1)))
	require.NoError(t, err)
	require.False(t, resp.isError())
	var ok addResult
	require.NoError(t, resp.DecodeResult(&ok))
	require.Equal(t, 5, ok.Sum)

	resp, err = c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: -1, B: 3}), IntID(2)))
	require.NoError(t, err)
	require.True(t, resp.isError())
	require.Equal(t, CodeInvalidParams, resp.error.Code())

	var detail map[string]int
	require.NoError(t, resp.error.DecodeData(&detail))
	require.Equal(t, -1, detail["a"])
	require.Equal(t, 3, detail["b"])

	require.Equal(t, []string{"mw", "mw"}, log, "middleware ran on both calls")
}

// tagMiddleware appends its name to *log when the request passes through,
// letting tests assert ordering of the wrapping.
func tagMiddleware(name string, log *[]string) Middleware {
	return func(next RawHandler) RawHandler {
		return func(ctx context.Context, raw Params) (jsontext.Value, error) {
			*log = append(*log, name)
			return next(ctx, raw)
		}
	}
}

type authKey struct{}

func TestRegisterMiddlewareRunsBeforeDecode(t *testing.T) {
	s := NewServer()
	// Cross-cutting middleware that rejects on the raw layer without ever
	// decoding into P — the concern middleware is for, now that params
	// validation belongs to the handler.
	requireAuth := func(next RawHandler) RawHandler {
		return func(ctx context.Context, raw Params) (jsontext.Value, error) {
			if ctx.Value(authKey{}) != "secret" {
				return nil, NewError(CodeServerError, "unauthorized")
			}
			return next(ctx, raw)
		}
	}
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	}, requireAuth)

	c := NewClient(s.Sender())
	authed := context.WithValue(context.Background(), authKey{}, "secret")

	resp, err := c.Send(authed, NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), IntID(1)))
	require.NoError(t, err)
	require.False(t, resp.isError())
	var ok addResult
	require.NoError(t, resp.DecodeResult(&ok))
	require.Equal(t, 5, ok.Sum)

	// Unauthorized, and with params that could not decode into P either. The
	// middleware's error is what comes back, so it ran before the decode.
	resp, err = c.Send(context.Background(), NewRequest("add", mustParams(t, jsontext.Value(`{"a":"x"}`)), IntID(2)))
	require.NoError(t, err)
	require.True(t, resp.isError())
	require.Equal(t, CodeServerError, resp.error.Code())
	require.Equal(t, "unauthorized", resp.error.Message())
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
	resp, err := c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: 1, B: 1}), IntID(1)))
	require.NoError(t, err)
	require.False(t, resp.isError())
	// Server middleware wraps around per-method middleware; mw[0] is outermost.
	require.Equal(t, []string{"server1", "server2", "method1", "method2", "handler"}, log)
}

// TestRegisterRejectsEmptyName: Serve rejects method "" as a missing method
// before the map lookup, so an empty registration is unreachable code.
func TestRegisterRejectsEmptyName(t *testing.T) {
	s := NewServer()
	require.Panics(t, func() {
		s.Register("", func(context.Context, struct{}) (int, error) { return 1, nil })
	})
	require.Panics(t, func() {
		s.RegisterRaw("", func(context.Context, Params) (jsontext.Value, error) { return nil, nil })
	})
}

func TestUseAfterRegisterPanics(t *testing.T) {
	s := NewServer()
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})
	require.Panics(t, func() {
		s.Use(func(next RawHandler) RawHandler { return next })
	})
}

func TestProtocolErrorsCarryTheCauseAsMessage(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(), []byte(`{not valid json`))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.Equal(t, CodeParseError, resp.error.Code())
	require.NotEmpty(t, resp.error.Message())
	require.Empty(t, resp.error.Data(), "library errors attach no Data")

	r := s.Serve(context.Background(), NewRequest("missing", Params{}, IntID(1)))
	require.Equal(t, CodeMethodNotFound, r.error.Code())
	require.Contains(t, r.error.Message(), "missing")
	require.Empty(t, r.error.Data(), "library errors attach no Data")
}

func TestDuplicateMemberNamesRejected(t *testing.T) {
	s := newTestServer(t)

	t.Run("duplicate on envelope", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","method":"boom","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.True(t, resp.isError())
		require.Equal(t, CodeInvalidRequest, resp.error.Code())
	})

	t.Run("duplicate inside params", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"a":2,"b":3},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.True(t, resp.isError())
		require.Equal(t, CodeInvalidRequest, resp.error.Code())
	})

	t.Run("duplicate params via Serve go through the typed decode", func(t *testing.T) {
		// A transport calling Serve directly may not have tokenized params;
		// the typed pipeline still rejects duplicates, as Invalid params.
		resp := s.Serve(context.Background(), Request{
			jsonrpc: Version,
			method:  "add",
			// NewParams would reject the duplicate, so build it directly.
			params: Params{value: jsontext.Value(`{"a":1,"a":2}`)},
			id:     IntID(1),
		})
		require.True(t, resp.isError())
		require.Equal(t, CodeInvalidParams, resp.error.Code())
	})

	t.Run("duplicate in one batch element fails only that element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"add","method":"boom","id":2}
		]`))
		require.NoError(t, err)
		resps := decodeResponses(t, out)
		require.Len(t, resps, 2)
		require.False(t, resps[0].isError())
		require.JSONEq(t, `{"sum":3}`, string(resps[0].result))
		require.True(t, resps[1].isError())
		require.Equal(t, CodeInvalidRequest, resps[1].error.Code())
		// The offending element's id cannot be trusted, so the entry has id null.
		require.JSONEq(t, "null", resps[1].id.String())
	})
}

func TestEnvelopeStrictness(t *testing.T) {
	s := newTestServer(t)

	t.Run("unknown envelope member rejected", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.True(t, resp.isError())
		require.Equal(t, CodeInvalidRequest, resp.error.Code())
	})

	t.Run("unknown member inside params tolerated", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2,"ignored":9},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.False(t, resp.isError())
		require.JSONEq(t, `{"sum":3}`, string(resp.result))
	})
}

// decodeError runs one message through ServeMessage and returns the single
// error response's code and message.
func decodeError(t *testing.T, s *Server, msg string) (int, string, ID) {
	t.Helper()
	out, err := s.ServeMessage(context.Background(), []byte(msg))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.True(t, resp.isError(), "expected an error response for %s", msg)
	return resp.error.Code(), resp.error.Message(), resp.id
}

func TestDefaultDecoderRejections(t *testing.T) {
	s := newTestServer(t)

	cases := []struct {
		name    string
		msg     string
		code    int
		message string
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
		{"null params", `{"jsonrpc":"2.0","method":"add","params":null,"id":1}`, CodeInvalidRequest, "params must be an object or array"},
		{"empty message", ``, CodeParseError, ""},
		{"truncated message", `{"jsonrpc":`, CodeParseError, ""},
		{"trailing value", `{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1} {"jsonrpc":"2.0"}`, CodeParseError, ""},
		{"trailing garbage", `{"jsonrpc":"2.0","method":"add","id":1} !`, CodeParseError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, message, _ := decodeError(t, s, tc.msg)
			require.Equal(t, tc.code, code)
			if tc.message != "" {
				require.Equal(t, tc.message, message)
			} else {
				require.NotEmpty(t, message)
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
		resp := decodeResponse(t, out)
		require.False(t, resp.isError())
		require.JSONEq(t, `{"sum":3}`, string(resp.result))
	}
}

func TestDefaultDecoderOmittedParams(t *testing.T) {
	s := NewServer()
	var seen Params
	called := false
	s.RegisterRaw("probe", func(_ context.Context, params Params) (jsontext.Value, error) {
		seen, called = params, true
		return jsontext.Value(`"ok"`), nil
	})

	// Omitting the member is the only way to send no parameters; "params":null
	// is a present member holding a non-structured value, which §4.2 forbids.
	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"probe","id":1}`))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.False(t, resp.isError())
	require.True(t, called)
	require.True(t, seen.IsZero())
}

func TestDecodeErrorEchoesRecoveredID(t *testing.T) {
	s := newTestServer(t)

	t.Run("id read before the failure is echoed", func(t *testing.T) {
		// The id precedes the offending member, so the decoder has it in hand.
		_, _, id := decodeError(t, s, `{"jsonrpc":"2.0","id":7,"method":"add","params":5}`)
		require.JSONEq(t, "7", id.String())
	})

	t.Run("id after the failure is not recoverable", func(t *testing.T) {
		_, _, id := decodeError(t, s, `{"jsonrpc":"2.0","method":"add","params":5,"id":7}`)
		require.JSONEq(t, "null", id.String())
	})

	t.Run("unusable id is not echoed", func(t *testing.T) {
		_, _, id := decodeError(t, s, `{"jsonrpc":"2.0","id":{"bad":1},"method":"add","params":5}`)
		require.JSONEq(t, "null", id.String())
	})

	t.Run("wrong version still echoes the id", func(t *testing.T) {
		// The version verdict belongs to Serve precisely so the id survives.
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"1.0","method":"add","id":9}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Equal(t, CodeInvalidRequest, resp.error.Code())
		require.JSONEq(t, "9", resp.id.String())
	})
}

// The server's options are payload-only: an unmarshaler for Request or a
// marshaler for Response or Error never reaches the envelope.
func TestSetOptionsDoesNotReachTheEnvelope(t *testing.T) {
	s := NewServer()
	s.SetOptions(
		json.WithUnmarshalers(json.UnmarshalFromFunc(func(*jsontext.Decoder, *Request) error {
			return errors.New("request unmarshaler ran")
		})),
		json.WithMarshalers(json.JoinMarshalers(
			json.MarshalToFunc(func(*jsontext.Encoder, Response) error {
				return errors.New("response marshaler ran")
			}),
			json.MarshalToFunc(func(*jsontext.Encoder, Error) error {
				return errors.New("error marshaler ran")
			}),
		)),
	)
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	t.Run("single", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`))
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(out))
	})

	t.Run("batch", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"missing","id":2}
		]`))
		require.NoError(t, err)
		require.JSONEq(t, `[
			{"jsonrpc":"2.0","result":{"sum":3},"id":1},
			{"jsonrpc":"2.0","error":{"code":-32601,"message":"method not found: missing"},"id":2}
		]`, string(out))
	})

	t.Run("a message that never reaches Serve", func(t *testing.T) {
		code, _, _ := decodeError(t, s, `{`)
		require.Equal(t, CodeParseError, code)
	})
}

func TestSetOptionsParamsUnmarshaler(t *testing.T) {
	const msg = `{"jsonrpc":"2.0","method":"echo","params":{"t":"21.5C"},"id":1}`

	t.Run("without options the string is Invalid params", func(t *testing.T) {
		s := NewServer()
		s.Register("echo", echoTemp)
		code, _, _ := decodeError(t, s, msg)
		require.Equal(t, CodeInvalidParams, code)
	})

	t.Run("the installed unmarshaler decodes P", func(t *testing.T) {
		var calls int
		s := NewServer()
		s.SetOptions(json.WithUnmarshalers(tempUnmarshaler(&calls)))
		s.Register("echo", echoTemp)

		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.False(t, resp.isError())
		require.Equal(t, 1, calls)
		// No marshaler was installed, so the result is the default number.
		require.JSONEq(t, `{"t":21.5}`, string(resp.result))
	})

	t.Run("omitted params never reach the unmarshaler", func(t *testing.T) {
		var calls int
		s := NewServer()
		s.SetOptions(json.WithUnmarshalers(tempUnmarshaler(&calls)))
		s.Register("echo", echoTemp)

		out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"echo","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.False(t, resp.isError())
		require.Equal(t, 0, calls)
		require.JSONEq(t, `{"t":0}`, string(resp.result))
	})
}

func TestSetOptionsResultMarshaler(t *testing.T) {
	s := NewServer()
	s.SetOptions(json.WithMarshalers(tempMarshaler))
	s.Register("echo", echoTemp)

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"echo","params":{"t":21.5},"id":1}`))
	require.NoError(t, err)
	// Check the bytes, not a decoded value: the marshaler's output must be
	// what reaches the wire.
	require.JSONEq(t, `{"jsonrpc":"2.0","result":{"t":"21.5C"},"id":1}`, string(out))
}

func TestSetOptionsAppliesToParamsAndBatch(t *testing.T) {
	s := NewServer()
	s.SetOptions(json.MatchCaseInsensitiveNames(true))
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	t.Run("json option reaches the params decode", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"A":1,"B":2},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.False(t, resp.isError())
		require.JSONEq(t, `{"sum":3}`, string(resp.result))
	})

	t.Run("json option does not loosen the envelope", func(t *testing.T) {
		// The envelope is token-walked, so member matching there is exact.
		code, msg, _ := decodeError(t, s, `{"jsonrpc":"2.0","Method":"add","id":1}`)
		require.Equal(t, CodeInvalidRequest, code)
		require.Equal(t, "unknown member: Method", msg)
	})

	t.Run("batch split still isolates a duplicate to its element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"add","method":"boom","id":2}
		]`))
		require.NoError(t, err)
		resps := decodeResponses(t, out)
		require.Len(t, resps, 2)
		require.False(t, resps[0].isError())
		require.True(t, resps[1].isError())
		require.Equal(t, CodeInvalidRequest, resps[1].error.Code())
	})
}

func TestSetOptionsPanicsAfterRegister(t *testing.T) {
	s := newTestServer(t)
	require.Panics(t, func() { s.SetOptions(json.WithMarshalers(tempMarshaler)) })
}

func TestRawWithOptions(t *testing.T) {
	// One method gets its own options through Raw + RegisterRaw; the server's
	// options stay at their defaults, as the sibling method shows.
	s := NewServer()
	opts := json.JoinOptions(json.WithUnmarshalers(tempUnmarshaler(nil)), json.WithMarshalers(tempMarshaler))
	s.RegisterRaw("strings", Raw(echoTemp, opts))
	s.Register("numbers", echoTemp)

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"strings","params":{"t":"21.5C"},"id":1}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","result":{"t":"21.5C"},"id":1}`, string(out))

	code, _, _ := decodeError(t, s, `{"jsonrpc":"2.0","method":"numbers","params":{"t":"21.5C"},"id":2}`)
	require.Equal(t, CodeInvalidParams, code)
}

// errDBUnavailable stands in for an application sentinel a handler wraps and
// an ErrorHandler recognizes.
var errDBUnavailable = errors.New("db unavailable")

func TestSetErrorHandlerSanitizesUnclassifiedErrors(t *testing.T) {
	var logged error
	s := NewServer()
	s.SetErrorHandler(func(ctx context.Context, req Request, err error) Error {
		if e, ok := errors.AsType[Error](err); ok {
			return e
		}
		logged = err
		return NewError(CodeServerError, "internal error")
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("dial postgres://user:hunter2@db: refused")
	})

	resp := s.Serve(context.Background(), NewRequest("boom", Params{}, IntID(1)))
	require.True(t, resp.isError())
	require.Equal(t, CodeServerError, resp.error.Code())
	// The detail stayed with the operator instead of going to the client.
	require.Equal(t, "internal error", resp.error.Message())
	require.EqualError(t, logged, "dial postgres://user:hunter2@db: refused")
}

func TestErrorHandlerCannotChangeResponseID(t *testing.T) {
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, req Request, err error) Error {
		req.id = StringID("hijacked")
		return DefaultErrorHandler(context.Background(), req, err)
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("boom")
	})
	ctx := context.Background()

	t.Run("Serve", func(t *testing.T) {
		resp := s.Serve(ctx, NewRequest("boom", Params{}, IntID(1)))
		require.Equal(t, IntID(1), resp.id)
		resp = s.Serve(ctx, NewRequest("missing", Params{}, IntID(2)))
		require.Equal(t, IntID(2), resp.id)
	})

	t.Run("decode failure", func(t *testing.T) {
		out, err := s.ServeMessage(ctx, jsontext.Value(`{"jsonrpc":"2.0","id":3,"method":"boom","extra":1}`))
		require.NoError(t, err)
		var resp Response
		require.NoError(t, json.Unmarshal(out, &resp))
		require.Equal(t, IntID(3), resp.id)
	})

	t.Run("batch decode failure", func(t *testing.T) {
		out, err := s.ServeMessage(ctx, jsontext.Value(`[{"jsonrpc":"2.0","id":4,"method":"boom","extra":1}]`))
		require.NoError(t, err)
		resps, err := DecodeResponses(out)
		require.NoError(t, err)
		require.Len(t, resps, 1)
		require.Equal(t, IntID(4), resps[0].id)
	})
}

func TestErrorHandlerSeesClassifiedErrors(t *testing.T) {
	// An Error a component raised still reaches the handler, so a verbose
	// json/v2 message can be replaced.
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, _ Request, err error) Error {
		e, ok := errors.AsType[Error](err)
		require.True(t, ok, "expected the component's Error, got %T", err)
		return NewError(e.Code(), "invalid params")
	})
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	code, msg, _ := decodeError(t, s, `{"jsonrpc":"2.0","method":"add","params":{"a":"x"},"id":1}`)
	require.Equal(t, CodeInvalidParams, code)
	require.Equal(t, "invalid params", msg)
}

func TestErrorHandlerReceivesRequest(t *testing.T) {
	var gotMethod string
	var gotID ID
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, req Request, err error) Error {
		gotMethod, gotID = req.Method(), req.ID()
		return DefaultErrorHandler(context.Background(), req, err)
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("boom")
	})

	s.Serve(context.Background(), NewRequest("boom", Params{}, IntID(7)))
	require.Equal(t, "boom", gotMethod)
	require.JSONEq(t, "7", gotID.String())
}

func TestErrorHandlerObservesNotificationFailure(t *testing.T) {
	var seen error
	var wasNotification bool
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, req Request, err error) Error {
		seen, wasNotification = err, req.IsNotification()
		return NewError(CodeInternalError, "unused")
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("boom")
	})

	// The spec allows no reply, but the failure is no longer silent.
	resp := s.Serve(context.Background(), NewNotification("boom", Params{}))
	require.True(t, resp.IsZero())
	require.EqualError(t, seen, "boom")
	require.True(t, wasNotification)
}

func TestErrorHandlerCoversDecodePath(t *testing.T) {
	t.Run("envelope decode", func(t *testing.T) {
		var got Request
		s := NewServer()
		s.SetErrorHandler(func(_ context.Context, req Request, err error) Error {
			got = req
			return NewError(CodeServerError, "rejected")
		})
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		// id precedes the offending member, so the decoder had read it before
		// it failed and the handler sees it.
		code, msg, _ := decodeError(t, s, `{"jsonrpc":"2.0","id":1,"method":"add","surprise":true}`)
		require.Equal(t, CodeServerError, code)
		require.Equal(t, "rejected", msg)
		require.Equal(t, "add", got.Method())
		require.JSONEq(t, "1", got.ID().String())
	})

	t.Run("batch-level failure passes a zero request", func(t *testing.T) {
		var called bool
		s := NewServer()
		s.SetErrorHandler(func(_ context.Context, req Request, err error) Error {
			called = true
			require.Zero(t, req, "no request could be decoded")
			return NewError(CodeServerError, "rejected")
		})
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		code, _, _ := decodeError(t, s, `[]`)
		require.True(t, called)
		require.Equal(t, CodeServerError, code)
	})
}

func TestMiddlewareCanWrapErrors(t *testing.T) {
	// Wrapping is what a plain error return buys: the middleware adds context
	// with %w and the ErrorHandler still recognizes the sentinel underneath.
	s := NewServer()
	s.SetErrorHandler(func(ctx context.Context, req Request, err error) Error {
		if errors.Is(err, errDBUnavailable) {
			return withData(NewError(CodeServerError, "service unavailable"),
				map[string]string{"detail": err.Error()})
		}
		return DefaultErrorHandler(ctx, req, err)
	})
	s.Use(func(next RawHandler) RawHandler {
		return func(ctx context.Context, raw Params) (jsontext.Value, error) {
			out, err := next(ctx, raw)
			if err != nil {
				return nil, fmt.Errorf("lookup: %w", err)
			}
			return out, nil
		}
	})
	s.Register("lookup", func(context.Context, struct{}) (any, error) {
		return nil, fmt.Errorf("query users: %w", errDBUnavailable)
	})

	resp := s.Serve(context.Background(), NewRequest("lookup", Params{}, IntID(1)))
	require.True(t, resp.isError())
	require.Equal(t, CodeServerError, resp.error.Code())
	require.Equal(t, "service unavailable", resp.error.Message())
	var data struct {
		Detail string `json:"detail"`
	}
	require.NoError(t, resp.error.DecodeData(&data))
	require.Equal(t, "lookup: query users: db unavailable", data.Detail)
}

func TestSetErrorHandlerPanics(t *testing.T) {
	t.Run("nil handler", func(t *testing.T) {
		s := NewServer()
		require.Panics(t, func() { s.SetErrorHandler(nil) })
	})

	t.Run("after a method is registered", func(t *testing.T) {
		s := newTestServer(t)
		require.Panics(t, func() {
			s.SetErrorHandler(func(context.Context, Request, error) Error { return Error{} })
		})
	})
}
