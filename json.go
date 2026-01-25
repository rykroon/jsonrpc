package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"iter"
	"strconv"
)

// copied code from experimental encoding/json/jsontext

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
func (v value) Kind() kind {
	if v := v[consumeWhitespace(v):]; len(v) > 0 {
		return kind(v[0]).normalize()
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
type kind byte

const invalidKind kind = 0

// String prints the kind in a humanly readable fashion.
func (k kind) String() string {
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
		return "{"
	case '}':
		return "}"
	case '[':
		return "["
	case ']':
		return "]"
	default:
		return "<invalid jsontext.Kind: " + strconv.QuoteRune(rune(k)) + ">"
	}
}

// normalize coalesces all possible starting characters of a number as just '0'.
func (k kind) normalize() kind {
	if k == '-' || ('0' <= k && k <= '9') {
		return '0'
	}
	return k
}

func IterJsonObject(data []byte) iter.Seq2[string, json.RawMessage] {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return func(yield func(key string, value json.RawMessage) bool) {
		tk, err := dec.Token()
		if err != nil {
			return
		}
		delim, ok := tk.(json.Delim)
		if !ok || delim != '{' {
			return
		}

		for dec.More() {
			tk, err := dec.Token()
			if err != nil {
				return
			}

			key, ok := tk.(string)
			if !ok {
				return
			}

			value := json.RawMessage{}
			err = dec.Decode(&value)
			if err != nil {
				return
			}

			if !yield(key, value) {
				return
			}
		}
	}
}
