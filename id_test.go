package jsonrpc

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIDConstructors(t *testing.T) {
	for want, id := range map[string]ID{
		"42":                   NewIntID(42),
		"-7":                   NewIntID(int64(-7)),
		"0":                    NewIntID(uint8(0)),
		"18446744073709551615": NewIntID(uint64(1<<64 - 1)),
		`"abc"`:                NewStringID("abc"),
		`""`:                   NewStringID(""),
		`"a\"b<"`:              NewStringID(`a"b<`),
		`"\ufffd"`:             NewStringID("\xff"),
	} {
		require.False(t, id.IsZero(), want)
		require.JSONEq(t, want, id.String())
	}
	require.True(t, ID{}.IsZero())
	require.True(t, newNullID().IsNull())
}

func TestIDJSON(t *testing.T) {
	t.Run("round trip compares equal", func(t *testing.T) {
		for _, in := range []ID{NewIntID(-7), NewIntID(uint64(1<<64 - 1)), NewStringID("a<b"), newNullID()} {
			b, err := json.Marshal(in)
			require.NoError(t, err)
			var out ID
			require.NoError(t, json.Unmarshal(b, &out))
			require.Equal(t, in, out, string(b))
		}
	})

	t.Run("escapes are normalized", func(t *testing.T) {
		var id ID
		require.NoError(t, json.Unmarshal([]byte(`"\u0061\u003c"`), &id))
		require.Equal(t, NewStringID("a<"), id)
	})

	t.Run("numbers are kept verbatim", func(t *testing.T) {
		var id ID
		require.NoError(t, json.Unmarshal([]byte(`1e2`), &id))
		require.Equal(t, "1e2", id.String())
	})

	t.Run("bad kinds are rejected", func(t *testing.T) {
		for _, in := range []string{`true`, `{}`, `[1]`} {
			var id ID
			require.ErrorContains(t, json.Unmarshal([]byte(in), &id), "id must be a JSON string, number, or null", in)
			require.True(t, id.IsZero())
		}
	})

	t.Run("zero is omitted or refused", func(t *testing.T) {
		b, err := json.Marshal(struct {
			ID ID `json:"id,omitzero"`
		}{})
		require.NoError(t, err)
		require.Equal(t, `{}`, string(b))

		_, err = json.Marshal(ID{})
		require.ErrorContains(t, err, "zero ID")
	})
}
