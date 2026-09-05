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

// decodeResponse parses one marshaled response off the wire. Response is an
// interface, so tests go through DecodeResponse rather than unmarshaling into
// a struct.
func decodeResponse(t *testing.T, data jsontext.Value) Response {
	t.Helper()
	resp, err := DecodeResponse(data)
	require.NoError(t, err)
	return resp
}

// decodeResponses parses a marshaled batch reply.
func decodeResponses(t *testing.T, data jsontext.Value) []Response {
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
	require.Nil(t, resp.Error())

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
	require.NotNil(t, resp.Error())
	require.Equal(t, -32001, resp.Error().Code)
	require.Equal(t, "custom", resp.Error().Message)
	require.JSONEq(t, `{"x":1}`, string(resp.Error().Data))
}

func TestClientSendInternalError(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("boom", mustParams(t, struct{}{}), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInternalError, resp.Error().Code)
}

func TestMethodNotFound(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("missing", nil, NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeMethodNotFound, resp.Error().Code)
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
	s.RegisterHandler("void", func(_ context.Context, _ jsontext.Value) (jsontext.Value, *Error) {
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
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInternalError, resp.Error().Code)
}

func TestInvalidIDNotEchoedOnVersionError(t *testing.T) {
	s := newTestServer(t)
	resp := s.Serve(context.Background(), &Request{
		JSONRPC: "1.0",
		Method:  "add",
		ID:      jsontext.Value(`{"x":1}`),
	})
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInvalidRequest, resp.Error().Code)
	require.JSONEq(t, "null", string(resp.ID()))
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
	c := NewClient(SenderFunc(func(ctx context.Context, req *Request) (Response, error) {
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
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInvalidRequest, resp.Error().Code)
	require.JSONEq(t, "1", string(resp.ID()))
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

// MarshalJSONTo writes the raw members with WriteValue, which rejects an
// empty or malformed value rather than emitting invalid JSON. Only a response
// built by hand inside this package can get into that state.
func TestResponseMarshalRejectsBadRawValue(t *testing.T) {
	_, err := json.Marshal(&SuccessResponse{result: nil, id: NewID(1)})
	require.Error(t, err, "an empty result is not JSON null")

	_, err = json.Marshal(&SuccessResponse{result: jsontext.Value(`{oops`), id: NewID(1)})
	require.Error(t, err, "a malformed result must not reach the wire")

	_, err = json.Marshal(&ErrorResponse{err: NewError(CodeInternalError, "x"), id: nil})
	require.Error(t, err, "an empty id is not JSON null")
}

func TestServeReturnsConcreteResponseTypes(t *testing.T) {
	s := newTestServer(t)

	ok := s.Serve(context.Background(), NewRequest("add", mustParams(t, addParams{A: 2, B: 3}), NewID(1)))
	require.IsType(t, &SuccessResponse{}, ok)
	require.True(t, ok.IsSuccess())
	require.False(t, ok.IsError())
	require.Nil(t, ok.Error())
	require.JSONEq(t, `{"sum":5}`, string(ok.Result()))

	bad := s.Serve(context.Background(), NewRequest("missing", nil, NewID(1)))
	require.IsType(t, &ErrorResponse{}, bad)
	require.True(t, bad.IsError())
	require.False(t, bad.IsSuccess())
	require.Nil(t, bad.Result(), "an error response carries no result")
	require.Error(t, bad.Decode(new(addResult)), "decoding an error response reports an error")
}

func TestDecodeResponse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want any // a *SuccessResponse or *ErrorResponse, or nil to expect failure
	}{
		{"success", `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, &SuccessResponse{}},
		{"null result is a success", `{"jsonrpc":"2.0","result":null,"id":1}`, &SuccessResponse{}},
		{"error", `{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":1}`, &ErrorResponse{}},
		{"null id", `{"jsonrpc":"2.0","error":{"code":-32700,"message":"nope"},"id":null}`, &ErrorResponse{}},
		{"unknown members are tolerated", `{"jsonrpc":"2.0","result":1,"id":1,"extra":true}`, &SuccessResponse{}},
		{"both result and error", `{"jsonrpc":"2.0","result":1,"error":{"code":-1,"message":"x"},"id":1}`, nil},
		{"neither result nor error", `{"jsonrpc":"2.0","id":1}`, nil},
		{"wrong version", `{"jsonrpc":"1.0","result":1,"id":1}`, nil},
		{"missing id", `{"jsonrpc":"2.0","result":1}`, nil},
		{"duplicate member", `{"jsonrpc":"2.0","result":1,"result":2,"id":1}`, nil},
		{"not an object", `[1]`, nil},
		{"malformed", `{"jsonrpc":`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeResponse([]byte(tc.in))
			if tc.want == nil {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tc.want, got)
			require.NotEmpty(t, got.ID())
		})
	}
}

func TestDecodeResponseRoundTrip(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":"abc"}`))
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.True(t, resp.IsSuccess())
	require.JSONEq(t, `"abc"`, string(resp.ID()))

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

// A Sender is free to build a Response itself, so Call must not report a
// failure it cannot name: an *ErrorResponse holding a nil *Error would
// otherwise return a non-nil error interface wrapping a nil pointer.
func TestClientCallErrorResponseWithoutErrorObject(t *testing.T) {
	c := NewClient(SenderFunc(func(_ context.Context, req *Request) (Response, error) {
		return NewErrorResponse(nil, req.ID), nil
	}))

	err := c.Call(context.Background(), "add", addParams{A: 1, B: 2}, nil)
	require.ErrorContains(t, err, "no error object")
	_, isRPCErr := errors.AsType[*Error](err)
	require.False(t, isRPCErr)
}

func TestMessageServerSingleRequest(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.Nil(t, resp.Error())
	require.JSONEq(t, `{"sum":3}`, string(resp.Result()))
	require.JSONEq(t, "1", string(resp.ID()))
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
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeParseError, resp.Error().Code)
	require.JSONEq(t, "null", string(resp.ID()))
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
	require.Nil(t, resps[0].Error())
	require.JSONEq(t, "1", string(resps[0].ID()))
	require.JSONEq(t, `{"sum":3}`, string(resps[0].Result()))
	require.Nil(t, resps[1].Error())
	require.JSONEq(t, "2", string(resps[1].ID()))
	require.JSONEq(t, `{"sum":30}`, string(resps[1].Result()))
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
	require.JSONEq(t, "7", string(resps[0].ID()))
	require.JSONEq(t, `{"sum":5}`, string(resps[0].Result()))
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
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInvalidRequest, resp.Error().Code)
	require.JSONEq(t, "null", string(resp.ID()))
}

func TestBatchInvalidElements(t *testing.T) {
	s := newTestServer(t)

	t.Run("single invalid element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1]`))
		require.NoError(t, err)

		resps := decodeResponses(t, out)
		require.Len(t, resps, 1)
		require.NotNil(t, resps[0].Error())
		require.Equal(t, CodeInvalidRequest, resps[0].Error().Code)
		require.JSONEq(t, "null", string(resps[0].ID()))
	})

	t.Run("three invalid elements", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[1,2,3]`))
		require.NoError(t, err)

		resps := decodeResponses(t, out)
		require.Len(t, resps, 3)
		for _, r := range resps {
			require.NotNil(t, r.Error())
			require.Equal(t, CodeInvalidRequest, r.Error().Code)
		}
	})
}

func TestBatchMalformedJSONIsSingleParseError(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`[{"jsonrpc":"2.0","method":"add","id":1},{"jsonrpc":`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeParseError, resp.Error().Code)
	require.JSONEq(t, "null", string(resp.ID()))
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

	require.Nil(t, resps[0].Error())
	require.JSONEq(t, `"ok"`, string(resps[0].ID()))
	require.JSONEq(t, `{"sum":3}`, string(resps[0].Result()))

	require.NotNil(t, resps[1].Error())
	require.Equal(t, CodeInvalidRequest, resps[1].Error().Code)
	require.JSONEq(t, "null", string(resps[1].ID()))

	require.NotNil(t, resps[2].Error())
	require.Equal(t, CodeInvalidRequest, resps[2].Error().Code)
	require.JSONEq(t, "9", string(resps[2].ID()))
}

func TestMessageServerInvalidShape(t *testing.T) {
	s := newTestServer(t)
	data := []byte(`12345`)
	out, err := s.ServeMessage(context.Background(), data)
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInvalidRequest, resp.Error().Code)
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
			require.NotNil(t, resp.Error())
			require.Equal(t, CodeInvalidRequest, resp.Error().Code)
			require.Contains(t, resp.Error().Message, "id")
			require.JSONEq(t, "null", string(resp.ID()))
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
			require.Nil(t, resp.Error())
			require.JSONEq(t, tc.id, string(resp.ID()))
		})
	}
}

func TestSendPropagatesCallerID(t *testing.T) {
	s := newTestServer(t)
	var seen jsontext.Value
	c := NewClient(SenderFunc(func(ctx context.Context, req *Request) (Response, error) {
		seen = append(seen[:0], req.ID...)
		return s.Serve(ctx, req), nil
	}))

	req := NewRequest("add", mustParams(t, addParams{A: 1, B: 2}), NewID("req-abc"))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error())
	require.JSONEq(t, `"req-abc"`, string(seen))
	require.JSONEq(t, `"req-abc"`, string(resp.ID()))
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

func TestTypedWithValidationMiddleware(t *testing.T) {
	s := NewServer()
	// Pre-decode validation middleware owns the full *Error including
	// structured Data, then delegates to the typed handler.
	requirePositive := func(next Handler) Handler {
		return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, *Error) {
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
	require.Nil(t, resp.Error())
	var ok addResult
	require.NoError(t, resp.Decode(&ok))
	require.Equal(t, 5, ok.Sum)

	resp, err = c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: -1, B: 3}), NewID(2)))
	require.NoError(t, err)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInvalidParams, resp.Error().Code)

	var detail map[string]int
	require.NoError(t, resp.Error().UnmarshalData(&detail))
	require.Equal(t, -1, detail["a"])
	require.Equal(t, 3, detail["b"])
}

// tagMiddleware appends its name to *log when the request passes through,
// letting tests assert ordering of the wrapping.
func tagMiddleware(name string, log *[]string) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, *Error) {
			*log = append(*log, name)
			return next(ctx, raw)
		}
	}
}

func TestRegisterMiddlewareValidatesBeforeDecode(t *testing.T) {
	s := NewServer()
	// A raw middleware that rejects without ever decoding into the typed P.
	requirePositive := func(next Handler) Handler {
		return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, *Error) {
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
	require.Nil(t, resp.Error())
	var ok addResult
	require.NoError(t, resp.Decode(&ok))
	require.Equal(t, 5, ok.Sum)

	resp, err = c.Send(context.Background(), NewRequest("add", mustParams(t, addParams{A: -1, B: 3}), NewID(2)))
	require.NoError(t, err)
	require.NotNil(t, resp.Error())
	require.Equal(t, CodeInvalidParams, resp.Error().Code)
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
	require.Nil(t, resp.Error())
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

func TestProtocolErrorsCarryTheCauseAsMessage(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(), []byte(`{not valid json`))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.Equal(t, CodeParseError, resp.Error().Code)
	require.NotEmpty(t, resp.Error().Message)
	require.Empty(t, resp.Error().Data, "library errors attach no Data")

	r := s.Serve(context.Background(), NewRequest("missing", nil, NewID(1)))
	require.Equal(t, CodeMethodNotFound, r.Error().Code)
	require.Contains(t, r.Error().Message, "missing")
	require.Empty(t, r.Error().Data, "library errors attach no Data")
}

func TestDuplicateMemberNamesRejected(t *testing.T) {
	s := newTestServer(t)

	t.Run("duplicate on envelope", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","method":"boom","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.NotNil(t, resp.Error())
		require.Equal(t, CodeInvalidRequest, resp.Error().Code)
	})

	t.Run("duplicate inside params", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"a":2,"b":3},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.NotNil(t, resp.Error())
		require.Equal(t, CodeInvalidRequest, resp.Error().Code)
	})

	t.Run("duplicate params via Serve go through DecodeParams", func(t *testing.T) {
		// A transport calling Serve directly may not have tokenized params;
		// the typed pipeline still rejects duplicates, as Invalid params.
		resp := s.Serve(context.Background(), &Request{
			JSONRPC: Version,
			Method:  "add",
			Params:  jsontext.Value(`{"a":1,"a":2}`),
			ID:      NewID(1),
		})
		require.NotNil(t, resp.Error())
		require.Equal(t, CodeInvalidParams, resp.Error().Code)
	})

	t.Run("duplicate in one batch element fails only that element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"add","method":"boom","id":2}
		]`))
		require.NoError(t, err)
		resps := decodeResponses(t, out)
		require.Len(t, resps, 2)
		require.Nil(t, resps[0].Error())
		require.JSONEq(t, `{"sum":3}`, string(resps[0].Result()))
		require.NotNil(t, resps[1].Error())
		require.Equal(t, CodeInvalidRequest, resps[1].Error().Code)
		// The offending element's id cannot be trusted, so the entry has id null.
		require.JSONEq(t, "null", string(resps[1].ID()))
	})
}

