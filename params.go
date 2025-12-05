package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
)

type Params struct {
	data json.RawMessage
}

func NewParams(v any) (*Params, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	params := &Params{data: data}
	if !params.ByName() && !params.ByPosition() {
		return nil, errors.New("invalid params")
	}

	return params, nil
}

func (p Params) String() string {
	return string(p.data)
}

func (p Params) ByPosition() bool {
	return len(p.data) != 0 && p.data[0] == '['
}

func (p Params) ByName() bool {
	return len(p.data) != 0 && p.data[0] == '{'
}

func (p *Params) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.data)
}

func (p *Params) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &p.data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal json: %w", err)
	}
	if p.ByName() || p.ByPosition() {
		return nil
	}
	return errors.New("not a valid params type")
}

type Positional interface {
	GetParamPointers() []any
}

// Decode the params into a value
func (p *Params) DecodeInto(v any) error {
	if p.ByPosition() {
		if positional, ok := v.(Positional); ok {
			pointers := positional.GetParamPointers()
			return json.Unmarshal(p.data, &pointers)
		}
		return fmt.Errorf("type %T does not support positional params", v)
	}
	return json.Unmarshal(p.data, v)
}
