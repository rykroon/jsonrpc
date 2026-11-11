package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603

	// CodeServerError and CodeServerErrorMin bound the server-defined range,
	// reserved for application errors. CodeServerError is the conventional
	// default.
	CodeServerError    = -32000 // first (highest) server-defined code
	CodeServerErrorMin = -32099 // last (lowest) server-defined code
)

// Error is the JSON-RPC error object. It is immutable: build one with NewError
// and WithData, and read it through its accessors. Its optional data is raw
// JSON; Data returns a copy, so an Error never shares bytes with a caller.
type Error struct {
	code    int
	message string
	data    jsontext.Value
}

var (
	_ json.MarshalerTo     = Error{}
	_ json.UnmarshalerFrom = (*Error)(nil)
)

// MarshalJSONTo writes the error object, omitting absent data.
func (e Error) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("code")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.Int(int64(e.code))); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String("message")); err != nil {
		return err
	}
	if err := enc.WriteToken(jsontext.String(e.message)); err != nil {
		return err
	}
	if len(e.data) > 0 {
		if err := enc.WriteToken(jsontext.String("data")); err != nil {
			return err
		}
		if err := enc.WriteValue(e.data); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// UnmarshalJSONFrom decodes one error object. Undefined members are skipped;
// duplicates are rejected. A failed decode leaves the receiver zeroed.
func (e *Error) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	// json/v2 does not zero the destination.
	*e = Error{}

	tok, err := d.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return errors.New("jsonrpc: error must be a JSON object")
	}

	// The receiver is replaced only once the object is read.
	var out Error
	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return err
		}
		switch tok.String() {
		case "code":
			// An int, so the decoder rejects fractions and overflow.
			if err := json.UnmarshalDecode(d, &out.code); err != nil {
				return err
			}
		case "message":
			if err := json.UnmarshalDecode(d, &out.message); err != nil {
				return err
			}
		case "data":
			val, err := d.ReadValue()
			if err != nil {
				return err
			}
			// ReadValue's buffer dies at the next read.
			out.data = val.Clone()
		default:
			if err := d.SkipValue(); err != nil {
				return err
			}
		}
	}

	// Consume the closing brace.
	if _, err := d.ReadToken(); err != nil {
		return err
	}
	*e = out
	return nil
}

func (e Error) Error() string {
	return fmt.Sprintf("jsonrpc: %d %s", e.code, e.message)
}

// Is reports whether target is an Error with the same code, so errors.Is
// matches a sentinel even after WithData, wrapping, or a round trip.
func (e Error) Is(target error) bool {
	t, ok := target.(Error)
	return ok && t.code == e.code
}

func NewError(code int, message string) Error {
	return Error{code: code, message: message}
}

// Code is the error code.
func (e Error) Code() int { return e.code }

// Message is the short description of the error.
func (e Error) Message() string { return e.message }

// WithData returns a copy of e carrying data, marshaled to JSON under opts.
func (e Error) WithData(data any, opts ...json.Options) (Error, error) {
	b, err := json.Marshal(data, opts...)
	if err != nil {
		return Error{}, err
	}
	e.data = b
	return e, nil
}

// Data returns a copy of the raw data, nil when absent.
func (e Error) Data() jsontext.Value {
	return e.data.Clone()
}

// DecodeData unmarshals the data into into under opts. Absent data leaves into
// untouched.
func (e Error) DecodeData(into any, opts ...json.Options) error {
	if len(e.data) == 0 {
		return nil
	}
	return json.Unmarshal(e.data, into, opts...)
}
