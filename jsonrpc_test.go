package jsonrpc

import (
	"bytes"
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
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

func mustParams(t *testing.T, v any) jsontext.Value {
	t.Helper()
	p, err := NewParams(v)
	require.NoError(t, err)
	return p
}

// decodeResponse parses one marshaled response off the wire.
func decodeResponse(t *testing.T, data jsontext.Value) *Response {
	t.Helper()
	resp, err := DecodeResponse(data)
	require.NoError(t, err)
	return resp
}

// decodeResponses parses a marshaled batch reply.
func decodeResponses(t *testing.T, data jsontext.Value) []*Response {
	t.Helper()
	resps, err := DecodeResponses(data)
	require.NoError(t, err)
	return resps
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
	s.RegisterRaw("void", func(_ context.Context, _ jsontext.Value) (jsontext.Value, error) {
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
		ID:      jsontext.Value(`{"x":1}`),
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
	// Both shapes carry an id member, even when it is null, and a success
	// response always carries a result member, even when it is null. The
	// constructors normalize an empty value, so this holds by construction.
	b, err := json.Marshal(NewErrorResponse(NewError(CodeParseError, "bad"), nil))
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)

	b, err = json.Marshal(NewSuccessResponse(nil, nil))
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)
	require.Contains(t, string(b), `"result":null`)
}

func TestResponseMarshalsExactShape(t *testing.T) {
	ok := NewSuccessResponse(jsontext.Value(`{"sum":3}`), NewID(1))
	b, err := json.Marshal(ok)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(b))

	bad := NewErrorResponse(NewError(CodeMethodNotFound, "nope"), NewID("x"))
	b, err = json.Marshal(bad)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":"x"}`, string(b))

	// Member order must not depend on which json package the caller reaches
	// for: the examples marshal responses with v1, ServeMessage with v2.
	v1, err := jsonv1.Marshal(ok)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(v1))
}

// A hand-built Response can hold a shape the spec forbids. Marshaling reports
// it rather than emitting it, and rejects a malformed raw result the same way.
func TestResponseMarshalRejectsInvalidShape(t *testing.T) {
	_, err := json.Marshal(&Response{ID: NewID(1)})
	require.ErrorContains(t, err, "neither result nor error", "a response must carry one of the two")

	_, err = json.Marshal(&Response{
		Result: jsontext.Value("1"),
		Error:  NewError(CodeInternalError, "x"),
		ID:     NewID(1),
	})
	require.ErrorContains(t, err, "both result and error")

	_, err = json.Marshal(&Response{Result: jsontext.Value(`{oops`), ID: NewID(1)})
	require.Error(t, err, "a malformed result must not reach the wire")

	// An empty id is not malformed: the spec's own fallback is null.
	b, err := json.Marshal(&Response{Result: jsontext.Value("1")})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":null}`, string(b))

	// Nor is a version the caller never set: the protocol's is written, not the
	// field's.
	b, err = json.Marshal(&Response{Result: jsontext.Value("1"), ID: NewID(1)})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":1}`, string(b))
}

func TestServeReturnsResponseShapes(t *testing.T) {
	s := newTestServer(t)

	ok := s.Serve(context.Background(), NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), NewID(1)))
	require.Nil(t, ok.Error)
	require.JSONEq(t, `{"sum":5}`, string(ok.Result))

	bad := s.Serve(context.Background(), NewRequest("missing", nil, NewID(1)))
	require.NotNil(t, bad.Error)
	require.Nil(t, bad.Result, "an error response carries no result")
	require.NoError(t, bad.Decode(new(addResult)), "there is no result to decode")
}

