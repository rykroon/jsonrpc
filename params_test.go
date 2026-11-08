package jsonrpc

import (
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

func TestParamsDecodeInto(t *testing.T) {
	t.Run("decodes", func(t *testing.T) {
		var got addParams
		require.NoError(t, mustParams(t, addParams{A: 1, B: 2}).DecodeInto(&got))
		require.Equal(t, addParams{A: 1, B: 2}, got)
	})

	t.Run("zero leaves the target untouched", func(t *testing.T) {
		got := addParams{A: 9}
		require.NoError(t, Params{}.DecodeInto(&got))
		require.Equal(t, addParams{A: 9}, got)
	})

	t.Run("honors options", func(t *testing.T) {
		p := mustParams(t, jsontext.Value(`{"a":1,"extra":true}`))
		var got addParams
		require.NoError(t, p.DecodeInto(&got), "unknown members are tolerated by default")
		require.Error(t, p.DecodeInto(&got, json.RejectUnknownMembers(true)))
	})

	t.Run("a mismatch is an error", func(t *testing.T) {
		var got addParams
		require.Error(t, mustParams(t, []int{1}).DecodeInto(&got))
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
