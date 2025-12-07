package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
)

type Params struct {
	raw json.RawMessage
}

func NewParams(v any) (*Params, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	if !isArray(data) && !isObject(data) {
		return nil, errors.New("invalid params")
	}

	return &Params{data}, nil
}

func (p Params) String() string {
	return string(p.raw)
}

func (p Params) ByPosition() bool {
	return isArray(p.raw)
}

func (p Params) ByName() bool {
	return isObject(p.raw)
}

func (p *Params) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.raw)
}

func (p *Params) UnmarshalJSON(data []byte) error {
	if !isObject(data) && !isArray(data) {
		return errors.New("invalid params")
	}
	p.raw = data
	return nil
}

type Positional interface {
	GetParamPointers() []any
}

// Decode the params into a value
func (p *Params) DecodeInto(v any) error {
	if p.ByPosition() {
		if positional, ok := v.(Positional); ok {
			pointers := positional.GetParamPointers()
			return json.Unmarshal(p.raw, &pointers)
		}
		return fmt.Errorf("type %T does not support positional params", v)
	}
	return json.Unmarshal(p.raw, v)
}
