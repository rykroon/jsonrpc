package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Response is a JSON-RPC 2.0 response: either a *SuccessResponse carrying a
// result or an *ErrorResponse carrying an error object. The spec allows no
// third shape and forbids both members at once, so the two concrete types are
// the whole set; the interface exists so Server.Serve and Sender can return
// either one.
//
// The concrete types keep their fields unexported, so a Response can only come
// from NewSuccessResponse, NewErrorResponse, or DecodeResponse. That makes the
// invariants — a result member on every success, an id member on both — hold
// by construction rather than by convention.
type Response interface {
	// Result returns the result member, or nil on an error response.
	Result() jsontext.Value
	// Error returns the error object, or nil on a success response.
	Error() *Error
	// ID returns the id member. It is JSON null when the request id could not
	// be determined.
	ID() jsontext.Value
	IsSuccess() bool
	IsError() bool
	// Decode unmarshals the result into the given target. It reports an error
	// on an error response, so check IsError (or Error) first.
	Decode(any) error
}

var (
	_ Response         = (*SuccessResponse)(nil)
	_ Response         = (*ErrorResponse)(nil)
	_ json.MarshalerTo = (*SuccessResponse)(nil)
	_ json.MarshalerTo = (*ErrorResponse)(nil)
)

// SuccessResponse is a response carrying a result.
type SuccessResponse struct {
	result jsontext.Value
	id     jsontext.Value
}

// NewSuccessResponse assembles a SuccessResponse. An empty result or id
// becomes JSON null, since the spec requires both members to be present and
// the encoder rejects an empty raw value.
func NewSuccessResponse(result, id jsontext.Value) *SuccessResponse {
	if len(result) == 0 {
		result = jsontext.Value("null")
	}
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &SuccessResponse{result: result, id: id}
}

func (r *SuccessResponse) Result() jsontext.Value { return r.result }

func (r *SuccessResponse) Error() *Error { return nil }

func (r *SuccessResponse) ID() jsontext.Value { return r.id }

func (r *SuccessResponse) IsSuccess() bool { return true }

func (r *SuccessResponse) IsError() bool { return false }

// Decode unmarshals r's result into into, doing nothing when into is nil.
func (r *SuccessResponse) Decode(into any) error {
	if into == nil || len(r.result) == 0 {
		return nil
	}
	return json.Unmarshal(r.result, into)
}

// MarshalJSONTo writes the response directly to enc. The members are written
// as tokens rather than through a struct because the shape is fixed by the
// spec and the fields are unexported; WriteValue also rejects an empty raw
// value, so a malformed response fails here instead of reaching the wire.
func (r *SuccessResponse) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("jsonrpc")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(Version)); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("result")); err != nil {
		return err
	}
	if err := enc.WriteValue(r.result); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("id")); err != nil {
		return err
	}
	if err := enc.WriteValue(r.id); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

// ErrorResponse is a response carrying an error object instead of a result.
type ErrorResponse struct {
	err *Error
	id  jsontext.Value
}

// NewErrorResponse assembles an ErrorResponse. An empty id becomes JSON null,
// which is what the spec requires when the request id could not be read.
func NewErrorResponse(err *Error, id jsontext.Value) *ErrorResponse {
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &ErrorResponse{err: err, id: id}
}

func (r *ErrorResponse) Result() jsontext.Value { return nil }

func (r *ErrorResponse) Error() *Error { return r.err }

func (r *ErrorResponse) ID() jsontext.Value { return r.id }

func (r *ErrorResponse) IsSuccess() bool { return false }

func (r *ErrorResponse) IsError() bool { return true }

// Decode always fails: an error response carries no result. Check IsError
// before decoding.
func (r *ErrorResponse) Decode(any) error {
	return errors.New("jsonrpc: error response has no result to decode")
}

// MarshalJSONTo writes the response directly to enc. The error object is a
// struct, so it is handed to MarshalEncode mid-stream.
func (r *ErrorResponse) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("jsonrpc")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(Version)); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("error")); err != nil {
		return err
	}
	if err := json.MarshalEncode(enc, r.err); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("id")); err != nil {
		return err
	}
	if err := enc.WriteValue(r.id); err != nil {
		return err
	}
	return enc.WriteToken(jsontext.EndObject)
}

// DecodeResponse parses one response object, returning a *SuccessResponse or
// an *ErrorResponse depending on which member is present. It is the client-side
// counterpart to DecodeRequest and the seam every transport needs, because a
// Response is an interface and so cannot be unmarshaled into directly.
//
// Exactly one of result and error must be present — the spec's core invariant,
// and the only way to tell the two shapes apart. Members the spec does not
// define are ignored, so a server that decorates its responses still
// interoperates. Duplicate member names are rejected.
//
// Use DecodeResponses for a batch reply.
func DecodeResponse(data jsontext.Value) (Response, error) {
	// Result is a jsontext.Value rather than a pointer so that a present
	// "result":null (a legal success response) is distinguishable from an
	// absent member: the former decodes to the four bytes "null", the latter
	// leaves the field nil.
	var raw struct {
		JSONRPC string         `json:"jsonrpc"`
		Result  jsontext.Value `json:"result"`
		Error   *Error         `json:"error"`
		ID      jsontext.Value `json:"id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode response: %w", err)
	}
	if raw.JSONRPC != Version {
		return nil, fmt.Errorf("jsonrpc: response jsonrpc must be %q, got %q", Version, raw.JSONRPC)
	}
	if len(raw.ID) == 0 {
		return nil, errors.New("jsonrpc: response has no id")
	}
	switch {
	case raw.Result != nil && raw.Error != nil:
		return nil, errors.New("jsonrpc: response has both result and error")
	case raw.Error != nil:
		return &ErrorResponse{err: raw.Error, id: raw.ID}, nil
	case raw.Result != nil:
		return &SuccessResponse{result: raw.Result, id: raw.ID}, nil
	default:
		return nil, errors.New("jsonrpc: response has neither result nor error")
	}
}

// DecodeResponses parses a batch reply — a JSON array of response objects —
// element by element. A response to a single request is not an array; use
// DecodeResponse for that.
func DecodeResponses(data jsontext.Value) ([]Response, error) {
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode batch response: %w", err)
	}
	responses := make([]Response, 0, len(elems))
	for i, elem := range elems {
		resp, err := DecodeResponse(elem)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: batch response element %d: %w", i, err)
		}
		responses = append(responses, resp)
	}
	return responses, nil
}
