package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

const Version = "2.0"

// Request is a JSON-RPC 2.0 request, or a notification when len(ID) == 0.
type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  jsontext.Value `json:"params,omitzero"`
	ID      jsontext.Value `json:"id,omitzero"`
}

var _ json.UnmarshalerFrom = (*Request)(nil)

func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// UnmarshalJSONFrom decodes one request object token by token, so every
// rejection carries this package's message: an unknown envelope member, a
// non-string method, non-structured params (null included), or a duplicate
// member name anywhere is Invalid Request; malformed input is a Parse error.
// Params content is not otherwise validated. A missing method or wrong
// version is Serve's verdict, so an id read before the failure survives.
//
// The receiver is reset first because json/v2 does not zero the destination.
func (req *Request) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	*req = Request{}

	tok, err := d.ReadToken()
	if err != nil {
		return tokenError(err)
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return NewError(CodeInvalidRequest, "request must be a JSON object")
	}

	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return tokenError(err)
		}
		// Token.String allocates; the Token itself dies at the next read.
		switch name := tok.String(); name {
		case "jsonrpc":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return NewError(CodeInvalidRequest, "jsonrpc must be a string")
			}
			// Serve judges the version; failing here would discard the id.
			req.JSONRPC = tok.String()

		case "method":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return NewError(CodeInvalidRequest, "method must be a string")
			}
			req.Method = tok.String()

		case "params":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			switch val.Kind() {
			case jsontext.KindBeginObject, jsontext.KindBeginArray:
				// ReadValue's buffer is only valid until the next read.
				req.Params = jsontext.Value(val.Clone())
			default:
				// null is present but unstructured; omit params to send none.
				return NewError(CodeInvalidRequest, "params must be an object or array")
			}

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			req.ID = jsontext.Value(val.Clone())

		default:
			return NewError(CodeInvalidRequest, "unknown member: "+name)
		}
	}

	// Consume the closing brace.
	if _, err := d.ReadToken(); err != nil {
		return tokenError(err)
	}
	return nil
}

// tokenError: a duplicate name is Invalid Request, anything else a Parse error.
func tokenError(err error) *Error {
	if errors.Is(err, jsontext.ErrDuplicateName) {
		return NewError(CodeInvalidRequest, err.Error())
	}
	return NewError(CodeParseError, err.Error())
}

// NewRequest assembles a Request; see NewParams and NewID.
func NewRequest(method string, params, id jsontext.Value) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params, ID: id}
}

// NewNotification assembles a Request without an id.
func NewNotification(method string, params jsontext.Value) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params}
}

// NewID returns the JSON encoding of v for Request.ID.
func NewID[T ~string | ~int | ~int64 | ~uint64](v T) jsontext.Value {
	b, _ := json.Marshal(v)
	return b
}

// NewParams marshals v for Request.Params under opts. A nil v returns nil; a
// jsontext.Value passes through unmarshaled, opts included.
//
// The result must be structured, as §4.2 requires: a scalar, a null (a typed
// nil marshals to one), or an empty value is an error here rather than an
// Invalid Request from the far end. Only the first token is read, so a
// passed-through value is checked for shape, not validity.
func NewParams(v any, opts ...json.Options) (jsontext.Value, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.(jsontext.Value)
	if !ok {
		b, err := json.Marshal(v, opts...)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	switch raw.Kind() {
	case '{', '[':
		return raw, nil
	}
	return nil, fmt.Errorf("jsonrpc: params must be a JSON object or array, got %s", raw.Kind())
}