func TestDecodeResponse(t *testing.T) {
	const (
		fails = iota
		success
		failure
	)
	cases := []struct {
		name string
		in   string
		want int // the shape expected, or fails when the decode must not succeed
	}{
		{"success", `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, success},
		{"null result is a success", `{"jsonrpc":"2.0","result":null,"id":1}`, success},
		{"error", `{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":1}`, failure},
		{"null id", `{"jsonrpc":"2.0","error":{"code":-32700,"message":"nope"},"id":null}`, failure},
		{"unknown members are tolerated", `{"jsonrpc":"2.0","result":1,"id":1,"extra":true}`, success},
		{"both result and error", `{"jsonrpc":"2.0","result":1,"error":{"code":-1,"message":"x"},"id":1}`, fails},
		{"neither result nor error", `{"jsonrpc":"2.0","id":1}`, fails},
		{"wrong version", `{"jsonrpc":"1.0","result":1,"id":1}`, fails},
		{"missing id", `{"jsonrpc":"2.0","result":1}`, fails},
		{"duplicate member", `{"jsonrpc":"2.0","result":1,"result":2,"id":1}`, fails},
		{"not an object", `[1]`, fails},
		{"malformed", `{"jsonrpc":`, fails},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeResponse([]byte(tc.in))
			if tc.want == fails {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, got.ID)
			require.Equal(t, Version, got.JSONRPC)
			if tc.want == success {
				require.Nil(t, got.Error)
				require.NotEmpty(t, got.Result)
			} else {
				require.NotNil(t, got.Error)
				require.Nil(t, got.Result)
			}
		})
	}
}

func TestDecodeResponseRoundTrip(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":"abc"}`))
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.Nil(t, resp.Error)
	require.JSONEq(t, `"abc"`, string(resp.ID))

	var got addResult
	require.NoError(t, resp.Decode(&got))
	require.Equal(t, addResult{Sum: 3}, got)
}

func TestDecodeResponsesRejectsBadElement(t *testing.T) {
	_, err := DecodeResponses([]byte(`[{"jsonrpc":"2.0","result":1,"id":1},{"jsonrpc":"2.0","id":2}]`))
	require.ErrorContains(t, err, "element 1")

	// A single response is not a batch.
	_, err = DecodeResponses([]byte(`{"jsonrpc":"2.0","result":1,"id":1}`))
	require.Error(t, err)
}

// A Sender may build a Response itself, so Call must not read a response with
// neither member as a success with nothing to decode.
func TestClientCallResponseWithNeitherMember(t *testing.T) {
	c := NewClient(SenderFunc(func(_ context.Context, req *Request) (*Response, error) {
		return &Response{JSONRPC: Version, ID: req.ID}, nil
	}))

	var got addResult
	err := c.Call(context.Background(), "add", addParams{A: 1, B: 2}, &got)
	require.ErrorContains(t, err, "neither result nor error")
	_, isRPCErr := errors.AsType[*Error](err)
	require.False(t, isRPCErr)
}

func TestMessageServerSingleRequest(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
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

	resp := decodeResponse(t, out)
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

	resps := decodeResponses(t, out)
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

	resps := decodeResponses(t, out)
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
	resp := decodeResponse(t, out)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	require.JSONEq(t, "null", string(resp.ID))
}

