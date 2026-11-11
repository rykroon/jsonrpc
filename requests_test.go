package jsonrpc

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestRoundTripPreservesStringID(t *testing.T) {
	in := NewRequest("x", Params{}, StringID("abc"))
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out Request
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, `"abc"`, out.ID().String())
	require.False(t, out.IsNotification())
}

func TestRequestNotificationHasNoIDField(t *testing.T) {
	req := NewNotification("x", Params{})
	b, err := json.Marshal(req)
	require.NoError(t, err)
	require.NotContains(t, string(b), `"id"`)
	require.True(t, req.IsNotification())
}

// The envelope check is Request's own wire form, so it applies to any
// unmarshal, not only the ones a Server drives.
func TestRequestDecodesItself(t *testing.T) {
	t.Run("plain json.Unmarshal checks the envelope", func(t *testing.T) {
		var req Request
		err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":"add","params":5}`), &req)
		require.ErrorContains(t, err, "params must be an object or array")

		e, ok := errors.AsType[Error](err)
		require.True(t, ok, "the Error survives json/v2's wrapping")
		require.Equal(t, CodeInvalidRequest, e.Code())
	})

	t.Run("params must be structured", func(t *testing.T) {
		var req Request
		err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"add","params":null,"id":1}`), &req)
		require.ErrorContains(t, err, "params must be an object or array")
	})

	t.Run("a good request decodes", func(t *testing.T) {
		var req Request
		require.NoError(t, json.Unmarshal(
			[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1},"id":1}`), &req))
		require.Equal(t, "add", req.Method())
		require.JSONEq(t, `{"a":1}`, req.Params().String())
		require.JSONEq(t, "1", req.ID().String())
	})

	// v1 is implemented over v2 in Go 1.27, so it honors the method too.
	t.Run("under encoding/json v1", func(t *testing.T) {
		var req Request
		err := jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"add","params":5,"id":1}`), &req)
		require.ErrorContains(t, err, "params must be an object or array")
	})

	t.Run("members the spec does not define are skipped", func(t *testing.T) {
		var req Request
		require.NoError(t, json.Unmarshal(
			[]byte(`{"jsonrpc":"2.0","meta":{"a":[1,{"b":2}]},"method":"add","surprise":true,"id":1}`), &req))
		require.Equal(t, "add", req.Method())
		require.Equal(t, IntID(1), req.ID())
	})
}

// json/v2 does not zero the destination, so a stale id would make the second
// message look like a request rather than a notification.
func TestRequestUnmarshalJSONFromReplacesPriorMembers(t *testing.T) {
	var req Request
	require.NoError(t, json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1},"id":1}`), &req))
	require.False(t, req.IsNotification())

	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","method":"ping"}`), &req))
	require.Empty(t, req.ID(), "the first message's id does not survive")
	require.Empty(t, req.Params(), "nor its params")
	require.True(t, req.IsNotification())
}

// The reset must not cost the server the id it attributes a decode failure to.
func TestRequestUnmarshalJSONFromKeepsMembersReadBeforeAFailure(t *testing.T) {
	var req Request
	err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"method":"add","params":5}`), &req)
	require.ErrorContains(t, err, "params must be an object or array")
	require.JSONEq(t, "1", req.ID().String())
	require.Equal(t, "add", req.Method())
}

// Marshaling is lenient; Client.Call and Notify are what refuse an empty method.
func TestRequestMarshalAlwaysWritesVersion(t *testing.T) {
	out, err := json.Marshal(Request{})
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","method":""}`, string(out))
}
