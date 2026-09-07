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
// MarshalJSONTo writes it and UnmarshalJSONFrom reads it, so a response the
// spec forbids never reaches the wire. Both walk the object themselves; the
// struct tags only record the wire names.
type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	Result  jsontext.Value `json:"result,omitzero"`
	Error   *Error         `json:"error,omitzero"`
	ID      jsontext.Value `json:"id"`
}

var (
	_ json.MarshalerTo     = Response{}
	_ json.UnmarshalerFrom = (*Response)(nil)
)

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

// MarshalJSONTo writes the response as tokens rather than through the struct
// tags, so the shape is the spec's regardless of what the fields hold: the
// version is always Version, the members are ordered, and an empty id is
// written as JSON null. A Server's ResponseEncoder overrides this, and can
// delegate here.
//
// Exactly one of Result and Error must be set — a response with both or with
// neither is a bug, and is reported here instead of being sent. WriteValue
// likewise rejects a malformed raw result.
func (r Response) MarshalJSONTo(enc *jsontext.Encoder) error {
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

// UnmarshalJSONFrom decodes one response object and checks what the spec
// requires: the version is "2.0", an id is present, and exactly one of result
// and error is set. It applies to every json.Unmarshal of a Response, so a
// Sender gets the checks for free.
//
// Undefined members are skipped, so a decorated response still interoperates;
// duplicate member names are rejected. A failed check leaves the receiver
// zeroed, never holding a shape the spec forbids.
//
// Use DecodeResponses for a batch reply.
func (r *Response) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	// json/v2 does not zero the destination, so a member this message omits
	// must not survive from a previous one.
	*r = Response{}

	tok, err := d.ReadToken()
	if err != nil {
		return decodeResponseError(err)
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return errors.New("jsonrpc: response must be a JSON object")
	}

	// The local replaces the receiver only once the checks pass, so a rejected
	// message never leaves r holding both a result and an error.
	var resp Response
	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return decodeResponseError(err)
		}
		// Token.String allocates, so the name stays valid across the reads
		// below; the Token itself does not.
		switch name := tok.String(); name {
		case "jsonrpc":
			tok, err := d.ReadToken()
			if err != nil {
				return decodeResponseError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return errors.New("jsonrpc: response jsonrpc must be a string")
			}
			resp.JSONRPC = tok.String()

		case "result":
			val, err := d.ReadValue()
			if err != nil {
				return decodeResponseError(err)
			}
			// ReadValue's buffer dies at the next read. Raw also keeps a
			// present "result":null, a legal success, distinct from an absent
			// member: four bytes against nil.
			resp.Result = jsontext.Value(val.Clone())

		case "error":
			// A struct, so UnmarshalDecode takes it mid-stream, inheriting
			// whatever options d carries.
			if err := json.UnmarshalDecode(d, &resp.Error); err != nil {
				return decodeResponseError(err)
			}

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return decodeResponseError(err)
			}
			resp.ID = jsontext.Value(val.Clone())

		default:
			if err := d.SkipValue(); err != nil {
				return decodeResponseError(err)
			}
		}
	}

	// Consume the closing brace, completing the one value this decoder reads.
	if _, err := d.ReadToken(); err != nil {
		return decodeResponseError(err)
	}

	if resp.JSONRPC != Version {
		return fmt.Errorf("jsonrpc: response jsonrpc must be %q, got %q", Version, resp.JSONRPC)
	}
	if len(resp.ID) == 0 {
		return errors.New("jsonrpc: response has no id")
	}
	switch {
	case resp.Result != nil && resp.Error != nil:
		return errors.New("jsonrpc: response has both result and error")
	case resp.Result == nil && resp.Error == nil:
		return errors.New("jsonrpc: response has neither result nor error")
	}
	*r = resp
	return nil
}

// decodeResponseError wraps a decode failure. These stay plain errors, unlike
// the request side's: a malformed reply has no one to report a spec code to.
func decodeResponseError(err error) error {
	return fmt.Errorf("jsonrpc: decode response: %w", err)
}

// DecodeResponses parses a batch reply — a JSON array of response objects —
// element by element, so each gets Response.UnmarshalJSONFrom's checks. A reply
// to a single request is a plain json.Unmarshal into a Response.
func DecodeResponses(data jsontext.Value) ([]*Response, error) {
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode batch response: %w", err)
	}
	responses := make([]*Response, 0, len(elems))
	for i, elem := range elems {
		var resp *Response
		if err := json.Unmarshal(elem, &resp); err != nil {
			return nil, fmt.Errorf("jsonrpc: batch response element %d: %w", i, err)
		}
		responses = append(responses, resp)
	}
	return responses, nil
}