func TestBatchInvalidElements(t *testing.T) {
	s := newTestServer(t)

	t.Run("single invalid element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1]`))
		require.NoError(t, err)

		resps := decodeResponses(t, out)
		require.Len(t, resps, 1)
		require.NotNil(t, resps[0].Error)
		require.Equal(t, CodeInvalidRequest, resps[0].Error.Code)
		require.JSONEq(t, "null", string(resps[0].ID))
	})

	t.Run("three invalid elements", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1,2,3]`))
		require.NoError(t, err)

		resps := decodeResponses(t, out)
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

	resp := decodeResponse(t, out)
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

	resps := decodeResponses(t, out)
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

	resp := decodeResponse(t, out)
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
				Params:  jsontext.Value(`{"a":1,"b":2}`),
				ID:      jsontext.Value(tc.id),
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
				Params:  jsontext.Value(`{"a":1,"b":2}`),
				ID:      jsontext.Value(tc.id),
			})
			require.NotNil(t, resp)
			require.Nil(t, resp.Error)
			require.JSONEq(t, tc.id, string(resp.ID))
		})
	}
}

func TestSendPropagatesCallerID(t *testing.T) {
	s := newTestServer(t)
	var seen jsontext.Value
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
	raw := jsontext.Value(`{"a":1}`)
	out, err := NewParams(raw)
	require.NoError(t, err)
	require.Equal(t, string(raw), string(out))

	out, err = NewParams(nil)
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestRawWithHandlerValidation(t *testing.T) {
	s := NewServer()
	// Validation lives in the handler, next to the code that depends on it,
	// and owns the full *Error including structured Data. The decode above it
	// only reports params that do not fit P.
	add := Raw(func(_ context.Context, p addParams) (addResult, error) {
		if p.A < 0 || p.B < 0 {
			return addResult{}, NewError(CodeInvalidParams, "operands must be non-negative").
				MustSetData(map[string]any{"a": p.A, "b": p.B})
		}
		return addResult{Sum: p.A + p.B}, nil
	})
	// A Raw value installs like any other RawHandler, per-method middleware
	// included.
	var log []string
	s.RegisterRaw("add", add, tagMiddleware("mw", &log))

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

	require.Equal(t, []string{"mw", "mw"}, log, "middleware ran on both calls")
}

// tagMiddleware appends its name to *log when the request passes through,
// letting tests assert ordering of the wrapping.
func tagMiddleware(name string, log *[]string) Middleware {
	return func(next RawHandler) RawHandler {
		return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, error) {
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
		return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, error) {
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

	resp, err := c.Send(authed, NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), NewID(1)))
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var ok addResult
	require.NoError(t, resp.Decode(&ok))
	require.Equal(t, 5, ok.Sum)

	// Unauthorized, and with params that could not decode into P either. The
	// middleware's error is what comes back, so it ran before the decode.
	resp, err = c.Send(context.Background(), NewRequest("add", jsontext.Value(`{"a":"x"}`), NewID(2)))
	require.NoError(t, err)
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeServerError, resp.Error.Code)
	require.Equal(t, "unauthorized", resp.Error.Message)
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
		s.Use(func(next RawHandler) RawHandler { return next })
	})
}

func TestProtocolErrorsCarryTheCauseAsMessage(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(), []byte(`{not valid json`))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.Equal(t, CodeParseError, resp.Error.Code)
	require.NotEmpty(t, resp.Error.Message)
	require.Empty(t, resp.Error.Data, "library errors attach no Data")

	r := s.Serve(context.Background(), NewRequest("missing", nil, NewID(1)))
	require.Equal(t, CodeMethodNotFound, r.Error.Code)
	require.Contains(t, r.Error.Message, "missing")
	require.Empty(t, r.Error.Data, "library errors attach no Data")
}

func TestDuplicateMemberNamesRejected(t *testing.T) {
	s := newTestServer(t)

	t.Run("duplicate on envelope", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","method":"boom","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	})

	t.Run("duplicate inside params", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"a":2,"b":3},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	})

	t.Run("duplicate params via Serve go through the typed decode", func(t *testing.T) {
		// A transport calling Serve directly may not have tokenized params;
		// the typed pipeline still rejects duplicates, as Invalid params.
		resp := s.Serve(context.Background(), &Request{
			JSONRPC: Version,
			Method:  "add",
			Params:  jsontext.Value(`{"a":1,"a":2}`),
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
		resps := decodeResponses(t, out)
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
		resp := decodeResponse(t, out)
		require.NotNil(t, resp.Error)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
	})

	t.Run("unknown member inside params tolerated", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2,"ignored":9},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	})
}

func TestParamsAsRawMessagePassThrough(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("add", jsontext.Value(`{"a":7,"b":8}`), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	var got addResult
	require.NoError(t, resp.Decode(&got))
	require.Equal(t, 15, got.Sum)
}

// decodeError runs one message through ServeMessage and returns the single
// error response's code and message.
func decodeError(t *testing.T, s *Server, msg string) (int, string, jsontext.Value) {
	t.Helper()
	out, err := s.ServeMessage(context.Background(), []byte(msg))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.NotNil(t, resp.Error, "expected an error response for %s", msg)
	return resp.Error.Code, resp.Error.Message, resp.ID
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
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	}
}

func TestDefaultDecoderOmittedParams(t *testing.T) {
	s := NewServer()
	var seen jsontext.Value
	called := false
	s.RegisterRaw("probe", func(_ context.Context, params jsontext.Value) (jsontext.Value, error) {
		seen, called = params, true
		return jsontext.Value(`"ok"`), nil
	})

	// Omitting the member is the only way to send no parameters; "params":null
	// is a present member holding a non-structured value, which §4.2 forbids.
	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"probe","id":1}`))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.Nil(t, resp.Error)
	require.True(t, called)
	require.Nil(t, seen)
}

