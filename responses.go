package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Response is a JSON-RPC 2.0 response; exactly one of Result and Error is set.
// ID is always present, and is JSON null when the request id could not be
// determined. Result stays raw because the spec leaves its type open; decode it
// with Decode.
//
// A Response writes itself with MarshalJSONTo, so the spec's invariants are
// enforced where they matter — a malformed response fails to marshal rather
// than reaching the wire. The struct tags describe the same shape for the
// decode side.
type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	Result  jsontext.Value `json:"result,omitzero"`
	Error   *Error         `json:"error,omitzero"`
	ID      jsontext.Value `json:"id"`
}

var _ json.MarshalerTo = Response{}

// NewSuccessResponse assembles a Response carrying a result. An empty result or
// id becomes JSON null, since the spec requires both members.
func NewSuccessResponse(result, id jsontext.Value) *Response {
	if len(result) == 0 {
		result = jsontext.Value("null")
	}
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &Response{JSONRPC: Version, Result: result, ID: id}
}

// NewErrorResponse assembles a Response carrying an error object. An empty id
// becomes JSON null, as the spec requires when the request id could not be read.
func NewErrorResponse(err *Error, id jsontext.Value) *Response {
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &Response{JSONRPC: Version, Error: err, ID: id}
}

// Decode unmarshals r.Result into into, doing nothing when into is nil or
// Result is empty. Check r.Error first.
func (r Response) Decode(into any) error {
	if into == nil || len(r.Result) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result, into)
}

// MarshalJSONTo writes the response in canonical form via EncodeResponse. It is
// a Response's own wire form, used by both json packages; a Server writes
// responses with its ResponseEncoder instead.
func (r Response) MarshalJSONTo(enc *jsontext.Encoder) error {
	return EncodeResponse(enc, &r)
}

// EncodeResponse is the package's default ResponseEncoder. It writes the
// response as tokens rather than through the struct tags, so the shape is the
// spec's regardless of what the fields hold: the version is always Version, the
// members are ordered, and an empty id is written as JSON null.
//
// Exactly one of Result and Error must be set — a response with both or with
// neither is a bug, and is reported here instead of being sent. WriteValue
// likewise rejects a malformed raw result.
func EncodeResponse(enc *jsontext.Encoder, r *Response) error {
	switch {
	case r.Error != nil && len(r.Result) > 0:
		return errors.New("jsonrpc: response has both result and error")
	case r.Error == nil && len(r.Result) == 0:
		return errors.New("jsonrpc: response has neither result nor error")
	}

	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("jsonrpc")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(Version)); err != nil {
		return err
	}
	if r.Error != nil {
		if err := enc.WriteToken(jsontext.String("error")); err != nil {
			return err
		}
		// The error object is a struct, so it is handed to MarshalEncode
		// mid-stream, inheriting whatever options enc carries.
		if err := json.MarshalEncode(enc, r.Error); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.String("result")); err != nil {
			return err
		}
		if err := enc.WriteValue(r.Result); err != nil {
			return err
		}
	}
	if err := enc.WriteToken(jsontext.String("id")); err != nil {
		return err
	}
	id := r.ID
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	if err := enc.WriteValue(id); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

// DecodeResponse parses one response object, checking the invariants a Response
// built by this package holds: the version is "2.0", an id member is present,
// and exactly one of result and error is set.
//
// Undefined members are ignored, so a server that decorates its responses still
// interoperates; duplicate member names are rejected.
//
// Use DecodeResponses for a batch reply.
func DecodeResponse(data jsontext.Value) (*Response, error) {
	// Result holds raw JSON, so a present "result":null (a legal success
	// response) stays distinguishable from an absent member: the former
	// decodes to four bytes, the latter to nil.
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode response: %w", err)
	}
	if resp.JSONRPC != Version {
		return nil, fmt.Errorf("jsonrpc: response jsonrpc must be %q, got %q", Version, resp.JSONRPC)
	}
	if len(resp.ID) == 0 {
		return nil, errors.New("jsonrpc: response has no id")
	}
	switch {
	case resp.Result != nil && resp.Error != nil:
		return nil, errors.New("jsonrpc: response has both result and error")
	case resp.Result == nil && resp.Error == nil:
		return nil, errors.New("jsonrpc: response has neither result nor error")
	}
	return &resp, nil
}

// DecodeResponses parses a batch reply — a JSON array of response objects —
// element by element. Use DecodeResponse for a reply to a single request.
func DecodeResponses(data jsontext.Value) ([]*Response, error) {
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode batch response: %w", err)
	}
	responses := make([]*Response, 0, len(elems))
	for i, elem := range elems {
		resp, err := DecodeResponse(elem)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: batch response element %d: %w", i, err)
		}
		responses = append(responses, resp)
	}
	return responses, nil
}
