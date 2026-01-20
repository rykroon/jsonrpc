package jsonrpc

import "encoding/json"

type Params interface {
	ByPosition() bool
	ByName() bool
	IsAbsent() bool // maybe not?
	DecodeInto(any) error
}

type paramBytes []byte

func (p paramBytes) ByPosition() bool {
	return len(p) != 0 && p[0] == '['
}

func (p paramBytes) ByName() bool {
	return len(p) != 0 && p[0] == '{'
}

func (p paramBytes) IsAbsent() bool {
	return len(p) == 0
}

func (p paramBytes) DecodeInto(v any) error {
	return json.Unmarshal(p, v)
}

type paramMap[T any] map[string]T

func NewParamFromMap[T any](m map[string]T) Params {
	return paramMap[T](m)
}

func (pm paramMap[T]) ByPosition() bool {
	return false
}

func (pm paramMap[T]) ByName() bool {
	return true
}

func (pm paramMap[T]) IsAbsent() bool {
	return false
}

func (pm paramMap[T]) DecodeInto(v any) error {
	b, err := json.Marshal(pm)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

type paramSlice[T any] []T

func NewParamFromSlice[T any](s []T) Params {
	return paramSlice[T](s)
}

func (ps paramSlice[T]) ByPosition() bool {
	return true
}

func (ps paramSlice[T]) ByName() bool {
	return false
}

func (ps paramSlice[T]) IsAbsent() bool {
	return false
}

func (ps paramSlice[T]) DecodeInto(v any) error {
	b, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
