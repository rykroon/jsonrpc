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

type idRaw []byte

func NewId[T string | int](v T) Id {
	data, _ := json.Marshal(v)
	return idRaw(data)
}

func NullId() Id {
	return idRaw("null")
}

func (id idRaw) String() (string, bool) {
	jv := jsonValue(id)
	if jv.Kind().normalize() != 's' {
		return "", false
	}
	s := ""
	err := json.Unmarshal(id, &s)
	return s, err == nil
}

func (id idRaw) Int() (int, bool) {
	jv := jsonValue(id)
	if jv.Kind().normalize() != '0' {
		return 0, false
	}
	i := 0
	err := json.Unmarshal(id, &i)
	return i, err == nil
}

func (id idRaw) IsNull() bool {
	jv := jsonValue(id)
	return jv.Kind().normalize() == 'n'
}

func (id idRaw) MarshalJSON() ([]byte, error) {
	return id, nil
}
