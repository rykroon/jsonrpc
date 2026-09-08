package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Response is a JSON-RPC 2.0 response: exactly one of Result and Error is set,
// and ID is always present, JSON null when the request id could not be read.
// MarshalJSONTo and UnmarshalJSONFrom walk the object themselves, so a shape
// the spec forbids never crosses the wire; the struct tags only record names.
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

// NewSuccessResponse builds a Response; an empty result or id becomes null.
func NewSuccessResponse(result, id jsontext.Value) *Response {
	if len(result) == 0 {
		result = jsontext.Value("null")
	}
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &Response{JSONRPC: Version, Result: result, ID: id}
}

// NewErrorResponse builds a Response; an empty id becomes null.
func NewErrorResponse(err *Error, id jsontext.Value) *Response {
	if len(id) == 0 {
		id = jsontext.Value("null")
	}
	return &Response{JSONRPC: Version, Error: err, ID: id}
}

// Decode unmarshals r.Result into into under opts. Check r.Error first.
func (r Response) Decode(into any, opts ...json.Options) error {
	if into == nil || len(r.Result) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result, into, opts...)
}

// MarshalJSONTo writes the canonical object — Version, result or error, id —
// and reports a response holding both members or neither instead of sending
// it. A marshaler for *Response in a Server's options overrides this.
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
		// A struct, so MarshalEncode takes it mid-stream with enc's options.
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

// UnmarshalJSONFrom decodes one response object, checking the version, the
// presence of an id, and that exactly one of result and error is set.
// Undefined members are skipped; duplicates are rejected. A failed check
// leaves the receiver zeroed. Use DecodeResponses for a batch reply.
func (r *Response) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	// json/v2 does not zero the destination.
	*r = Response{}

	tok, err := d.ReadToken()
	if err != nil {
		return decodeResponseError(err)
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return errors.New("jsonrpc: response must be a JSON object")
	}

	// The receiver is replaced only once the checks pass.
	var resp Response
	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return decodeResponseError(err)
		}
		// Token.String allocates; the Token itself dies at the next read.
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
			// ReadValue's buffer dies at the next read. Raw also keeps a present
			// "result":null distinct from an absent member.
			resp.Result = jsontext.Value(val.Clone())

		case "error":
			// A struct, so UnmarshalDecode takes it mid-stream with d's options.
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

	// Consume the closing brace.
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

// decodeResponseError stays a plain error: a malformed reply has no one to
// report a spec code to.
func decodeResponseError(err error) error {
	return fmt.Errorf("jsonrpc: decode response: %w", err)
}

// DecodeResponses parses a batch reply element by element, giving each
// Response.UnmarshalJSONFrom's checks.
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