func TestDecodeErrorEchoesRecoveredID(t *testing.T) {
	s := newTestServer(t)

	t.Run("id read before the failure is echoed", func(t *testing.T) {
		// The id precedes the offending member, so the decoder has it in hand.
		_, _, id := decodeError(t, s, `{"jsonrpc":"2.0","id":7,"method":"add","params":5}`)
		require.JSONEq(t, "7", string(id))
	})

	t.Run("id after the failure is not recoverable", func(t *testing.T) {
		_, _, id := decodeError(t, s, `{"jsonrpc":"2.0","method":"add","params":5,"id":7}`)
		require.JSONEq(t, "null", string(id))
	})

	t.Run("unusable id is not echoed", func(t *testing.T) {
		_, _, id := decodeError(t, s, `{"jsonrpc":"2.0","id":{"bad":1},"method":"add","params":5}`)
		require.JSONEq(t, "null", string(id))
	})

	t.Run("wrong version still echoes the id", func(t *testing.T) {
		// The version verdict belongs to Serve precisely so the id survives.
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"1.0","method":"add","id":9}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
		require.JSONEq(t, "9", string(resp.ID))
	})
}

func TestSetRequestDecoderErrorPassThrough(t *testing.T) {
	custom := NewError(-32050, "bad envelope").MustSetData(map[string]string{"hint": "read the docs"})

	t.Run("bespoke *Error surfaces verbatim", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(*jsontext.Decoder, *Request) error { return custom })
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"add","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		// The server must not have rewritten any of it.
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
		s.SetRequestDecoder(func(*jsontext.Decoder, *Request) error {
			return fmt.Errorf("decoding envelope: %w", custom)
		})

		out, err := s.ServeMessage(context.Background(), []byte(`{}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Equal(t, -32050, resp.Error.Code)
		require.Equal(t, "bad envelope", resp.Error.Message)
	})

	t.Run("plain error is classified as Invalid Request", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(*jsontext.Decoder, *Request) error { return errors.New("nope") })

		code, message, _ := decodeError(t, s, `{}`)
		require.Equal(t, CodeInvalidRequest, code)
		require.Equal(t, "nope", message)
	})

	t.Run("typed-nil *Error does not read as success", func(t *testing.T) {
		s := NewServer()
		s.SetRequestDecoder(func(*jsontext.Decoder, *Request) error {
			var e *Error
			return fmt.Errorf("wrapped: %w", e)
		})

		code, _, _ := decodeError(t, s, `{}`)
		require.Equal(t, CodeInvalidRequest, code)
	})
}

func TestSetRequestDecoderReplacesBehavior(t *testing.T) {
	// A decoder that delegates to the default but relaxes one of its rules:
	// unknown envelope members are dropped instead of rejected.
	lenient := func(d *jsontext.Decoder, req *Request) error {
		// One ReadValue satisfies the decoder's one-value contract; the
		// bytes are then replayed through the default as many times as
		// this decoder needs.
		data, err := d.ReadValue()
		if err != nil {
			return err
		}
		data = data.Clone()
		if err := req.UnmarshalJSONFrom(jsontext.NewDecoder(bytes.NewReader(data))); err != nil {
			var stripped map[string]jsontext.Value
			if json.Unmarshal(data, &stripped) != nil {
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
			return req.UnmarshalJSONFrom(jsontext.NewDecoder(bytes.NewReader(clean)))
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
		resp := decodeResponse(t, out)
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
	})

	t.Run("applies to every batch element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true},
			{"jsonrpc":"2.0","method":"add","params":{"a":3,"b":4},"id":2,"other":1}
		]`))
		require.NoError(t, err)
		resps := decodeResponses(t, out)
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
		s.SetRequestDecoder(func(d *jsontext.Decoder, req *Request) error {
			// Ignoring the message still means consuming its one value.
			if err := d.SkipValue(); err != nil {
				return err
			}
			req.JSONRPC, req.Method, req.ID = "1.0", "add", jsontext.Value("1")
			return nil
		})
		out, err := s.ServeMessage(context.Background(), []byte(`{}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Equal(t, CodeInvalidRequest, resp.Error.Code)
		require.JSONEq(t, "1", string(resp.ID))
	})

	t.Run("server owns the framing", func(t *testing.T) {
		// A decoder that never delegates to the default still cannot let
		// trailing data through: the server checks that nothing follows the
		// one value the decoder consumed.
		s := NewServer()
		s.SetRequestDecoder(func(d *jsontext.Decoder, req *Request) error {
			val, err := d.ReadValue()
			if err != nil {
				return err
			}
			return json.Unmarshal(val, req)
		})
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		msg := `{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`
		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		require.JSONEq(t, `{"sum":3}`, string(decodeResponse(t, out).Result))

		code, _, _ := decodeError(t, s, msg+" !")
		require.Equal(t, CodeParseError, code)
	})
}

// The strict envelope check is Request's own wire form, so it applies to any
// unmarshal, not only the ones a Server drives.
func TestRequestDecodesItself(t *testing.T) {
	t.Run("plain json.Unmarshal is strict", func(t *testing.T) {
		var req Request
		err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"add","surprise":true,"id":1}`), &req)
		require.ErrorContains(t, err, "unknown member: surprise")

		e, ok := errors.AsType[*Error](err)
		require.True(t, ok, "the *Error survives json/v2's wrapping")
		require.Equal(t, CodeInvalidRequest, e.Code)
	})

	t.Run("params must be structured", func(t *testing.T) {
		var req Request
		err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"add","params":null,"id":1}`), &req)
		require.ErrorContains(t, err, "params must be an object or array")
	})

	t.Run("a good request decodes", func(t *testing.T) {
		var req Request
		require.NoError(t, json.Unmarshal(
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1},"id":1}`), &req))
		require.Equal(t, "add", req.Method)
		require.JSONEq(t, `{"a":1}`, string(req.Params))
		require.JSONEq(t, "1", string(req.ID))
	})

	// v1 is implemented over v2 in Go 1.27, so it honors the method too.
	t.Run("under encoding/json v1", func(t *testing.T) {
		var req Request
		err := jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"add","surprise":true,"id":1}`), &req)
		require.ErrorContains(t, err, "unknown member: surprise")
	})
}

// An unmarshaler or marshaler in the options outranks a method, so SetOptions
// alone takes over either wire form.
func TestSetOptionsOverridesTheTypesOwnForm(t *testing.T) {
	t.Run("a *Request unmarshaler replaces the default", func(t *testing.T) {
		s := NewServer()
		// A decoder that tolerates the unknown member the default rejects.
		s.SetOptions(json.WithUnmarshalers(json.UnmarshalFromFunc(
			func(d *jsontext.Decoder, req *Request) error {
				var raw map[string]jsontext.Value
				if err := json.UnmarshalDecode(d, &raw); err != nil {
					return err
				}
				req.JSONRPC, req.Method = Version, "add"
				req.Params, req.ID = raw["params"], raw["id"]
				return nil
			},
		)))
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"surprise":true,"id":1}`))
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(out))
	})

	t.Run("a *Response marshaler replaces the default", func(t *testing.T) {
		s := NewServer()
		s.SetOptions(json.WithMarshalers(json.MarshalToFunc(stampEncoder)))
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`))
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1,"served_by":"test"}`, string(out))
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
			s.SetRequestDecoder(func(*jsontext.Decoder, *Request) error { return nil })
		})
	})
}

