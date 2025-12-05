package jsonrpc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestResponse(t *testing.T) {
	err := NewError(0, "test", nil).(*Error)
	tests := []struct {
		name     string
		response *Response
		expected string
	}{
		{
			"err_resp_null_id",
			NewErrorResp(nil, err),
			`{"jsonrpc": "2.0", "id": null, "error": {"code": 0, "message": "test"}}`,
		},
		{
			"err_resp_with_id",
			NewErrorResp(NewId("Hello World"), err),
			`{"jsonrpc": "2.0", "id": "Hello World", "error": {"code": 0, "message": "test"}}`,
		},
		{
			"success_resp",
			NewSuccessResp(NewId("Hello World"), json.RawMessage([]byte("123"))),
			`{"jsonrpc": "2.0", "id": "Hello World", "result": 123}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tc.response)
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
