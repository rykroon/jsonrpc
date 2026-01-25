package jsonrpc

import (
	"encoding/json"
)

type Id interface {
	String() (string, bool)
	Int() (int, bool)
	IsNull() bool
	json.Marshaler
}

type idType struct {
	value any
}

func NewId[T string | int](v T) Id {
	return idType{v}
}

func NullId() Id {
	return idType{nil}
}

func (id idType) String() (string, bool) {
	s, ok := id.value.(string)
	return s, ok
}

func (id idType) Int() (int, bool) {
	i, ok := id.value.(int)
	return i, ok
}

func (id idType) IsNull() bool {
	return id.value == nil
}

func (id idType) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.value)
}

// func (id idType) UnmarshalJSON(data []byte) error {
// 	if !isJsonInt(data) || !isJsonString(data) || !isJsonNull(data) {
// 		// return json.UnmarshalTypeError
// 		return NewError(0, "id must be string, int or null", nil)
// 	}
// 	return json.Unmarshal(data, &id.value)
// }

type rawId []byte

func (id rawId) String() (string, bool) {
	if value(id).Kind() != 's' {
		return "", false
	}
	s := ""
	err := json.Unmarshal(id, &s)
	return s, err == nil
}

func (id rawId) Int() (int, bool) {
	if value(id).Kind() != '0' {
		return 0, false
	}
	i := 0
	err := json.Unmarshal(id, &i)
	return i, err == nil
}

func (id rawId) IsNull() bool {
	return value(id).Kind() != 'n'
}

func (id rawId) MarshalJSON() ([]byte, error) {
	return id, nil
}

// func (id *rawId) UnmarshalJSON(data []byte) error {
// 	if !isJsonInt(data) || !isJsonString(data) || !isJsonNull(data) {
// 		return NewError(0, "id must be string, int or null", nil)
// 	}
// 	*id = data
// 	return nil
// }