// stampEncoder writes the canonical members plus one the spec never defines.
func stampEncoder(enc *jsontext.Encoder, resp *Response) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("jsonrpc")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(Version)); err != nil {
		return err
	}
	if resp.Error != nil {
		if err := enc.WriteToken(jsontext.String("error")); err != nil {
			return err
		}
		if err := json.MarshalEncode(enc, resp.Error); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.String("result")); err != nil {
			return err
		}
		if err := enc.WriteValue(resp.Result); err != nil {
			return err
		}
	}
	if err := enc.WriteToken(jsontext.String("id")); err != nil {
		return err
	}
	if err := enc.WriteValue(resp.ID); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("served_by")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("test")); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

func TestSetResponseEncoderReplacesBehavior(t *testing.T) {
	s := NewServer()
	s.SetResponseEncoder(stampEncoder)
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	t.Run("single response", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`))
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1,"served_by":"test"}`, string(out))
	})

	t.Run("every element of a batch", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"missing","id":2}
		]`))
		require.NoError(t, err)
		require.JSONEq(t, `[
			{"jsonrpc":"2.0","result":{"sum":3},"id":1,"served_by":"test"},
			{"jsonrpc":"2.0","error":{"code":-32601,"message":"method not found: missing"},"id":2,"served_by":"test"}
		]`, string(out))
	})

	t.Run("a message that never reaches Serve", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`{`))
		require.NoError(t, err)
		require.Contains(t, string(out), `"served_by":"test"`)
	})
}

// An encoder that delegates to the default leaves the wire form untouched, so
// wrapping it costs nothing.
func TestSetResponseEncoderDelegates(t *testing.T) {
	var ran int
	s := NewServer()
	s.SetResponseEncoder(func(enc *jsontext.Encoder, resp *Response) error {
		ran++
		return resp.MarshalJSONTo(enc)
	})
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`))
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(out))
	require.Equal(t, 1, ran, "custom ResponseEncoder did not run")

	// A notification produces no response, so the encoder never runs for it.
	out, err = s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2}}`))
	require.NoError(t, err)
	require.Nil(t, out)
	require.Equal(t, 1, ran)
}

// An encoder failure has no response left to report it in, so it surfaces as
// ServeMessage's error return.
func TestSetResponseEncoderErrorSurfaces(t *testing.T) {
	s := NewServer()
	s.SetResponseEncoder(func(*jsontext.Encoder, *Response) error {
		return errors.New("encoder failed")
	})
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`))
	require.ErrorContains(t, err, "encoder failed")
	require.Nil(t, out)
}

