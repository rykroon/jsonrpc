package jsonrpc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewIdString(t *testing.T) {
	s1 := "Hello World"
	id := NewId(s1)

	s2, err := id.AsString()
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	require.Equal(t, s1, s2)

	_, err = id.AsInt64()
	if err == nil {
		t.Error("expected an error")
	}
}

func TestNewIdInt(t *testing.T) {
	i1 := 100
	id := NewId(i1)

	i2, err := id.AsInt64()
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	require.Equal(t, int64(i1), i2)

	_, err = id.AsString()
	if err == nil {
		t.Error("expected an error")
	}
}
