package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewParams(t *testing.T) {
	var params *Params
	array := []any{1, 2, 3}
	params, err := NewParams(array)
	if err != nil {
		t.Fatal(err)
	}
	require.JSONEq(t, params.String(), `[1,2,3]`)

	obj := map[string]any{"one": 1, "two": 2, "three": 3}
	params, err = NewParams(obj)
	if err != nil {
		t.Fatal(err)
	}
	require.JSONEq(t, params.String(), `{"one":1,"two":2,"three":3}`)

	s := "Hello World"
	_, err = NewParams(s)
	if err == nil {
		t.Fatal("expected error got nil")
	}
}

func TestUnmarshalParams(t *testing.T) {

}