func TestEnvelopeStrictness(t *testing.T) {
	s := newTestServer(t)

	t.Run("unknown envelope member rejected", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.NotNil(t, resp.Error())
		require.Equal(t, CodeInvalidRequest, resp.Error().Code)
	})

	t.Run("unknown member inside params tolerated", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(),
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2,"ignored":9},"id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Nil(t, resp.Error())
		require.JSONEq(t, `{"sum":3}`, string(resp.Result()))
	})
}

func TestParamsAsRawMessagePassThrough(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("add", jsontext.Value(`{"a":7,"b":8}`), NewID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, resp.Error())
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
	require.NotNil(t, resp.Error(), "expected an error response for %s", msg)
	return resp.Error().Code, resp.Error().Message, resp.ID()
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
		require.Nil(t, resp.Error())
		require.JSONEq(t, `{"sum":3}`, string(resp.Result()))
	}
}

func TestDefaultDecoderOmittedParams(t *testing.T) {
	s := NewServer()
	var seen jsontext.Value
	called := false
	s.RegisterHandler("probe", func(_ context.Context, params jsontext.Value) (jsontext.Value, *Error) {
		seen, called = params, true
		return jsontext.Value(`"ok"`), nil
	})

	// Omitting the member is the only way to send no parameters; "params":null
	// is a present member holding a non-structured value, which §4.2 forbids.
	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"probe","id":1}`))
	require.NoError(t, err)
	resp := decodeResponse(t, out)
	require.Nil(t, resp.Error())
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
		require.Equal(t, CodeInvalidRequest, resp.Error().Code)
		require.JSONEq(t, "9", string(resp.ID()))
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
		require.Equal(t, -32050, resp.Error().Code)
		require.Equal(t, "bad envelope", resp.Error().Message)
		var hint struct {
			Hint string `json:"hint"`
		}
		require.NoError(t, resp.Error().UnmarshalData(&hint))
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
		require.Equal(t, -32050, resp.Error().Code)
		require.Equal(t, "bad envelope", resp.Error().Message)
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
	// A decoder that delegates to DecodeRequest but relaxes one of its rules:
	// unknown envelope members are dropped instead of rejected.
	lenient := func(d *jsontext.Decoder, req *Request) error {
		// One ReadValue satisfies the decoder's one-value contract; the
		// bytes are then replayed through DecodeRequest as many times as
		// this decoder needs.
		data, err := d.ReadValue()
		if err != nil {
			return err
		}
		data = data.Clone()
		if err := DecodeRequest(jsontext.NewDecoder(bytes.NewReader(data)), req); err != nil {
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
			return DecodeRequest(jsontext.NewDecoder(bytes.NewReader(clean)), req)
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
		require.Nil(t, resp.Error())
		require.JSONEq(t, `{"sum":3}`, string(resp.Result()))
	})

	t.Run("applies to every batch element", func(t *testing.T) {
		out, err := s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1,"extra":true},
			{"jsonrpc":"2.0","method":"add","params":{"a":3,"b":4},"id":2,"other":1}
		]`))
		require.NoError(t, err)
		resps := decodeResponses(t, out)
		require.Len(t, resps, 2)
		require.Nil(t, resps[0].Error())
		require.Nil(t, resps[1].Error())
		require.JSONEq(t, `{"sum":3}`, string(resps[0].Result()))
		require.JSONEq(t, `{"sum":7}`, string(resps[1].Result()))
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
		require.Equal(t, CodeInvalidRequest, resp.Error().Code)
		require.JSONEq(t, "1", string(resp.ID()))
	})

	t.Run("server owns the framing", func(t *testing.T) {
		// A decoder that never delegates to DecodeRequest still cannot let
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
		require.JSONEq(t, `{"sum":3}`, string(decodeResponse(t, out).Result()))

		code, _, _ := decodeError(t, s, msg+" !")
		require.Equal(t, CodeParseError, code)
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
