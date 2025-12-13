package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

type Params struct {
	raw json.RawMessage
}

func NewParams(v any) (*Params, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	p := &Params{}
	err = p.UnmarshalJSON(data)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p Params) String() string {
	return string(p.raw)
}

func (p Params) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.raw)
}

func (p *Params) UnmarshalJSON(data []byte) error {
	if !isObject(data) && !isArray(data) {
		return &json.UnmarshalTypeError{
			Value: tokenName(data[0]),
			Type:  reflect.TypeOf(p).Elem(),
		}
	}
	p.raw = append(make([]byte, 0, len(data)), data...)
	return nil
}

func (p Params) IsAbsent() bool {
	return isAbsent(p.raw)
}

func (p Params) ByPosition() bool {
	return isArray(p.raw)
}

func (p Params) ByName() bool {
	return isObject(p.raw)
}

// Decode the params into a value
func (p Params) Decode(v any) error {
	if p.IsAbsent() {
		return errors.New("no params")
	}
	return json.Unmarshal(p.raw, v)
}
