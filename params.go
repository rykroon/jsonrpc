package jsonrpc

import (
	"encoding/json"
)

// IDEA: perhaps the interfaces should only implement json.Marshaler
// and not json.Unmarshaler. It makes sense that all jsonrpc interfaces
// should be able to serialize to json, but depending on the underlying
// implementation it may not make sense for it to be parsed from json.

type Params interface {
	ByPosition() bool
	ByName() bool
	DecodeInto(any) error
	Get(string) (any, bool)
	At(int) (any, bool)
	json.Marshaler
}

type rawParams []byte

func (p rawParams) ByPosition() bool {
	return len(p) != 0 && p[0] == '['
}

func (p rawParams) ByName() bool {
	return len(p) != 0 && p[0] == '{'
}

func (p rawParams) DecodeInto(v any) error {
	return json.Unmarshal(p, v)
}

func (p rawParams) Get(key string) (any, bool) {
	if !p.ByName() {
		return nil, false
	}
	// do later
	return nil, false
}

func (p rawParams) At(idx int) (any, bool) {
	if !p.ByPosition() {
		return nil, false
	}
	// do later
	return nil, false
}

func (p rawParams) MarshalJSON() ([]byte, error) {
	return json.Marshal([]byte(p))
}

// func (p *rawParams) UnmarshalJSON(data []byte) error {
// 	if !isJsonObject(data) && !isJsonArray(data) {
// 		// return json.UnmarshalTypeError ??
// 		return NewError(0, "params must be an object or array", nil)
// 	}
// 	*p = data
// 	return nil
// }

type mapParams[T any] map[string]T

func NewParamFromMap[T any](m map[string]T) Params {
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

func (p mapParams[T]) Get(key string) (any, bool) {
	value, ok := p[key]
	if !ok {
		return nil, false
	}
	return value, true
}

func (p mapParams[T]) At(idx int) (any, bool) {
	return nil, false
}

func (p mapParams[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]T(p))
}

// func (p mapParams[T]) UnmarshalJSON(data []byte) error {
// 	m := map[string]T{}
// 	err := json.Unmarshal(data, &m)
// 	if err != nil {
// 		return err
// 	}
// 	pm = m
// 	return nil
// }

type sliceParams[T any] []T

func NewParamFromSlice[T any](s []T) Params {
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

func (p sliceParams[T]) Get(key string) (any, bool) {
	return nil, false
}

func (p sliceParams[T]) At(idx int) (any, bool) {
	if idx < 0 || idx >= len(p) {
		return nil, false
	}
	return p[idx], true
}

func (p sliceParams[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal([]T(p))
}

// func (ps sliceParams[T]) UnmarshalJSON(data []byte) error {
// 	s := []T{}
// 	err := json.Unmarshal(data, &s)
// 	if err != nil {
// 		return err
// 	}
// 	ps = s
// 	return nil
// }
