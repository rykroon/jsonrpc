package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponse(t *testing.T) {
	resp := NewSuccessResponse(100, NewIdInt(123))
	require.Equal(t, resp.Jsonrpc(), "2.0")
	require.Equal(t, resp.Id(), NewIdInt(123))
	require.Equal(t, resp.Result(), 100)
}
