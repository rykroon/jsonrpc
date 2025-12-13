package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

type Params struct {
	value value
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
	return p.value.String()
}

func (p Params) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.value)
}

func (p *Params) UnmarshalJSON(data []byte) error {
	temp := value(data)
	if temp.Kind() != '{' && temp.Kind() != '[' {
		return &json.UnmarshalTypeError{
			Value: temp.Kind().String(),
			Type:  reflect.TypeOf(p).Elem(),
		}
	}
	p.value = temp.Clone()
	return nil
}

func (p Params) IsAbsent() bool {
	return len(p.value) == 0
}

func (p Params) ByPosition() bool {
	return p.value.Kind() == '['
}

func (p Params) ByName() bool {
	return p.value.Kind() == '{'
}

// Decode the params into a value
func (p Params) Decode(v any) error {
	if p.IsAbsent() {
		return errors.New("no params")
	}
	return json.Unmarshal(p.value, v)
}