func TestSetResponseEncoderPanics(t *testing.T) {
	t.Run("nil encoder", func(t *testing.T) {
		s := NewServer()
		require.Panics(t, func() { s.SetResponseEncoder(nil) })
	})

	t.Run("after a method is registered", func(t *testing.T) {
		s := newTestServer(t)
		require.Panics(t, func() {
			s.SetResponseEncoder(func(*jsontext.Encoder, *Response) error { return nil })
		})
	})
}

// temperature is a params/result member whose wire form the tests control
// through options rather than methods on the type: a string like "21.5C" when
// the temperature unmarshaler/marshaler is installed, a bare number otherwise.
type temperature float64

type tempParams struct {
	T temperature `json:"t"`
}

type tempResult struct {
	T temperature `json:"t"`
}

func tempUnmarshaler(calls *int) *json.Unmarshalers {
	return json.UnmarshalFromFunc(func(d *jsontext.Decoder, v *temperature) error {
		if calls != nil {
			*calls++
		}
		tok, err := d.ReadToken()
		if err != nil {
			return err
		}
		var f float64
		if _, err := fmt.Sscanf(tok.String(), "%gC", &f); err != nil {
			return fmt.Errorf("temperature: want a string like 21.5C, got %s", tok)
		}
		*v = temperature(f)
		return nil
	})
}

var tempMarshaler = json.MarshalToFunc(func(e *jsontext.Encoder, v temperature) error {
	return e.WriteToken(jsontext.String(fmt.Sprintf("%gC", float64(v))))
})

func echoTemp(_ context.Context, p tempParams) (tempResult, error) {
	return tempResult{T: p.T}, nil
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
		require.Nil(t, resp.Error)
		require.Equal(t, 1, calls)
		// No marshaler was installed, so the result is the default number.
		require.JSONEq(t, `{"t":21.5}`, string(resp.Result))
	})

	t.Run("omitted params never reach the unmarshaler", func(t *testing.T) {
		var calls int
		s := NewServer()
		s.SetOptions(json.WithUnmarshalers(tempUnmarshaler(&calls)))
		s.Register("echo", echoTemp)

		out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"echo","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Nil(t, resp.Error)
		require.Equal(t, 0, calls)
		require.JSONEq(t, `{"t":0}`, string(resp.Result))
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
		require.Nil(t, resp.Error)
		require.JSONEq(t, `{"sum":3}`, string(resp.Result))
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
		require.Nil(t, resps[0].Error)
		require.NotNil(t, resps[1].Error)
		require.Equal(t, CodeInvalidRequest, resps[1].Error.Code)
	})
}

