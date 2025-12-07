package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponse(t *testing.T) {
	err := NewError(0, "test", nil).(*Error)
	errWithData := NewError(0, "test", map[string]any{"foo": "bar"}).(*Error)

	tests := []struct {
		name     string
		response *Response
		expected string
	}{
		{
			"err_resp_with_null_id",
			NewErrorResp(nil, err),
			`{"jsonrpc": "2.0", "id": null, "error": {"code": 0, "message": "test"}}`,
		},
		{
			"err_resp_with_string_id",
			NewErrorResp(NewId("Hello World"), err),
			`{"jsonrpc": "2.0", "id": "Hello World", "error": {"code": 0, "message": "test"}}`,
		},
		{
			"err_resp_with_int_id",
			NewErrorResp(NewId(123), err),
			`{"jsonrpc": "2.0", "id": 123, "error": {"code": 0, "message": "test"}}`,
		},
		{
			"error_with_data",
			NewErrorResp(NewId(123), errWithData),
			`{"jsonrpc": "2.0", "id": 123, "error": {"code": 0, "message": "test", "data": {"foo": "bar"}}}`,
		},
		{
			"success_resp",
			NewSuccessResp(NewId("Hello World"), json.RawMessage([]byte("123"))),
			`{"jsonrpc": "2.0", "id": "Hello World", "result": 123}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.response)
			if err != nil {
				t.Fatal(err)
			}

			require.JSONEq(t, tc.expected, string(b))
		})
	}
}
