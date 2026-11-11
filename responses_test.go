package jsonrpc

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseAlwaysHasID(t *testing.T) {
	// Both shapes carry an id member, even when it is null, and a success
	// response always carries a result member, even when it is null. The
	// constructors normalize an empty value, so this holds by construction.
	b, err := json.Marshal(NewErrorResponse(NewError(CodeParseError, "bad"), ID{}))
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)

	b, err = json.Marshal(NewSuccessResponse(nil, ID{}))
	require.NoError(t, err)
	require.Contains(t, string(b), `"id":null`)
	require.Contains(t, string(b), `"result":null`)
}

func TestResponseMarshalsExactShape(t *testing.T) {
	ok := NewSuccessResponse(jsontext.Value(`{"sum":3}`), IntID(1))
	b, err := json.Marshal(ok)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(b))

	bad := NewErrorResponse(NewError(CodeMethodNotFound, "nope"), StringID("x"))
	b, err = json.Marshal(bad)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":"x"}`, string(b))

	// Member order must not depend on which json package the caller reaches
	// for: the examples marshal responses with v1, ServeMessage with v2.
	v1, err := jsonv1.Marshal(ok)
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, string(v1))
}

// A hand-built Response can hold a shape the spec forbids. Marshaling reports
// it rather than emitting it, and rejects a malformed raw result the same way.
func TestResponseMarshalRejectsInvalidShape(t *testing.T) {
	_, err := json.Marshal(&Response{})
	require.ErrorContains(t, err, "neither result nor error", "the zero Response is no response")

	// A result marks a success response, so a stray error is never sent with it.
	b, err := json.Marshal(&Response{
		result: jsontext.Value("1"),
		error:  NewError(CodeInternalError, "x"),
		id:     IntID(1),
	})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":1}`, string(b))

	_, err = json.Marshal(&Response{result: jsontext.Value(`{oops`), id: IntID(1)})
	require.Error(t, err, "a malformed result must not reach the wire")

	// An empty id is not malformed: the spec's own fallback is null.
	b, err = json.Marshal(&Response{result: jsontext.Value("1")})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":null}`, string(b))

	// Nor is a version the caller never set: the protocol's is written, not the
	// field's.
	b, err = json.Marshal(&Response{result: jsontext.Value("1"), id: IntID(1)})
	require.NoError(t, err)
	require.Equal(t, `{"jsonrpc":"2.0","result":1,"id":1}`, string(b))
}

func TestResponseUnmarshalJSONFrom(t *testing.T) {
	const (
		fails = iota
		success
		failure
	)
	cases := []struct {
		name string
		in   string
		want int // the shape expected, or fails when the decode must not succeed
	}{
		{"success", `{"jsonrpc":"2.0","result":{"sum":3},"id":1}`, success},
		{"null result is a success", `{"jsonrpc":"2.0","result":null,"id":1}`, success},
		{"error", `{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":1}`, failure},
		{"null id", `{"jsonrpc":"2.0","error":{"code":-32700,"message":"nope"},"id":null}`, failure},
		{"unknown members are tolerated", `{"jsonrpc":"2.0","result":1,"id":1,"extra":true}`, success},
		{"a structured unknown member is skipped whole", `{"jsonrpc":"2.0","meta":{"a":[1,{"b":2}]},"result":1,"id":1}`, success},
		{"members in any order", `{"id":1,"result":1,"jsonrpc":"2.0"}`, success},
		{"non-string jsonrpc", `{"jsonrpc":2.0,"result":1,"id":1}`, fails},
		{"error is not an object", `{"jsonrpc":"2.0","error":"nope","id":1}`, fails},
		{"duplicate member inside a nested value", `{"jsonrpc":"2.0","result":{"a":1,"a":2},"id":1}`, fails},
		{"truncated after a member", `{"jsonrpc":"2.0","result":1,"id":1`, fails},
		{"both result and error", `{"jsonrpc":"2.0","result":1,"error":{"code":-1,"message":"x"},"id":1}`, fails},
		{"neither result nor error", `{"jsonrpc":"2.0","id":1}`, fails},
		{"wrong version", `{"jsonrpc":"1.0","result":1,"id":1}`, fails},
		{"missing id", `{"jsonrpc":"2.0","result":1}`, fails},
		{"duplicate member", `{"jsonrpc":"2.0","result":1,"result":2,"id":1}`, fails},
		{"not an object", `[1]`, fails},
		{"malformed", `{"jsonrpc":`, fails},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Response
			err := json.Unmarshal([]byte(tc.in), &got)
			if tc.want == fails {
				require.Error(t, err)
				require.Zero(t, got, "a rejected response leaves the receiver untouched")
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, got.id)
			require.Equal(t, Version, got.jsonrpc)
			if tc.want == success {
				require.False(t, got.isError())
				require.NotEmpty(t, got.result)
			} else {
				require.True(t, got.isError())
				require.Nil(t, got.result)
			}
		})
	}
}

