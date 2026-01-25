package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIterJsonObject(t *testing.T) {
	data := []byte(`{"one": 1, "two": 2, "three": 3}`)
	for k, v := range IterJsonObject(data) {
		switch k {
		case "one":
			require.Equal(t, v, json.RawMessage("1"))
		case "two":
			require.Equal(t, v, json.RawMessage("2"))
		case "three":
			require.Equal(t, v, json.RawMessage("3"))
		}
	}
}

func TestIterJsonObjectEmpty(t *testing.T) {
	data := []byte(`{}`)
	for range IterJsonObject(data) {
		t.Error("should not enter loop")
	}
}

func TestIterJsonObjectNested(t *testing.T) {
	data := []byte(`{"one": 1, "two": {"a": 1, "b": 2, "c": 3}, "three": 3}`)
	for k, v := range IterJsonObject(data) {
		switch k {
		case "one":
			require.Equal(t, v, json.RawMessage("1"))
		case "two":
			require.Equal(t, v, json.RawMessage(`{"a": 1, "b": 2, "c": 3}`))
		case "three":
			require.Equal(t, v, json.RawMessage("3"))
		}
	}
}
