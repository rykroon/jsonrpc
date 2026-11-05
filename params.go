package jsonrpc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

type Params struct {
	value jsontext.Value
}

var (
	_ json.MarshalerTo     = Params{}
	_ json.UnmarshalerFrom = (*Params)(nil)
)

func NewParams2(p any, opts ...json.Options) (Params, error) {
	if p == nil {
		return Params{}, nil
	}
	b, err := json.Marshal(p, opts...)
	if err != nil {
		return Params{}, fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	v := jsontext.Value(b)
	switch v.Kind() {
	case jsontext.KindBeginArray, jsontext.KindBeginObject:
	default:
		return Params{}, fmt.Errorf("jsonrpc: params must be a JSON object or array, got %s", v.Kind())
	}
	return Params{v}, nil
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

func (p Params) DecodeInto(v any, opts ...json.Options) error {
	if p.IsZero() {
		return nil // absent params: leave v untouched, as typed handlers do
	}
	return json.Unmarshal(p.value, v, opts...)
}

func (p Params) Map() (map[string]jsontext.Value, error) {
	if !p.IsObject() {
		return nil, errors.New("jsonrpc: params must be a JSON object")
	}
	m := map[string]jsontext.Value{}
	err := p.DecodeInto(&m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (p Params) Slice() ([]jsontext.Value, error) {
	if !p.IsArray() {
		return nil, errors.New("jsonrpc: params must be a JSON array")
	}
	s := []jsontext.Value{}
	err := p.DecodeInto(&s)
	if err != nil {
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
	switch v.Kind() {
	case jsontext.KindBeginObject, jsontext.KindBeginArray:
		// ReadValue's buffer is only valid until the next read.
		p.value = v.Clone()
		return nil
	}
	return fmt.Errorf("jsonrpc: params must be a JSON object or array, got %s", v.Kind())
}
