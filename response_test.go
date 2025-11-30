package jsonrpc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestResponse(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
		expected map[string]any
	}{
		{
			"resp_null_id",
			NewErrorResp(nil, NewErrorTyped(0, "test", nil)),
			map[string]any{"id": "null", "error": map[string]any{"code": json.Number("0"), "message": "test"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.response)
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
