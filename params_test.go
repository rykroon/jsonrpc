package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewParamsMap(t *testing.T) {
	params := NewParamFromMap(map[string]any{"one": 1, "two": 2})
	require.Equal(t, params.ByName(), true)
	require.Equal(t, params.ByPosition(), false)
}

func TestNewParamsObject(t *testing.T) {
	params := NewParamFromSlice([]string{"one", "two", "three"})
	require.Equal(t, params.ByName(), false)
	require.Equal(t, params.ByPosition(), true)
}
