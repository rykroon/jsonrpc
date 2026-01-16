package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type Echo struct{}

func (srv *Echo) Echo(args NoParams, reply *string) error {
	*reply = "echo"
	return nil
}

func TestJsonRpcServer(t *testing.T) {
	server := NewServer()

	server.Register(&Echo{})

	tests := []struct {
		name     string
		in       *Request
		expected *Response
	}{
		{
			"test_1",
			NewRequest("Echo.Echo", nil, NewId(123)),
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
