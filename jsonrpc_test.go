package jsonrpc

import (
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
		return nil, withData(NewError(-32001, "custom"), map[string]int{"x": 1})
	})
	s.Register("boom", func(_ context.Context, _ struct{}) (any, error) {
		return nil, errors.New("internal boom")
	})
	return s
}

func mustParams(t *testing.T, v any) Params {
	t.Helper()
	p, err := NewParams(v)
	require.NoError(t, err)
	return p
}

// decodeResponse parses one marshaled response off the wire.
func decodeResponse(t *testing.T, data jsontext.Value) Response {
	t.Helper()
	var resp Response
	require.NoError(t, json.Unmarshal(data, &resp))
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

func TestErrorIsMatchesCode(t *testing.T) {
	errNotFound := NewError(-32004, "not found")

	require.ErrorIs(t, withData(NewError(-32004, "user 7 not found"), []int{7}), errNotFound)
	require.ErrorIs(t, fmt.Errorf("lookup: %w", errNotFound), errNotFound)
	require.NotErrorIs(t, NewError(-32005, "not found"), errNotFound)
	require.NotErrorIs(t, errors.New("not found"), errNotFound)

	// A server error still matches after crossing the wire.
	s := NewServer()
	s.Register("find", func(context.Context, struct{}) (any, error) {
		return nil, errNotFound
	})
	err := NewClient(s.Sender()).Call(context.Background(), "find", struct{}{}, nil)
	require.ErrorIs(t, err, errNotFound)
}

func TestErrorWithData(t *testing.T) {
	base := NewError(-32001, "custom")
	e, err := base.WithData(map[string]int{"x": 1})
	require.NoError(t, err)
	require.JSONEq(t, `{"x":1}`, string(e.Data()))
	require.Equal(t, base.Code(), e.Code())
	require.Equal(t, base.Message(), e.Message())

	// The receiver is never modified, so shared errors are safe.
	require.Empty(t, base.Data())

	_, err = e.WithData(make(chan int))
	require.Error(t, err)
	require.JSONEq(t, `{"x":1}`, string(e.Data()))
}

// withData is WithData for test data known to be marshalable.
func withData(e Error, data any) Error {
	c, err := e.WithData(data)
	if err != nil {
		panic(err)
	}
	return c
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

func TestRequestRoundTripPreservesStringID(t *testing.T) {
	in := NewRequest("x", Params{}, StringID("abc"))
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out Request
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, `"abc"`, out.ID().String())
	require.False(t, out.IsNotification())
}

func TestRequestNotificationHasNoIDField(t *testing.T) {
	req := NewNotification("x", Params{})
	b, err := json.Marshal(req)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"id"`)
	require.True(t, req.IsNotification())
}

func TestResponseAlwaysHasID(t *testing.T) {
	// Both shapes carry an id member, even when it is null, and a success
	// response always carries a result member, even when it is null. The
	// constructors normalize an empty value, so this holds by construction.
	b, err := json.Marshal(NewErrorResponse(NewError(CodeParseError, "bad"), ID{}))
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)

	b, err = json.Marshal(NewSuccessResponse(nil, ID{}))
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)
	require.Contains(t, string(b), `"result":null`)
}

func TestResponseMarshalsExactShape(t *testing.T) {
	ok := NewSuccessResponse(jsontext.Value(`{"sum":3}`), IntID(1))
	b, err := json.Marshal(ok)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(b))

	bad := NewErrorResponse(NewError(CodeMethodNotFound, "nope"), StringID("x"))
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
	_, err := json.Marshal(&Response{})
	require.ErrorContains(t, err, "neither result nor error", "the zero Response is no response")

	// A result marks a success response, so a stray error is never sent with it.
	b, err := json.Marshal(&Response{
		result: jsontext.Value("1"),
		error:  NewError(CodeInternalError, "x"),
		id:     IntID(1),
	})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":1}`, string(b))

	_, err = json.Marshal(&Response{result: jsontext.Value(`{oops`), id: IntID(1)})
	require.Error(t, err, "a malformed result must not reach the wire")

	// An empty id is not malformed: the spec's own fallback is null.
	b, err = json.Marshal(&Response{result: jsontext.Value("1")})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":null}`, string(b))

	// Nor is a version the caller never set: the protocol's is written, not the
	// field's.
	b, err = json.Marshal(&Response{result: jsontext.Value("1"), id: IntID(1)})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":1}`, string(b))
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

func TestResponseUnmarshalJSONFrom(t *testing.T) {
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
		{"a structured unknown member is skipped whole", `{"jsonrpc":"2.0","meta":{"a":[1,{"b":2}]},"result":1,"id":1}`, success},
		{"members in any order", `{"id":1,"result":1,"jsonrpc":"2.0"}`, success},
		{"non-string jsonrpc", `{"jsonrpc":2.0,"result":1,"id":1}`, fails},
		{"error is not an object", `{"jsonrpc":"2.0","error":"nope","id":1}`, fails},
		{"duplicate member inside a nested value", `{"jsonrpc":"2.0","result":{"a":1,"a":2},"id":1}`, fails},
		{"truncated after a member", `{"jsonrpc":"2.0","result":1,"id":1`, fails},
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
			var got Response
			err := json.Unmarshal([]byte(tc.in), &got)
			if tc.want == fails {
				require.Error(t, err)
				require.Zero(t, got, "a rejected response leaves the receiver untouched")
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, got.id)
			require.Equal(t, Version, got.jsonrpc)
			if tc.want == success {
				require.False(t, got.isError())
				require.NotEmpty(t, got.result)
			} else {
				require.True(t, got.isError())
				require.Nil(t, got.result)
			}
		})
	}
}

