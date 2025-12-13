package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewParamsArray(t *testing.T) {
	array := []any{1, 2, 3}
	params, err := NewParams(array)
	if err != nil {
		t.Fatal(err)
	}
	require.JSONEq(t, params.String(), `[1,2,3]`)

	s := "Hello World"
	_, err = NewParams(s)
	if err == nil {
		t.Fatal("expected error got nil")
	}
}

func TestNewParamsObject(t *testing.T) {
	obj := map[string]any{"one": 1, "two": 2, "three": 3}
	params, err := NewParams(obj)
	if err != nil {
		t.Fatal(err)
	}
	require.JSONEq(t, params.String(), `{"one":1,"two":2,"three":3}`)
}

func TestNewParamsFail(t *testing.T) {
	s := "Hello World"
	_, err := NewParams(s)
	if err == nil {
		t.Fatal("expected error got nil")
	}
}

func TestUnmarshalParams(t *testing.T) {
	s := []byte(`"not valid params"`)
	p := Params{}
	err := json.Unmarshal(s, &p)
	if err == nil {
		t.Error("expected an error got nil")
	}
}
