package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Response is an immutable JSON-RPC 2.0 response: exactly one of result and
// error is set, and the id is always present (null if the request's could not
// be read). Build one with NewSuccessResponse or NewErrorResponse. The zero
// Response means no response, as Serve returns for a notification.
type Response struct {
	jsonrpc string
	// Never empty on a success response, so an empty result marks an error.
	result jsontext.Value
	error  Error
	id     ID
}

func (r Response) JSONRPC() string { return r.jsonrpc }

// Result returns a copy of the raw result, nil on an error response.
func (r Response) Result() jsontext.Value { return r.result.Clone() }

// Error returns the error and true on an error response.
func (r Response) Error() (Error, bool) {
	if !r.isError() {
		return Error{}, false
	}
	return r.error, true
}

// isError reports whether r is an error response.
func (r Response) isError() bool { return len(r.result) == 0 && !r.IsZero() }

func (r Response) ID() ID { return r.id }

// IsZero reports whether r is the zero Response, meaning no response.
func (r Response) IsZero() bool { return r.id.IsZero() }

var (
	_ json.MarshalerTo     = Response{}
	_ json.UnmarshalerFrom = (*Response)(nil)
)

// NewSuccessResponse builds a Response; an empty result or zero id becomes
// null. The result is copied, so later changes to the caller's bytes do not
// reach the Response.
func NewSuccessResponse(result jsontext.Value, id ID) Response {
	if len(result) == 0 {
		result = jsontext.Value("null")
	} else {
		result = result.Clone()
	}
	if id.IsZero() {
		id = nullID()
	}
	return Response{jsonrpc: Version, result: result, id: id}
}

// NewErrorResponse builds a Response; a zero id becomes null.
func NewErrorResponse(err Error, id ID) Response {
	if id.IsZero() {
		id = nullID()
	}
	return Response{jsonrpc: Version, error: err, id: id}
}

// DecodeResult unmarshals the result into into under opts. Check Error first;
// an error response has no result and leaves into untouched.
func (r Response) DecodeResult(into any, opts ...json.Options) error {
	if into == nil || len(r.result) == 0 {
		return nil
	}
	return json.Unmarshal(r.result, into, opts...)
}

// MarshalJSONTo writes the canonical object — Version, result or error, id —
// and reports the zero Response, which holds neither member, instead of
// sending it.
func (r Response) MarshalJSONTo(enc *jsontext.Encoder) error {
	if len(r.result) == 0 && r.IsZero() {
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
	if len(r.result) == 0 {
		if err := enc.WriteToken(jsontext.String("error")); err != nil {
			return err
		}
		// MarshalEncode takes it mid-stream with enc's options.
		if err := json.MarshalEncode(enc, r.error); err != nil {
			return err
		}
	} else {
		if err := enc.WriteToken(jsontext.String("result")); err != nil {
			return err
		}
		if err := enc.WriteValue(r.result); err != nil {
			return err
		}
	}
	if err := enc.WriteToken(jsontext.String("id")); err != nil {
		return err
	}
	id := r.id
	if id.IsZero() {
		id = nullID()
	}
	if err := enc.WriteValue(jsontext.Value(id.raw)); err != nil {
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
	// Error's zero value is a valid error object, so presence is tracked here.
	var hasError bool
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
			resp.jsonrpc = tok.String()

		case "result":
			val, err := d.ReadValue()
			if err != nil {
				return decodeResponseError(err)
			}
			// ReadValue's buffer dies at the next read. Raw also keeps a present
			// "result":null distinct from an absent member.
			resp.result = jsontext.Value(val.Clone())

		case "error":
			// UnmarshalDecode takes it mid-stream with d's options.
			if err := json.UnmarshalDecode(d, &resp.error); err != nil {
				return decodeResponseError(err)
			}
			hasError = true

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return decodeResponseError(err)
			}
			id, err := idFromValue(val)
			if err != nil {
				return decodeResponseError(err)
			}
			resp.id = id

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

	if resp.jsonrpc != Version {
		return fmt.Errorf("jsonrpc: response jsonrpc must be %q, got %q", Version, resp.jsonrpc)
	}
	if resp.id.IsZero() {
		return errors.New("jsonrpc: response has no id")
	}
	switch {
	case resp.result != nil && hasError:
		return errors.New("jsonrpc: response has both result and error")
	case resp.result == nil && !hasError:
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
func DecodeResponses(data jsontext.Value) ([]Response, error) {
	var elems []jsontext.Value
	if err := json.Unmarshal(data, &elems); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode batch response: %w", err)
	}
	responses := make([]Response, 0, len(elems))
	for i, elem := range elems {
		var resp Response
		if err := json.Unmarshal(elem, &resp); err != nil {
			return nil, fmt.Errorf("jsonrpc: batch response element %d: %w", i, err)
		}
		responses = append(responses, resp)
	}
	return responses, nil
}
