package jsonrpc

import (
	"encoding/json"
	"errors"
)

type Params interface {
	ByPosition() bool
	ByName() bool
	DecodeInto(any) error
	json.Marshaler
}

type params []byte

func NewParams(v any) (Params, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	jv := jsonValue(data)
	if jv.Kind().normalize() != '{' && jv.Kind().normalize() != '[' {
		return nil, errors.New("must be a valid json object or array")
	}
	return params(data), nil
}

func (p params) ByPosition() bool {
	jv := jsonValue(p)
	return jv.Kind().normalize() == '['
}

func (p params) ByName() bool {
	jv := jsonValue(p)
	return jv.Kind().normalize() == '{'
}

func (p params) DecodeInto(v any) error {
	return json.Unmarshal(p, v)
}

func (p params) MarshalJSON() ([]byte, error) {
	return json.Marshal([]byte(p))
}

func (p *params) UnmarshalJSON(data []byte) error {
	jv := jsonValue(data)
	if jv.Kind().normalize() != '{' && jv.Kind().normalize() != '[' {
		return errors.New("invalid type for params")
	}
	*p = params(jv.Clone())
	return nil
}
