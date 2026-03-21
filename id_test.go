package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewIdString(t *testing.T) {
	id := NewId("Hello World")
	s, ok := id.String()
	require.Equal(t, ok, true)
	require.Equal(t, s, "Hello World")

	i, ok := id.Int()
	require.Equal(t, ok, false)
	require.Equal(t, i, 0)

	require.Equal(t, id.IsNull(), false)
}

func TestNewIdInt(t *testing.T) {
	id := NewId(123)
	s, ok := id.String()
	require.Equal(t, ok, false)
	require.Equal(t, s, "")

	i, ok := id.Int()
	require.Equal(t, ok, true)
	require.Equal(t, i, 123)

	require.Equal(t, id.IsNull(), false)
}

func TestNullId(t *testing.T) {
	id := NullId()
	s, ok := id.String()
	require.Equal(t, ok, false)
	require.Equal(t, s, "")

	i, ok := id.Int()
	require.Equal(t, ok, false)
	require.Equal(t, i, 0)

	require.Equal(t, id.IsNull(), true)
}
