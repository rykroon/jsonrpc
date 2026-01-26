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

type paramsRaw []byte

func (p paramsRaw) ByPosition() bool {
	return jsonValue(p).Kind() == '['
}

func (p paramsRaw) ByName() bool {
	return jsonValue(p).Kind() == '{'
}

func (p paramsRaw) DecodeInto(v any) error {
	return json.Unmarshal(p, v)
}

func (p paramsRaw) MarshalJSON() ([]byte, error) {
	return json.Marshal([]byte(p))
}

func (p *paramsRaw) UnmarshalJSON(data []byte) error {
	jv := jsonValue(data)
	if jv.Kind() != '{' && jv.Kind() != '[' {
		return errors.New("invalid type for params")
	}
	*p = data
	return nil
}

type mapParams[T any] map[string]T

func NewParamsMap[T any](m map[string]T) Params {
	return mapParams[T](m)
}

func (p mapParams[T]) ByPosition() bool {
	return false
}

func (p mapParams[T]) ByName() bool {
	return true
}

func (p mapParams[T]) DecodeInto(v any) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (p mapParams[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]T(p))
}

type sliceParams[T any] []T

// NewPositionalParams() ??? make variadic???
func NewParamsSlice[T any](s []T) Params {
	return sliceParams[T](s)
}

func (p sliceParams[T]) ByPosition() bool {
	return true
}

func (p sliceParams[T]) ByName() bool {
	return false
}

func (p sliceParams[T]) DecodeInto(v any) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (p sliceParams[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal([]T(p))
}
