package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestEncoding(t *testing.T) {
	// positionalParams, err := NewParams([]any{1.0, 2.0, 3.0})
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// namedParams, err := NewParams(map[string]any{"one": 1.0, "two": 2.0, "three": 3.0})
	// if err != nil {
	// 	t.Fatal(err)
	// }

	tests := []struct {
		name     string
		request  *Request
		expected string
	}{
		{
			"no_id_no_params",
			NewNotification("test", nil),
			`{"jsonrpc": "2.0", "method": "test"}`,
		},
		{
			"int_id_no_params",
			NewRequest("test", nil, NewId(123)),
			`{"jsonrpc": "2.0", "method": "test", "id": 123}`,
		},
		{
			"string_id_no_params",
			NewRequest("test", nil, NewId("hello_world")),
			`{"jsonrpc": "2.0", "method": "test", "id": "hello_world"}`,
		},
		// {
		// 	"no_id_positional_params",
		// 	NewNotification("test", positionalParams),
		// 	`{"jsonrpc":"2.0", "method": "test", "params": [1, 2, 3]}`,
		// },
		// {
		// 	"no_id_named_params",
		// 	NewNotification("test", namedParams),
		// 	`{"jsonrpc": "2.0", "method": "test", "params": {"one": 1, "two": 2, "three": 3}}`,
		// },
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			require.JSONEq(t, tc.expected, string(actual))
		})
	}
}

func TestRequestDecoding(t *testing.T) {
	// positionalParams, err := NewParams([]any{1.0, 2.0, 3.0})
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// namedParams, err := NewParams(map[string]any{"one": 1.0, "two": 2.0, "three": 3.0})
	// if err != nil {
	// 	t.Fatal(err)
	// }

	tests := []struct {
		name       string
		jsonString string
		expected   *Request
	}{
		{
			"no_id_no_params",
			`{"jsonrpc": "2.0", "method": "test"}`,
			NewNotification("test", nil),
		},
		{
			"int_id_no_params",
			`{"jsonrpc": "2.0", "method": "test", "id": 123}`,
			NewRequest("test", nil, NewId(123)),
		},
		{
			"string_id_no_params",
			`{"jsonrpc": "2.0", "method": "test", "id": "hello_world"}`,
			NewRequest("test", nil, NewId("hello_world")),
		},
		// {
		// 	"no_id_positional_params",
		// 	`{"jsonrpc":"2.0", "method": "test", "params": [1,2,3]}`,
		// 	NewNotification("test", positionalParams),
		// },
		// {
		// 	"no_id_named_params",
		// 	`{"jsonrpc": "2.0", "method": "test", "params": {"one":1, "two":2, "three":3}}`,
		// 	NewNotification("test", namedParams),
		// },
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := &Request{}
			err := json.Unmarshal([]byte(tc.jsonString), &actual)
			if err != nil {
				t.Fatal(err)
			}

			require.Equal(t, tc.expected.JsonRpc, actual.JsonRpc)
			require.Equal(t, tc.expected.Method, actual.Method)
			require.Equal(t, tc.expected.Id, actual.Id)
			// if tc.expected.Params.IsAbsent() {
			// 	require.Equal(t, tc.expected.Params, actual.Params)
			// } else {
			// 	require.JSONEq(t, tc.expected.Params.String(), actual.Params.String())
			// }

		})
	}
}
