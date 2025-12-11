package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseEncoding(t *testing.T) {
	err := NewError(0, "test", nil).(*Error)
	errWithData := NewError(0, "test", map[string]any{"foo": "bar"}).(*Error)

	tests := []struct {
		name     string
		response *Response
		expected string
	}{
		{
			"err_resp_with_null_id",
			NewErrorResp(NullId(), err),
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
			actual, err := json.Marshal(tc.response)
			if err != nil {
				t.Fatal(err)
			}

			require.JSONEq(t, tc.expected, string(actual))
		})
	}
}

func TestResponseDecoding(t *testing.T) {
	err := NewError(0, "test", nil).(*Error)
	errWithData := NewError(0, "test", map[string]any{"foo": "bar"}).(*Error)

	tests := []struct {
		name       string
		jsonString string
		expected   *Response
	}{
		{
			"err_resp_with_null_id",
			`{"jsonrpc": "2.0", "id": null, "error": {"code": 0, "message": "test"}}`,
			NewErrorResp(NullId(), err),
		},
		{
			"err_resp_with_string_id",
			`{"jsonrpc": "2.0", "id": "Hello World", "error": {"code": 0, "message": "test"}}`,
			NewErrorResp(NewId("Hello World"), err),
		},
		{
			"err_resp_with_int_id",
			`{"jsonrpc": "2.0", "id": 123, "error": {"code": 0, "message": "test"}}`,
			NewErrorResp(NewId(123), err),
		},
		{
			"error_with_data",
			`{"jsonrpc": "2.0", "id": 123, "error": {"code": 0, "message": "test", "data": {"foo": "bar"}}}`,
			NewErrorResp(NewId(123), errWithData),
		},
		{
			"success_resp",
			`{"jsonrpc": "2.0", "id": "Hello World", "result": 123}`,
			NewSuccessResp(NewId("Hello World"), json.RawMessage([]byte("123"))),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var actual *Response
			err := json.Unmarshal([]byte(tc.jsonString), &actual)
			if err != nil {
				t.Fatal(err)
			}

			require.Equal(t, tc.expected, actual)
		})
	}
}
