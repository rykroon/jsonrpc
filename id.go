package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
)

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type integer interface {
	signed | unsigned
}

type ID struct {
	raw string
}

var (
	_ json.MarshalerTo     = ID{}
	_ json.UnmarshalerFrom = (*ID)(nil)
)

func newNullID() ID {
	return ID{"null"}
}

func NewStringID(v string) ID {
	b, _ := jsontext.AppendQuote(nil, v)
	return ID{string(b)}
}

func NewIntID[T integer](v T) ID {
	if v < 0 {
		return ID{strconv.FormatInt(int64(v), 10)}
	}
	return ID{strconv.FormatUint(uint64(v), 10)}
}

func (i ID) IsZero() bool {
	return i.raw == ""
}

func (i ID) IsNull() bool {
	return i.raw == "null"
}

func (i ID) String() string {
	return i.raw
}

// MarshalJSONTo writes the id. A zero ID is an error; tag the field omitzero.
func (i ID) MarshalJSONTo(enc *jsontext.Encoder) error {
	if i.IsZero() {
		return errors.New("jsonrpc: cannot marshal a zero ID")
	}
	return enc.WriteValue(jsontext.Value(i.raw))
}

// UnmarshalJSONFrom reads a JSON string, number, or null. Strings are
// re-quoted minimally so differently escaped ids compare equal; numbers are
// kept verbatim, as canonicalizing would round integers above 2^53.
func (i *ID) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	switch v.Kind() {
	case jsontext.KindString:
		s, err := jsontext.AppendUnquote(nil, v)
		if err != nil {
			return fmt.Errorf("jsonrpc: id: %w", err)
		}
		*i = NewStringID(string(s))
	case jsontext.KindNumber, jsontext.KindNull:
		*i = ID{string(v)}
	default:
		return fmt.Errorf("jsonrpc: id must be a JSON string, number, or null, got %s", v.Kind())
	}
	return nil
}
