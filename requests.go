package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
)

const Version = "2.0"

// Request is a JSON-RPC 2.0 request, or a notification when ID is zero.
type Request struct {
	jsonrpc string
	method  string
	params  Params
	id      ID
}

func (req Request) JSONRPC() string {
	return req.jsonrpc
}

func (req Request) Method() string {
	return req.method
}

func (req Request) Params() Params {
	return req.params
}

func (req Request) ID() ID {
	return req.id
}

var (
	_ json.MarshalerTo     = Request{}
	_ json.UnmarshalerFrom = (*Request)(nil)
)

// MarshalJSONTo writes the request, omitting zero params and id.
func (req Request) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("jsonrpc")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(req.JSONRPC())); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("method")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(req.Method())); err != nil {
		return err
	}
	if !req.Params().IsZero() {
		if err := enc.WriteToken(jsontext.String("params")); err != nil {
			return err
		}
		if err := req.Params().MarshalJSONTo(enc); err != nil {
			return err
		}
	}
	if !req.ID().IsZero() {
		if err := enc.WriteToken(jsontext.String("id")); err != nil {
			return err
		}
		if err := req.ID().MarshalJSONTo(enc); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

func (r Request) IsNotification() bool {
	return r.id.IsZero()
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
			req.jsonrpc = tok.String()

		case "method":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return NewError(CodeInvalidRequest, "method must be a string")
			}
			req.method = tok.String()

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
			req.params = p

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			id, err := idFromValue(val)
			if err != nil {
				return NewError(CodeInvalidRequest, "id must be a string, number, or null")
			}
			req.id = id

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
func NewRequest(method string, params Params, id ID) Request {
	return Request{jsonrpc: Version, method: method, params: params, id: id}
}

// NewNotification assembles a Request without an id.
func NewNotification(method string, params Params) Request {
	return Request{jsonrpc: Version, method: method, params: params}
}