func TestSetOptionsPreservesRequestDecoder(t *testing.T) {
	const msg = `{"jsonrpc":"2.0","method":"echo","params":{"t":"21.5C"},"id":1}`

	// tracingDecoder records that it ran, then delegates to the default.
	tracingDecoder := func(ran *bool) RequestDecoder {
		return func(d *jsontext.Decoder, req *Request) error {
			*ran = true
			return req.UnmarshalJSONFrom(d)
		}
	}

	check := func(t *testing.T, s *Server, ran *bool) {
		t.Helper()
		s.Register("echo", echoTemp)
		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Nil(t, resp.Error)
		require.True(t, *ran, "custom RequestDecoder did not run")
		require.JSONEq(t, `{"t":21.5}`, string(resp.Result), "params unmarshaler did not run")
	}

	t.Run("SetRequestDecoder then SetOptions", func(t *testing.T) {
		var ran bool
		s := NewServer()
		s.SetRequestDecoder(tracingDecoder(&ran))
		s.SetOptions(json.WithUnmarshalers(tempUnmarshaler(nil)))
		check(t, s, &ran)
	})

	t.Run("SetOptions then SetRequestDecoder", func(t *testing.T) {
		var ran bool
		s := NewServer()
		s.SetOptions(json.WithUnmarshalers(tempUnmarshaler(nil)))
		s.SetRequestDecoder(tracingDecoder(&ran))
		check(t, s, &ran)
	})

	t.Run("the RequestDecoder outranks a *Request unmarshaler in the options", func(t *testing.T) {
		var ran bool
		s := NewServer()
		s.SetOptions(json.WithUnmarshalers(json.JoinUnmarshalers(
			json.UnmarshalFromFunc(func(d *jsontext.Decoder, _ *Request) error {
				d.SkipValue()
				return errors.New("options unmarshaler must not run")
			}),
			tempUnmarshaler(nil),
		)))
		s.SetRequestDecoder(tracingDecoder(&ran))
		check(t, s, &ran)
	})
}

func TestSetOptionsPreservesResponseEncoder(t *testing.T) {
	const msg = `{"jsonrpc":"2.0","method":"echo","params":{"t":21.5},"id":1}`

	t.Run("the result marshaler and the encoder both apply", func(t *testing.T) {
		s := NewServer()
		s.SetOptions(json.WithMarshalers(tempMarshaler))
		s.SetResponseEncoder(stampEncoder)
		s.Register("echo", echoTemp)

		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","result":{"t":"21.5C"},"id":1,"served_by":"test"}`, string(out))
	})

	t.Run("the ResponseEncoder outranks a *Response marshaler in the options", func(t *testing.T) {
		s := NewServer()
		s.SetOptions(json.WithMarshalers(json.JoinMarshalers(
			json.MarshalToFunc(func(enc *jsontext.Encoder, _ *Response) error {
				return enc.WriteToken(jsontext.String("options marshaler must not run"))
			}),
			tempMarshaler,
		)))
		s.SetResponseEncoder(stampEncoder)
		s.Register("echo", echoTemp)

		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","result":{"t":"21.5C"},"id":1,"served_by":"test"}`, string(out))
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
	s.SetErrorHandler(func(ctx context.Context, req *Request, err error) *Error {
		if e, ok := errors.AsType[*Error](err); ok && e != nil {
			return e
		}
		logged = err
		return NewError(CodeServerError, "internal error")
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("dial postgres://user:hunter2@db: refused")
	})

	resp := s.Serve(context.Background(), NewRequest("boom", nil, NewID(1)))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeServerError, resp.Error.Code)
	// The detail stayed with the operator instead of going to the client.
	require.Equal(t, "internal error", resp.Error.Message)
	require.EqualError(t, logged, "dial postgres://user:hunter2@db: refused")
}

func TestErrorHandlerSeesClassifiedErrors(t *testing.T) {
	// An *Error a component raised still reaches the handler, so a verbose
	// json/v2 message can be replaced.
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, _ *Request, err error) *Error {
		e, ok := errors.AsType[*Error](err)
		require.True(t, ok, "expected the component's *Error, got %T", err)
		return NewError(e.Code, "invalid params")
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
	var gotID jsontext.Value
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, req *Request, err error) *Error {
		gotMethod, gotID = req.Method, req.ID
		return DefaultErrorHandler(context.Background(), req, err)
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("boom")
	})

	s.Serve(context.Background(), NewRequest("boom", nil, NewID(7)))
	require.Equal(t, "boom", gotMethod)
	require.JSONEq(t, "7", string(gotID))
}

