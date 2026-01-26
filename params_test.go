package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewParamsMap(t *testing.T) {
	params := NewParamsMap(map[string]any{"one": 1, "two": 2})
	require.Equal(t, params.ByName(), true)
	require.Equal(t, params.ByPosition(), false)

	one, ok := params.Get("one")
	require.Equal(t, one, 1)
	require.Equal(t, ok, true)

	wrong, ok := params.Get("wrong")
	require.Equal(t, wrong, nil)
	require.Equal(t, ok, false)
}

func TestNewParamsObject(t *testing.T) {
	params := NewParamsSlice([]string{"one", "two", "three"})
	require.Equal(t, params.ByName(), false)
	require.Equal(t, params.ByPosition(), true)

	one, ok := params.At(0)
	require.Equal(t, one, "one")
	require.Equal(t, ok, true)

	wrong, ok := params.At(100)
	require.Equal(t, wrong, nil)
	require.Equal(t, ok, false)
}
