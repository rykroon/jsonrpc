package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
)

const Version = "2.0"

// Request is a JSON-RPC 2.0 request, or a notification when ID is zero.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  Params `json:"params,omitzero"`
	ID      ID     `json:"id,omitzero"`
}

var _ json.UnmarshalerFrom = (*Request)(nil)

func (r *Request) IsNotification() bool {
	return r.ID.IsZero()
}

// UnmarshalJSONFrom decodes one request object token by token, so every
// rejection carries this package's message: an unknown envelope member, a
// non-string method, non-structured params (null included), an id that is not
// a string, number, or null, or a duplicate member name anywhere is Invalid
// Request; malformed input is a Parse error.
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
			p, err := paramsFromValue(val)
			if err != nil {
				// null is present but unstructured; omit params to send none.
				return NewError(CodeInvalidRequest, "params must be an object or array")
			}
			req.Params = p

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			id, err := idFromValue(val)
			if err != nil {
				return NewError(CodeInvalidRequest, "id must be a string, number, or null")
			}
			req.ID = id

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
func tokenError(err error) Error {
	if errors.Is(err, jsontext.ErrDuplicateName) {
		return NewError(CodeInvalidRequest, err.Error())
	}
	return NewError(CodeParseError, err.Error())
}

// NewRequest assembles a Request; see NewParams, StringID, and IntID.
func NewRequest(method string, params Params, id ID) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params, ID: id}
}

// NewNotification assembles a Request without an id.
func NewNotification(method string, params Params) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params}
}
