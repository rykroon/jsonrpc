package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequest(t *testing.T) {
	req := NewRequest("test", nil, nil)
	require.Equal(t, req.Jsonrpc(), "2.0")
	require.Equal(t, req.Method(), "test")
	require.Equal(t, req.Params(), nil)
	require.Equal(t, req.Id(), nil)
	require.Equal(t, req.IsNotification(), true)
}
