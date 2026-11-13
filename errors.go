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

// The spec's predefined errors, with its messages. Return one from a handler
// with Err, and recognize one with FromError and Code:
//
//	if e, ok := jsonrpc.FromError(err); ok && e.Code() == jsonrpc.CodeMethodNotFound {
//		// the server has no such method
//	}
var (
	ErrParse          = NewError(CodeParseError, "Parse error")
	ErrInvalidRequest = NewError(CodeInvalidRequest, "Invalid Request")
	ErrMethodNotFound = NewError(CodeMethodNotFound, "Method not found")
	ErrInvalidParams  = NewError(CodeInvalidParams, "Invalid params")
	ErrInternal       = NewError(CodeInternalError, "Internal error")
)

// Error is the JSON-RPC error object. It is immutable data, like Request and
// Response, and is comparable with ==. It does not implement error: Err wraps it
// as one, and FromError recovers it, as gRPC's status package does. Build one
// with NewError and WithData, and read it through its accessors.
type Error struct {
	code    int
	message string
	// data is the optional raw JSON, "" when absent. A string keeps Error
	// comparable and its bytes unshared.
	data string
}

func NewError(code int, message string) Error {
	return Error{code: code, message: message}
}

// Code is the error code.
func (e Error) Code() int { return e.code }

// Message is the short description of the error.
func (e Error) Message() string { return e.message }

// Data returns a copy of the raw data, nil when absent.
func (e Error) Data() jsontext.Value {
	if e.data == "" {
		return nil
	}
	return jsontext.Value(e.data)
}

// WithData returns a copy of e carrying data, marshaled to JSON under opts.
func (e Error) WithData(data any, opts ...json.Options) (Error, error) {
	b, err := json.Marshal(data, opts...)
	if err != nil {
		return Error{}, err
	}
	e.data = string(b)
	return e, nil
}

// DecodeData unmarshals the data into into under opts. Absent data leaves into
// untouched.
func (e Error) DecodeData(into any, opts ...json.Options) error {
	if e.data == "" {
		return nil
	}
	return json.Unmarshal([]byte(e.data), into, opts...)
}

// Err returns e as an error, for returning from a Handler, RawHandler, or
// Middleware. FromError recovers e from it.
func (e Error) Err() error {
	return &rpcError{e}
}

// FromError reports the Error carried by err, looking through %w wrapping.
// It returns false for nil and for errors that carry no Error.
func FromError(err error) (Error, bool) {
	if r, ok := errors.AsType[*rpcError](err); ok {
		return r.e, true
	}
	return Error{}, false
}

// rpcError carries an Error as an error. It is unexported so callers match it
// only through FromError.
type rpcError struct {
	e Error
}

func (r *rpcError) Error() string {
	return fmt.Sprintf("jsonrpc: %d %s", r.e.code, r.e.message)
}

// Is reports whether target carries an Error with the same code, so
// errors.Is(err, jsonrpc.ErrMethodNotFound.Err()) matches whatever the message
// or data.
func (r *rpcError) Is(target error) bool {
	t, ok := target.(*rpcError)
	return ok && t.e.code == r.e.code
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
	if e.data != "" {
		if err := enc.WriteToken(jsontext.String("data")); err != nil {
			return err
		}
		if err := enc.WriteValue(jsontext.Value(e.data)); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// UnmarshalJSONFrom decodes one error object, which must have an integer code
// and a string message. Undefined members are skipped; duplicates are rejected. A failed
// decode leaves the receiver zeroed.
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
	var hasCode, hasMessage bool
	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return err
		}
		switch tok.String() {
		case "code":
			// null would decode as 0 without complaint.
			if d.PeekKind() != jsontext.KindNumber {
				return errors.New("jsonrpc: error code must be an integer")
			}
			// An int, so the decoder rejects fractions and overflow.
			if err := json.UnmarshalDecode(d, &out.code); err != nil {
				return err
			}
			hasCode = true
		case "message":
			if d.PeekKind() != jsontext.KindString {
				return errors.New("jsonrpc: error message must be a string")
			}
			if err := json.UnmarshalDecode(d, &out.message); err != nil {
				return err
			}
			hasMessage = true
		case "data":
			val, err := d.ReadValue()
			if err != nil {
				return err
			}
			// The conversion copies ReadValue's buffer, which dies at the next read.
			out.data = string(val)
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
	if !hasCode || !hasMessage {
		return errors.New("jsonrpc: error must have a code and a message")
	}
	*e = out
	return nil
}
