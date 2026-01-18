package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJsonRpcServer(t *testing.T) {
	server := NewServer()

	type EchoParams struct {
		Echo string `json:"echo"`
	}

	server.Register("echo", func(params EchoParams) (string, error) {
		return params.Echo, nil
	})

	params, _ := NewParams(EchoParams{Echo: "echo"})

	tests := []struct {
		name     string
		in       *Request
		expected *Response
	}{
		{
			"test_1",
			NewRequest("echo", params, NewId(123)),
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
