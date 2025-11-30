package jsonrpc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestRequestEncoding(t *testing.T) {
	positionalParams, err := NewParams([]any{1.0, 2.0, 3.0})
	if err != nil {
		t.Fatal(err)
	}
	namedParams, err := NewParams(map[string]any{"one": 1.0, "two": 2.0, "three": 3.0})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		request  *Request
		expected string
	}{
		{
			"no_id_no_params",
			NewRequest("test", nil, nil),
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
		{
			"no_id_positional_params",
			NewRequest("test", positionalParams, nil),
			`{"jsonrpc":"2.0", "method": "test", "params": [1, 2, 3]}`,
		},
		{
			"no_id_named_params",
			NewRequest("test", namedParams, nil),
			`{"jsonrpc": "2.0", "method": "test", "params": {"one":1, "two":2, "three":3}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			got := make(NestedMap)
			decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
			err = decoder.Decode(&got)
			if err != nil {
				t.Fatal(err)
			}

			decodedExpected := make(NestedMap)
			decoder = json.NewDecoder(bytes.NewReader([]byte(tc.expected)))
			err = decoder.Decode(&decodedExpected)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, decodedExpected) {
				t.Errorf("\nGot\n%q\nwanted\n%q", got, decodedExpected)
			}
		})
	}
}