func TestErrorHandlerObservesNotificationFailure(t *testing.T) {
	var seen error
	var wasNotification bool
	s := NewServer()
	s.SetErrorHandler(func(_ context.Context, req *Request, err error) *Error {
		seen, wasNotification = err, req.IsNotification()
		return NewError(CodeInternalError, "unused")
	})
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("boom")
	})

	// The spec allows no reply, but the failure is no longer silent.
	require.Nil(t, s.Serve(context.Background(), NewNotification("boom", nil)))
	require.EqualError(t, seen, "boom")
	require.True(t, wasNotification)
}

func TestErrorHandlerCoversDecodePath(t *testing.T) {
	t.Run("envelope decode", func(t *testing.T) {
		var got *Request
		s := NewServer()
		s.SetErrorHandler(func(_ context.Context, req *Request, err error) *Error {
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
		require.NotNil(t, got)
		require.Equal(t, "add", got.Method)
		require.JSONEq(t, "1", string(got.ID))
	})

	t.Run("batch-level failure passes a nil request", func(t *testing.T) {
		var called bool
		s := NewServer()
		s.SetErrorHandler(func(_ context.Context, req *Request, err error) *Error {
			called = true
			require.Nil(t, req, "no request could be decoded")
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
	s.SetErrorHandler(func(ctx context.Context, req *Request, err error) *Error {
		if errors.Is(err, errDBUnavailable) {
			return NewError(CodeServerError, "service unavailable").
				MustSetData(map[string]string{"detail": err.Error()})
		}
		return DefaultErrorHandler(ctx, req, err)
	})
	s.Use(func(next RawHandler) RawHandler {
		return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, error) {
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

	resp := s.Serve(context.Background(), NewRequest("lookup", nil, NewID(1)))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeServerError, resp.Error.Code)
	require.Equal(t, "service unavailable", resp.Error.Message)
	var data struct {
		Detail string `json:"detail"`
	}
	require.NoError(t, resp.Error.UnmarshalData(&data))
	require.Equal(t, "lookup: query users: db unavailable", data.Detail)
}

func TestErrorHandlerNilResultIsInternalError(t *testing.T) {
	s := NewServer()
	s.SetErrorHandler(func(context.Context, *Request, error) *Error { return nil })
	s.Register("boom", func(context.Context, struct{}) (any, error) {
		return nil, errors.New("boom")
	})

	// A nil from the handler must not produce a response with no error object.
	resp := s.Serve(context.Background(), NewRequest("boom", nil, NewID(1)))
	require.NotNil(t, resp.Error)
	require.Equal(t, CodeInternalError, resp.Error.Code)
}

func TestDefaultErrorHandlerTypedNil(t *testing.T) {
	e := DefaultErrorHandler(context.Background(), nil, (*Error)(nil))
	require.NotNil(t, e)
	require.Equal(t, CodeInternalError, e.Code)
}

func TestDecoderTypedNilErrorDoesNotPanic(t *testing.T) {
	// A decoder returning a typed-nil *Error once reached err.Error() on a nil
	// receiver.
	s := NewServer()
	s.SetRequestDecoder(func(d *jsontext.Decoder, _ *Request) error {
		d.SkipValue()
		return (*Error)(nil)
	})
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	code, _, _ := decodeError(t, s, `{"jsonrpc":"2.0","method":"add","id":1}`)
	require.Equal(t, CodeInvalidRequest, code)
}

func TestSetErrorHandlerPanics(t *testing.T) {
	t.Run("nil handler", func(t *testing.T) {
		s := NewServer()
		require.Panics(t, func() { s.SetErrorHandler(nil) })
	})

	t.Run("after a method is registered", func(t *testing.T) {
		s := newTestServer(t)
		require.Panics(t, func() {
			s.SetErrorHandler(func(context.Context, *Request, error) *Error { return nil })
		})
	})
}