// json/v2 does not zero the destination, so a Response decoded into twice must
// not keep the first message's members.
func TestResponseUnmarshalJSONFromReplacesPriorMembers(t *testing.T) {
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":{"sum":3},"id":1}`), &resp))
	require.NotNil(t, resp.result)

	require.NoError(t, json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":2}`), &resp))
	require.Nil(t, resp.result, "the first message's result does not survive")
	require.True(t, resp.isError())
	require.JSONEq(t, "2", resp.id.String())

	// A rejected message clears it too, so the previous response cannot be
	// mistaken for this one.
	require.Error(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":3}`), &resp))
	require.Zero(t, resp)
}

// jsonrpchttp's Sender decodes with encoding/json v1, implemented over v2 in
// Go 1.27, so the checks apply there too.
func TestResponseUnmarshalJSONFromUnderJSONV1(t *testing.T) {
	var resp Response
	err := jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1}`), &resp)
	require.ErrorContains(t, err, "neither result nor error")

	require.NoError(t, jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","result":1,"id":1}`), &resp))
	require.JSONEq(t, "1", string(resp.result))
}

func TestResponseUnmarshalJSONFromRoundTrip(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":"abc"}`))
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.False(t, resp.isError())
	require.JSONEq(t, `"abc"`, resp.id.String())

	var got addResult
	require.NoError(t, resp.DecodeResult(&got))
	require.Equal(t, addResult{Sum: 3}, got)
}

