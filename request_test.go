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
		expected map[string]any
	}{
		{
			"no_id_no_params",
			NewRequest("test", nil, nil),
			map[string]any{"jsonrpc": "2.0", "method": "test"},
		},
		{
			"int_id_no_params",
			NewRequest("test", nil, NewId(123)),
			map[string]any{"jsonrpc": "2.0", "method": "test", "id": json.Number("123")},
		},
		{
			"string_id_no_params",
			NewRequest("test", nil, NewId("hello_world")),
			map[string]any{"jsonrpc": "2.0", "method": "test", "id": "hello_world"},
		},
		{
			"no_id_positional_params",
			NewRequest("test", positionalParams, nil),
			map[string]any{
				"jsonrpc": "2.0",
				"method":  "test",
				"params":  []any{json.Number("1"), json.Number("2"), json.Number("3")}},
		},
		{
			"no_id_named_params",
			NewRequest("test", namedParams, nil),
			map[string]any{
				"jsonrpc": "2.0",
				"method":  "test",
				"params": map[string]any{
					"one": json.Number("1"), "two": json.Number("2"), "three": json.Number("3"),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			decoder := json.NewDecoder(bytes.NewReader(jsonBytes))
			decoder.UseNumber()
			err = decoder.Decode(&got)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("Got %q, wanted %q", got, tc.expected)
			}
		})
	}
}
