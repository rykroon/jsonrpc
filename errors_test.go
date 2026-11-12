package jsonrpc

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorIsMatchesCode(t *testing.T) {
	errNotFound := NewError(-32004, "not found")

	require.ErrorIs(t, withData(NewError(-32004, "user 7 not found"), []int{7}), errNotFound)
	require.ErrorIs(t, fmt.Errorf("lookup: %w", errNotFound), errNotFound)
	require.NotErrorIs(t, NewError(-32005, "not found"), errNotFound)
	require.NotErrorIs(t, errors.New("not found"), errNotFound)

	// A server error still matches after crossing the wire.
	s := NewServer()
	s.Register("find", func(context.Context, struct{}) (any, error) {
		return nil, errNotFound
	})
	_, err := NewClient(s.Sender()).Call(context.Background(), "find", struct{}{}, nil)
	require.ErrorIs(t, err, errNotFound)
}

func TestErrorWithData(t *testing.T) {
	base := NewError(-32001, "custom")
	e, err := base.WithData(map[string]int{"x": 1})
	require.NoError(t, err)
	require.JSONEq(t, `{"x":1}`, string(e.Data()))
	require.Equal(t, base.Code(), e.Code())
	require.Equal(t, base.Message(), e.Message())

	// The receiver is never modified, so shared errors are safe.
	require.Empty(t, base.Data())

	_, err = e.WithData(make(chan int))
	require.Error(t, err)
	require.JSONEq(t, `{"x":1}`, string(e.Data()))
}

func TestErrorWithDataOptions(t *testing.T) {
	e, err := NewError(-32001, "custom").WithData(
		map[string][]int{"x": nil},
		json.FormatNilSliceAsNull(true),
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"x":null}`, string(e.Data()))
}

// withData is WithData for test data known to be marshalable.
func withData(e Error, data any) Error {
	c, err := e.WithData(data)
	if err != nil {
		panic(err)
	}
	return c
}

// TestErrorDataUsesJSONV2 pins the escaping: v1 would write \u003c here.
func TestErrorDataUsesJSONV2(t *testing.T) {
	e := withData(NewError(CodeServerError, "x"), map[string]string{"q": "<b>&</b>"})
	require.Equal(t, `{"q":"<b>&</b>"}`, string(e.Data()))

	var out map[string]string
	require.NoError(t, e.DecodeData(&out))
	require.Equal(t, "<b>&</b>", out["q"])
}

func TestErrorJSONRoundTrip(t *testing.T) {
	e := withData(NewError(CodeInvalidParams, "bad"), map[string]int{"x": 1})
	b, err := json.Marshal(e)
	require.NoError(t, err)
	require.JSONEq(t, `{"code":-32602,"message":"bad","data":{"x":1}}`, string(b))

	var got Error
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, e.Code(), got.Code())
	require.Equal(t, e.Message(), got.Message())
	require.JSONEq(t, `{"x":1}`, string(got.Data()))

	b, err = json.Marshal(NewError(CodeInvalidParams, "bad"))
	require.NoError(t, err)
	require.Equal(t, `{"code":-32602,"message":"bad"}`, string(b), "absent data is omitted")
}

func TestErrorUnmarshalRejectsMalformed(t *testing.T) {
	for _, in := range []string{
		`null`,
		`[]`,
		`{"code":1.5,"message":"x"}`,
		`{"code":"1","message":"x"}`,
		`{"code":1,"message":2}`,
		`{"code":null,"message":"x"}`,
		`{"code":1,"message":null}`,
		`{"code":true,"message":"x"}`,
		`{"code":1,"code":2,"message":"x"}`,
	} {
		e := withData(NewError(1, "keep"), 1)
		require.Error(t, json.Unmarshal([]byte(in), &e), in)
		require.Zero(t, e.Code(), "a failed decode leaves the receiver zeroed: %s", in)
	}

	var e Error
	require.NoError(t, json.Unmarshal([]byte(`{"code":1,"message":"x","extra":true}`), &e))
	require.Equal(t, NewError(1, "x"), e, "undefined members are skipped")
}

// Data hands out a copy, so a caller cannot alter the bytes an Error holds.
func TestErrorDataDoesNotAlias(t *testing.T) {
	e := withData(NewError(CodeServerError, "x"), []int{1})
	e.Data()[0] = 'x'
	require.Equal(t, `[1]`, string(e.Data()))
}

func TestSentinelsMatchByCode(t *testing.T) {
	s := NewServer()
	c := NewClient(s.Sender())
	_, err := c.Call(context.Background(), "missing", nil, nil)
	require.ErrorIs(t, err, ErrMethodNotFound)
	require.NotErrorIs(t, err, ErrInvalidParams)
}

func TestErrorDecodeRequiresCodeAndMessage(t *testing.T) {
	for _, in := range []string{`{}`, `{"code":1}`, `{"message":"m"}`} {
		var e Error
		require.Error(t, json.Unmarshal([]byte(in), &e), in)
	}
	var e Error
	require.NoError(t, json.Unmarshal([]byte(`{"code":1,"message":""}`), &e))

	var resp Response
	require.Error(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","error":{},"id":1}`), &resp))
}
