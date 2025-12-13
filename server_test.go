package jsonrpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJsonRpcServer(t *testing.T) {
	server := NewServer()

	server.RegisterFunc("echo", func(ctx context.Context, params Params) (any, error) {
		return "echo", nil
	})

	tests := []struct {
		name     string
		in       *Request
		expected *Response
	}{
		{
			"test_1",
			NewRequest("echo", nil, NewId(123)),
			NewSuccessResp(NewId(123), []byte(`"echo"`)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := server.ServeJsonRpc(t.Context(), tc.in)
			require.Equal(t, actual, tc.expected)
		})
	}
}
