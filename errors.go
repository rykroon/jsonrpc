package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// ErrInvalidResponse reports a reply that is valid JSON but not a valid
// response to the call: not a response object (no id, both result and error,
// a wrong version, ...), no response at all, or one whose id is not the
// request's. Malformed JSON is reported as a *jsontext.SyntacticError instead.
// Match it with errors.Is.
var ErrInvalidResponse = errors.New("jsonrpc: invalid response")

// invalidResponse wraps ErrInvalidResponse with a detail.
func invalidResponse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidResponse}, args...)...)
}

// isSyntaxError reports malformed JSON. A duplicate member name is a
// SyntacticError to jsontext but valid JSON grammar, so it is not one.
func isSyntaxError(err error) bool {
	_, ok := errors.AsType[*jsontext.SyntacticError](err)
	return ok && !errors.Is(err, jsontext.ErrDuplicateName)
}

// The spec's predefined error codes. Recognize one with FromError and Code:
//
//	if e, ok := jsonrpc.FromError(err); ok && e.Code() == jsonrpc.CodeMethodNotFound {
//		// the server has no such method
//	}
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

// Error is the JSON-RPC error object. It is immutable data, like Request and
// Response. It does not implement error: Err wraps it as one, and FromError
// recovers it, as gRPC's status package does. Build one with NewError and
// WithData, and read it through its accessors. Compare errors by Code, or with
// errors.Is on their Err; Error is not comparable with ==.
type Error struct {
	code    int
	message string
	data    jsontext.Value // raw JSON, nil when absent
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
	return e.data.Clone()
}

// WithData returns a copy of e carrying data, marshaled to JSON under opts.
func (e Error) WithData(data any, opts ...json.Options) (Error, error) {
	b, err := json.Marshal(data, opts...)
	if err != nil {
		return Error{}, err
	}
	e.data = b
	return e, nil
}

// DecodeData unmarshals the data into into under opts. Absent data leaves into
// untouched.
func (e Error) DecodeData(into any, opts ...json.Options) error {
	if len(e.data) == 0 {
		return nil
	}
	return json.Unmarshal(e.data, into, opts...)
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
// errors.Is(err, errNotFound.Err()) matches whatever the message or data.
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
			// Copy ReadValue's buffer, which the next read reuses.
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
	if !hasCode || !hasMessage {
		return errors.New("jsonrpc: error must have a code and a message")
	}
	*e = out
	return nil
}