// json/v2 does not zero the destination, so a Response decoded into twice must
// not keep the first message's members.
func TestResponseUnmarshalJSONFromReplacesPriorMembers(t *testing.T) {
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":{"sum":3},"id":1}`), &resp))
	require.NotNil(t, resp.result)

	require.NoError(t, json.Unmarshal(
		[]byte(`{"jsonrpc":"2.0","error":{"code":-32601,"message":"nope"},"id":2}`), &resp))
	require.Nil(t, resp.result, "the first message's result does not survive")
	require.True(t, resp.isError())
	require.JSONEq(t, "2", resp.id.String())

	// A rejected message clears it too, so the previous response cannot be
	// mistaken for this one.
	require.Error(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":3}`), &resp))
	require.Zero(t, resp)
}

// jsonrpchttp's Sender decodes with encoding/json v1, implemented over v2 in
// Go 1.27, so the checks apply there too.
func TestResponseUnmarshalJSONFromUnderJSONV1(t *testing.T) {
	var resp Response
	err := jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1}`), &resp)
	require.ErrorContains(t, err, "neither result nor error")

	require.NoError(t, jsonv1.Unmarshal([]byte(`{"jsonrpc":"2.0","result":1,"id":1}`), &resp))
	require.JSONEq(t, "1", string(resp.result))
}

func TestResponseUnmarshalJSONFromRoundTrip(t *testing.T) {
	s := newTestServer(t)

	out, err := s.ServeMessage(context.Background(),
		[]byte(`{"jsonrpc":"2.0","method":"add","params":{"a":1,"b":2},"id":"abc"}`))
	require.NoError(t, err)

	resp := decodeResponse(t, out)
	require.False(t, resp.isError())
	require.JSONEq(t, `"abc"`, resp.id.String())

	var got addResult
	require.NoError(t, resp.DecodeResult(&got))
	require.Equal(t, addResult{Sum: 3}, got)
}

func TestDecodeResponsesRejectsBadElement(t *testing.T) {
	_, err := DecodeResponses([]byte(`[{"jsonrpc":"2.0","result":1,"id":1},{"jsonrpc":"2.0","id":2}]`))
	require.ErrorContains(t, err, "element 1")

	// A single response is not a batch.
	_, err = DecodeResponses([]byte(`{"jsonrpc":"2.0","result":1,"id":1}`))
	require.Error(t, err)
}

// An error object whose members are all zero is still an error response: the
// decoder tracks the member's presence, not the Error's value.
func TestResponseUnmarshalZeroError(t *testing.T) {
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(`{"jsonrpc":"2.0","error":{"code":0,"message":""},"id":1}`), &resp))
	e, ok := resp.Error()
	require.True(t, ok)
	require.Equal(t, 0, e.Code())

	err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","result":1,"error":{"code":0,"message":""},"id":1}`), &resp)
	require.ErrorContains(t, err, "both result and error")
}

// Result hands out a copy, so a caller cannot alter the bytes a Response holds.
func TestResponseResultDoesNotAlias(t *testing.T) {
	resp := NewSuccessResponse(jsontext.Value(`[1]`), IntID(1))
	resp.Result()[0] = 'x'
	require.Equal(t, `[1]`, string(resp.Result()))
}

func TestNewSuccessResponseCopiesResult(t *testing.T) {
	b := jsontext.Value(`[1]`)
	resp := NewSuccessResponse(b, IntID(1))
	b[1] = '9'
	require.Equal(t, `[1]`, string(resp.Result()))
}