func TestDecodeResponsesRejectsBadElement(t *testing.T) {
	_, err := DecodeResponses([]byte(`[{"jsonrpc":"2.0","result":1,"id":1},{"jsonrpc":"2.0","id":2}]`))
	require.ErrorContains(t, err, "element 1")

	// A single response is not a batch.
	_, err = DecodeResponses([]byte(`{"jsonrpc":"2.0","result":1,"id":1}`))
	require.Error(t, err)
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

func TestNewParamsPassthrough(t *testing.T) {
	raw := jsontext.Value(`{"a":1}`)
	out, err := NewParams(raw)
	require.NoError(t, err)
	require.Equal(t, string(raw), out.String())

	again, err := NewParams(out)
	require.NoError(t, err)
	require.Equal(t, out, again, "a Params passes through")

	out, err = NewParams(nil)
	require.NoError(t, err)
	require.True(t, out.IsZero())
}

// TestNewParamsRequiresStructured pins §4.2 at the point of construction, so a
// request the server would reject is not built in the first place.
func TestNewParamsRequiresStructured(t *testing.T) {
	t.Run("accepts both structured shapes", func(t *testing.T) {
		for _, v := range []any{
			addParams{A: 1},
			[]int{1, 2},
			jsontext.Value(`{"a":1}`),
			jsontext.Value(` [1] `),
		} {
			out, err := NewParams(v)
			require.NoError(t, err, "%v", v)
			require.NotEmpty(t, out)
		}
	})

	t.Run("rejects scalars, nulls, and empties", func(t *testing.T) {
		for _, v := range []any{
			5,
			"world",
			true,
			(*addParams)(nil), // marshals to null
			jsontext.Value(`5`),
			jsontext.Value(`null`),
		} {
			out, err := NewParams(v)
			require.Error(t, err, "%v", v)
			require.True(t, out.IsZero())
			require.Contains(t, err.Error(), "params must be a JSON object or array")
		}
	})

	t.Run("raw values are validated, not passed through", func(t *testing.T) {
		for _, v := range []jsontext.Value{
			jsontext.Value(``), // empty but non-nil, so not the "no params" case
			jsontext.Value(`{oops`),
		} {
			out, err := NewParams(v)
			require.ErrorContains(t, err, "marshal params", "%q", v)
			require.True(t, out.IsZero())
		}
	})

	t.Run("the check sees the options marshaler's output", func(t *testing.T) {
		// tempMarshaler turns a temperature into a string, so P itself becomes
		// a scalar even though the Go value is not one.
		_, err := NewParams(temperature(21.5), json.WithMarshalers(tempMarshaler))
		require.ErrorContains(t, err, "got string")
	})

	t.Run("Call and Notify report it before sending", func(t *testing.T) {
		var sent int
		c := NewClient(SenderFunc(func(context.Context, Request) (Response, error) {
			sent++
			return Response{}, nil
		}))
		require.ErrorContains(t, c.Call(context.Background(), "m", 5, nil), "params must be a JSON object or array")
		require.ErrorContains(t, c.Notify(context.Background(), "m", 5), "params must be a JSON object or array")
		require.Zero(t, sent)
	})
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

func TestParamsAsRawMessagePassThrough(t *testing.T) {
	s := newTestServer(t)
	c := NewClient(s.Sender())

	req := NewRequest("add", mustParams(t, jsontext.Value(`{"a":7,"b":8}`)), IntID(1))
	resp, err := c.Send(context.Background(), req)
	require.NoError(t, err)
	require.False(t, resp.isError())
	var got addResult
	require.NoError(t, resp.DecodeResult(&got))
	require.Equal(t, 15, got.Sum)
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

// requestUnmarshaler installs fn as the *Request unmarshaler, which is how a
// server takes over the envelope decode.
func requestUnmarshaler(fn func(*jsontext.Decoder, *Request) error) json.Options {
	return json.WithUnmarshalers(json.UnmarshalFromFunc(fn))
}

func TestRequestUnmarshalerErrorPassThrough(t *testing.T) {
	custom := withData(NewError(-32050, "bad envelope"), map[string]string{"hint": "read the docs"})

	t.Run("bespoke Error surfaces verbatim", func(t *testing.T) {
		s := NewServer()
		s.SetOptions(requestUnmarshaler(func(*jsontext.Decoder, *Request) error { return custom }))
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		out, err := s.ServeMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"add","id":1}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		// The server must not have rewritten any of it.
		require.Equal(t, -32050, resp.error.Code())
		require.Equal(t, "bad envelope", resp.error.Message())
		var hint struct {
			Hint string `json:"hint"`
		}
		require.NoError(t, resp.error.DecodeData(&hint))
		require.Equal(t, "read the docs", hint.Hint)
	})

	t.Run("wrapped Error is unwrapped", func(t *testing.T) {
		s := NewServer()
		s.SetOptions(requestUnmarshaler(func(*jsontext.Decoder, *Request) error {
			return fmt.Errorf("decoding envelope: %w", custom)
		}))

		out, err := s.ServeMessage(context.Background(), []byte(`{}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Equal(t, -32050, resp.error.Code())
		require.Equal(t, "bad envelope", resp.error.Message())
	})

	t.Run("plain error is classified as Invalid Request", func(t *testing.T) {
		s := NewServer()
		s.SetOptions(requestUnmarshaler(func(*jsontext.Decoder, *Request) error { return errors.New("nope") }))

		code, message, _ := decodeError(t, s, `{}`)
		require.Equal(t, CodeInvalidRequest, code)
		require.Equal(t, "nope", message)
	})
}

// A *Request unmarshaler replaces the decode, not the checks around it.
func TestRequestUnmarshalerCannotBypassServer(t *testing.T) {
	t.Run("Serve still owns the version verdict", func(t *testing.T) {
		// Even an unmarshaler that accepts anything cannot smuggle a bad
		// version past Serve, which validates every Request however it was
		// built.
		s := NewServer()
		s.SetOptions(requestUnmarshaler(func(d *jsontext.Decoder, req *Request) error {
			// Ignoring the message still means consuming its one value.
			if err := d.SkipValue(); err != nil {
				return err
			}
			req.jsonrpc, req.method, req.id = "1.0", "add", IntID(1)
			return nil
		}))
		out, err := s.ServeMessage(context.Background(), []byte(`{}`))
		require.NoError(t, err)
		resp := decodeResponse(t, out)
		require.Equal(t, CodeInvalidRequest, resp.error.Code())
		require.JSONEq(t, "1", resp.id.String())
	})

	t.Run("json/v2 still owns the framing", func(t *testing.T) {
		// An unmarshaler that never delegates to the default still cannot let
		// trailing data through: nothing may follow the one value it consumed.
		s := NewServer()
		s.SetOptions(requestUnmarshaler(func(d *jsontext.Decoder, req *Request) error {
			val, err := d.ReadValue()
			if err != nil {
				return err
			}
			return json.Unmarshal(val, req)
		}))
		s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
			return addResult{Sum: p.A + p.B}, nil
		})

		msg := `{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`
		out, err := s.ServeMessage(context.Background(), []byte(msg))
		require.NoError(t, err)
		require.JSONEq(t, `{"sum":3}`, string(decodeResponse(t, out).result))

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

		e, ok := errors.AsType[Error](err)
		require.True(t, ok, "the Error survives json/v2's wrapping")
		require.Equal(t, CodeInvalidRequest, e.Code())
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
		require.Equal(t, "add", req.Method())
		require.JSONEq(t, `{"a":1}`, req.Params().String())
		require.JSONEq(t, "1", req.ID().String())
	})

	// v1 is implemented over v2 in Go 1.27, so it honors the method too.
	t.Run("under encoding/json v1", func(t *testing.T) {
		var req Request
		err := jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"add","surprise":true,"id":1}`), &req)
		require.ErrorContains(t, err, "unknown member: surprise")
	})
}

// json/v2 does not zero the destination, so a stale id would make the second
// message look like a request rather than a notification.
func TestRequestUnmarshalJSONFromReplacesPriorMembers(t *testing.T) {
	var req Request
	require.NoError(t, json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1},"id":1}`), &req))
	require.False(t, req.IsNotification())

	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"ping"}`), &req))
	require.Empty(t, req.ID(), "the first message's id does not survive")
	require.Empty(t, req.Params(), "nor its params")
	require.True(t, req.IsNotification())
}

