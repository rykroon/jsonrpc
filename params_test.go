package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParamsKinds(t *testing.T) {
	obj := mustParams(t, jsontext.Value(`{"a":1}`))
	require.True(t, obj.IsObject())
	require.False(t, obj.IsArray())
	require.False(t, obj.IsZero())
	require.Equal(t, `{"a":1}`, obj.String())

	arr := mustParams(t, []int{1, 2})
	require.True(t, arr.IsArray())
	require.False(t, arr.IsObject())
	require.Equal(t, `[1,2]`, arr.String())

	var zero Params
	require.True(t, zero.IsZero())
	require.False(t, zero.IsObject())
	require.False(t, zero.IsArray())
	require.Equal(t, "", zero.String())
}

func TestParamsDecode(t *testing.T) {
	t.Run("decodes", func(t *testing.T) {
		var got addParams
		require.NoError(t, mustParams(t, addParams{A: 1, B: 2}).Decode(&got))
		require.Equal(t, addParams{A: 1, B: 2}, got)
	})

	t.Run("zero leaves the target untouched", func(t *testing.T) {
		got := addParams{A: 9}
		require.NoError(t, Params{}.Decode(&got))
		require.Equal(t, addParams{A: 9}, got)
	})

	t.Run("honors options", func(t *testing.T) {
		p := mustParams(t, jsontext.Value(`{"a":1,"extra":true}`))
		var got addParams
		require.NoError(t, p.Decode(&got), "unknown members are tolerated by default")
		require.Error(t, p.Decode(&got, json.RejectUnknownMembers(true)))
	})

	t.Run("a mismatch is an error", func(t *testing.T) {
		var got addParams
		require.Error(t, mustParams(t, []int{1}).Decode(&got))
	})
}

func TestParamsMap(t *testing.T) {
	p := mustParams(t, jsontext.Value(`{"n":12345678901234567890,"s":"x"}`))
	m, err := p.Map()
	require.NoError(t, err)
	require.Len(t, m, 2)
	require.Equal(t, "12345678901234567890", string(m["n"]), "members stay undecoded, so no float64 rounding")
	require.Equal(t, `"x"`, string(m["s"]))

	for _, p := range []Params{{}, mustParams(t, []int{1})} {
		m, err := p.Map()
		require.ErrorContains(t, err, "params must be a JSON object")
		require.Nil(t, m)
	}
}

func TestParamsSlice(t *testing.T) {
	p := mustParams(t, jsontext.Value(`[12345678901234567890,"x",{}]`))
	s, err := p.Slice()
	require.NoError(t, err)
	require.Equal(t, []string{"12345678901234567890", `"x"`, `{}`},
		[]string{string(s[0]), string(s[1]), string(s[2])})

	empty, err := mustParams(t, []int{}).Slice()
	require.NoError(t, err)
	require.Empty(t, empty)

	for _, p := range []Params{{}, mustParams(t, jsontext.Value(`{}`))} {
		s, err := p.Slice()
		require.ErrorContains(t, err, "params must be a JSON array")
		require.Nil(t, s)
	}
}

func TestParamsJSON(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		for _, in := range []Params{mustParams(t, jsontext.Value(`{"a":[1,2]}`)), mustParams(t, []string{"x"})} {
			b, err := json.Marshal(in)
			require.NoError(t, err)
			var out Params
			require.NoError(t, json.Unmarshal(b, &out))
			require.Equal(t, in.String(), out.String())
		}
	})

	t.Run("only objects and arrays decode", func(t *testing.T) {
		for _, in := range []string{`null`, `5`, `"x"`, `true`} {
			var p Params
			err := json.Unmarshal([]byte(in), &p)
			require.ErrorContains(t, err, "params must be a JSON object or array", in)
			require.True(t, p.IsZero(), in)
		}
	})

	t.Run("a decoded value does not alias the input", func(t *testing.T) {
		in := []byte(`{"a":1}`)
		var p Params
		require.NoError(t, json.Unmarshal(in, &p))
		copy(in, `[9,9,9]`)
		require.Equal(t, `{"a":1}`, p.String())
	})

	t.Run("zero is omitted or refused", func(t *testing.T) {
		b, err := json.Marshal(struct {
			Params Params `json:"params,omitzero"`
		}{})
		require.NoError(t, err)
		require.Equal(t, `{}`, string(b))

		_, err = json.Marshal(Params{})
		require.ErrorContains(t, err, "zero Params")
	})
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
		_, err := c.Call(context.Background(), "m", 5, nil)
		require.ErrorContains(t, err, "params must be a JSON object or array")
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
