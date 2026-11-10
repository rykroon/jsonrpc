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
