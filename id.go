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

type idString string

func NewIdStr(s string) Id {
	return idString(s)
}

func (id idString) String() (string, bool) {
	return string(id), true
}

func (id idString) Int() (int, bool) {
	return 0, false
}

func (id idString) IsNull() bool {
	return false
}

func (id idString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(id))
}

type idInt int64

func NewIdInt(i int64) Id {
	return idInt(i)
}

func (id idInt) String() (string, bool) {
	return "", false
}

func (id idInt) Int() (int, bool) {
	return int(id), true
}

func (id idInt) IsNull() bool {
	return false
}

func (id idInt) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(id))
}

func NullId() Id {
	return idRaw("null")
}

type idRaw []byte

func (id idRaw) String() (string, bool) {
	if jsonValue(id).Kind() != 's' {
		return "", false
	}
	s := ""
	err := json.Unmarshal(id, &s)
	return s, err == nil
}

func (id idRaw) Int() (int, bool) {
	if jsonValue(id).Kind() != '0' {
		return 0, false
	}
	i := 0
	err := json.Unmarshal(id, &i)
	return i, err == nil
}

func (id idRaw) IsNull() bool {
	return jsonValue(id).Kind() == 'n'
}

func (id idRaw) MarshalJSON() ([]byte, error) {
	return id, nil
}
