package jsonrpc

import (
	"bytes"
	"errors"
	"strconv"
)

type value []byte

// Clone returns a copy of v.
func (v value) Clone() value {
	return bytes.Clone(v)
}

// String returns the string formatting of v.
func (v value) String() string {
	if v == nil {
		return "null"
	}
	return string(v)
}

// MarshalJSON returns v as the JSON encoding of v.
// It returns the stored value as the raw JSON output without any validation.
// If v is nil, then this returns a JSON null.
func (v value) MarshalJSON() ([]byte, error) {
	// NOTE: This matches the behavior of v1 json.RawMessage.MarshalJSON.
	if v == nil {
		return []byte("null"), nil
	}
	return v, nil
}

// UnmarshalJSON sets v as the JSON encoding of b.
// It stores a copy of the provided raw JSON input without any validation.
func (v *value) UnmarshalJSON(b []byte) error {
	// NOTE: This matches the behavior of v1 json.RawMessage.UnmarshalJSON.
	if v == nil {
		return errors.New("jsontext.Value: UnmarshalJSON on nil pointer")
	}
	*v = append((*v)[:0], b...)
	return nil
}

// Kind returns the starting token kind.
// For a valid value, this will never include '}' or ']'.
func (v value) Kind() Kind {
	if v := v[consumeWhitespace(v):]; len(v) > 0 {
		return Kind(v[0]).normalize()
	}
	return invalidKind
}

// ConsumeWhitespace consumes leading JSON whitespace per RFC 7159, section 2.
func consumeWhitespace(b []byte) (n int) {
	// NOTE: The arguments and logic are kept simple to keep this inlinable.
	for len(b) > n && (b[n] == ' ' || b[n] == '\t' || b[n] == '\r' || b[n] == '\n') {
		n++
	}
	return n
}

// Kind represents each possible JSON token kind with a single byte,
// which is conveniently the first byte of that kind's grammar
// with the restriction that numbers always be represented with '0':
//
//   - 'n': null
//   - 'f': false
//   - 't': true
//   - '"': string
//   - '0': number
//   - '{': object begin
//   - '}': object end
//   - '[': array begin
//   - ']': array end
//
// An invalid kind is usually represented using 0,
// but may be non-zero due to invalid JSON data.
type Kind byte

const invalidKind Kind = 0

// String prints the kind in a humanly readable fashion.
func (k Kind) String() string {
	switch k {
	case 'n':
		return "null"
	case 'f':
		return "false"
	case 't':
		return "true"
	case '"':
		return "string"
	case '0':
		return "number"
	case '{':
		return "object"
	case '}':
		return "}"
	case '[':
		return "array"
	case ']':
		return "]"
	default:
		return "<invalid jsontext.Kind: " + strconv.QuoteRune(rune(k)) + ">"
	}
}

// normalize coalesces all possible starting characters of a number as just '0'.
func (k Kind) normalize() Kind {
	if k == '-' || ('0' <= k && k <= '9') {
		return '0'
	}
	return k
}