// The reset must not cost the server the id it attributes a decode failure to.
func TestRequestUnmarshalJSONFromKeepsMembersReadBeforeAFailure(t *testing.T) {
	var req Request
	err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":"add","surprise":true}`), &req)
	require.ErrorContains(t, err, "unknown member: surprise")
	require.JSONEq(t, "1", req.ID().String())
	require.Equal(t, "add", req.Method())
}

// An unmarshaler or marshaler in the options outranks a method, so SetOptions
// alone takes over either wire form.
func TestSetOptionsOverridesTheTypesOwnForm(t *testing.T) {
	t.Run("a *Request unmarshaler replaces the default", func(t *testing.T) {
		s := NewServer()
		// A decoder that tolerates the unknown member the default rejects.
		s.SetOptions(json.WithUnmarshalers(json.UnmarshalFromFunc(
			func(d *jsontext.Decoder, req *Request) error {
				var raw struct {
					Params Params `json:"params"`
					ID     ID     `json:"id"`
				}
				if err := json.UnmarshalDecode(d, &raw); err != nil {
					return err
				}
				req.jsonrpc, req.method = Version, "add"
				req.params, req.id = raw.Params, raw.ID
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

		out, err = s.ServeMessage(context.Background(), []byte(`[
			{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1},
			{"jsonrpc":"2.0","method":"missing","id":2}
		]`))
		require.NoError(t, err)
		require.JSONEq(t, `[
			{"jsonrpc":"2.0","result":{"sum":3},"id":1,"served_by":"test"},
			{"jsonrpc":"2.0","error":{"code":-32601,"message":"method not found: missing"},"id":2,"served_by":"test"}
		]`, string(out), "batch")

		out, err = s.ServeMessage(context.Background(), []byte(`{`))
		require.NoError(t, err)
		require.Contains(t, string(out), `"served_by":"test"`, "a message that never reaches Serve")
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
	if resp.isError() {
		if err := enc.WriteToken(jsontext.String("error")); err != nil {
			return err
		}
		if err := json.MarshalEncode(enc, resp.error); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.String("result")); err != nil {
			return err
		}
		if err := enc.WriteValue(resp.result); err != nil {
			return err
		}
	}
	if err := enc.WriteToken(jsontext.String("id")); err != nil {
		return err
	}
	if err := json.MarshalEncode(enc, resp.id); err != nil {
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

// A marshaler failure has no response left to report it in, so it surfaces as
// ServeMessage's error return.
func TestResponseMarshalerErrorSurfaces(t *testing.T) {
	s := NewServer()
	s.SetOptions(json.WithMarshalers(json.MarshalToFunc(func(*jsontext.Encoder, *Response) error {
		return errors.New("marshaler failed")
	})))
	s.Register("add", func(_ context.Context, p addParams) (addResult, error) {
		return addResult{Sum: p.A + p.B}, nil
	})

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":1}`))
	require.ErrorContains(t, err, "marshaler failed")
	require.Nil(t, out)
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

// TestErrorDataUsesJSONV2 pins the escaping: v1 would write \u003c here.
func TestErrorDataUsesJSONV2(t *testing.T) {
	e := withData(NewError(CodeServerError, "x"), map[string]string{"q": "<b>&</b>"})
	require.Equal(t, `{"q":"<b>&</b>"}`, string(e.Data()))

	var out map[string]string
	require.NoError(t, e.DecodeData(&out))
	require.Equal(t, "<b>&</b>", out["q"])
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

// Result and Data hand out copies, so a caller cannot alter the bytes a
// Response or Error holds.
func TestAccessorsDoNotAlias(t *testing.T) {
	resp := NewSuccessResponse(jsontext.Value(`[1]`), IntID(1))
	resp.Result()[0] = 'x'
	require.Equal(t, `[1]`, string(resp.Result()))

	e := withData(NewError(CodeServerError, "x"), []int{1})
	e.Data()[0] = 'x'
	require.Equal(t, `[1]`, string(e.Data()))
}

// An error object whose members are all zero is still an error response: the
// decoder tracks the member's presence, not the Error's value.
func TestResponseUnmarshalZeroError(t *testing.T) {
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","error":{"code":0,"message":""},"id":1}`), &resp))
	e, ok := resp.Error()
	require.True(t, ok)
	require.Equal(t, 0, e.Code())

	err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":1,"error":{"code":0,"message":""},"id":1}`), &resp)
	require.ErrorContains(t, err, "both result and error")
}

func TestErrorJSONRoundTrip(t *testing.T) {
	e := withData(NewError(CodeInvalidParams, "bad"), map[string]int{"x": 1})
	b, err := json.Marshal(e)
	require.NoError(t, err)
	require.JSONEq(t, `{"code":-32602,"message":"bad","data":{"x":1}}`, string(b))

	var got Error
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, e.Code(), got.Code())
	require.Equal(t, e.Message(), got.Message())
	require.JSONEq(t, `{"x":1}`, string(got.Data()))

	b, err = json.Marshal(NewError(CodeInvalidParams, "bad"))
	require.NoError(t, err)
	require.Equal(t, `{"code":-32602,"message":"bad"}`, string(b), "absent data is omitted")
}

func TestErrorUnmarshalRejectsMalformed(t *testing.T) {
	for _, in := range []string{
		`null`,
		`[]`,
		`{"code":1.5,"message":"x"}`,
		`{"code":"1","message":"x"}`,
		`{"code":1,"message":2}`,
		`{"code":1,"code":2,"message":"x"}`,
	} {
		e := withData(NewError(1, "keep"), 1)
		require.Error(t, json.Unmarshal([]byte(in), &e), in)
		require.Zero(t, e.Code(), "a failed decode leaves the receiver zeroed: %s", in)
	}

	var e Error
	require.NoError(t, json.Unmarshal([]byte(`{"code":1,"message":"x","extra":true}`), &e))
	require.Equal(t, NewError(1, "x"), e, "undefined members are skipped")
}
