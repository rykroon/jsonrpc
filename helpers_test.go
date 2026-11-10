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
