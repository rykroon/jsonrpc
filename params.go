package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// Params is a request's params: a JSON object or array, or zero when absent.
type Params struct {
	value jsontext.Value
}

var (
	_ json.MarshalerTo     = Params{}
	_ json.UnmarshalerFrom = (*Params)(nil)
)

// NewParams marshals v under opts. A nil v gives zero Params (none sent); a
// Params passes through. The result must be an object or array, as §4.2
// requires, so a scalar or null (a typed nil marshals to one) is an error.
func NewParams(v any, opts ...json.Options) (Params, error) {
	switch v := v.(type) {
	case nil:
		return Params{}, nil
	case Params:
		return v, nil
	}
	b, err := json.Marshal(v, opts...)
	if err != nil {
		return Params{}, fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	return paramsFromValue(b)
}

// paramsFromValue checks v's shape and copies it, as v may be a decoder's buffer.
func paramsFromValue(v jsontext.Value) (Params, error) {
	switch v.Kind() {
	case jsontext.KindBeginArray, jsontext.KindBeginObject:
		return Params{v.Clone()}, nil
	}
	return Params{}, fmt.Errorf("jsonrpc: params must be a JSON object or array, got %s", v.Kind())
}

func (p Params) IsObject() bool {
	return p.value.Kind() == jsontext.KindBeginObject
}

func (p Params) IsArray() bool {
	return p.value.Kind() == jsontext.KindBeginArray
}

func (p Params) IsZero() bool {
	return len(p.value) == 0
}

// String returns the params' JSON, or "" when absent.
func (p Params) String() string {
	return string(p.value)
}

// Bytes returns a copy of the params' JSON, nil when absent.
func (p Params) Bytes() jsontext.Value {
	return p.value.Clone()
}

// Decode unmarshals the params into into under opts. Absent params leave into
// untouched, as typed handlers do.
func (p Params) Decode(into any, opts ...json.Options) error {
	if p.IsZero() {
		return nil
	}
	return json.Unmarshal(p.value, into, opts...)
}

// Map splits an object's members without decoding them.
func (p Params) Map() (map[string]jsontext.Value, error) {
	if !p.IsObject() {
		return nil, errors.New("jsonrpc: params must be a JSON object")
	}
	m := map[string]jsontext.Value{}
	if err := p.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// Slice splits an array's elements without decoding them.
func (p Params) Slice() ([]jsontext.Value, error) {
	if !p.IsArray() {
		return nil, errors.New("jsonrpc: params must be a JSON array")
	}
	s := []jsontext.Value{}
	if err := p.Decode(&s); err != nil {
		return nil, err
	}
	return s, nil
}

// MarshalJSONTo writes the params. Zero Params is an error; tag the field omitzero.
func (p Params) MarshalJSONTo(enc *jsontext.Encoder) error {
	if p.IsZero() {
		return errors.New("jsonrpc: cannot marshal zero Params")
	}
	return enc.WriteValue(p.value)
}

// UnmarshalJSONFrom reads a JSON object or array; null and scalars are errors.
func (p *Params) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	params, err := paramsFromValue(v)
	if err != nil {
		return err
	}
	*p = params
	return nil
}
