package jsonrpc

import (
	"encoding/json"
	"errors"
	"reflect"

	"golang.org/x/exp/constraints"
)

// A JSON-RPC Id
type Id struct {
	value value
}

// Create a new JSON-RPC Id
func NewId[T string | constraints.Integer](v T) Id {
	data, _ := json.Marshal(v)
	return Id{data}
}

func NullId() Id {
	return Id{[]byte("null")}
}

func (id Id) String() string {
	return id.value.String()
}

func (id Id) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.value)
}

func (i *Id) UnmarshalJSON(data []byte) error {
	temp := value(data)
	if temp.Kind() != 'n' && temp.Kind() != '"' && temp.Kind() != '0' {
		return &json.UnmarshalTypeError{
			Value: temp.Kind().String(),
			Type:  reflect.TypeOf(i).Elem(),
		}
	}
	i.value = temp.Clone()
	return nil
}

func (i Id) AsString() (string, error) {
	if i.value.Kind() != '"' {
		return "", errors.New("not a string")
	}
	s := ""
	err := json.Unmarshal(i.value, &s)
	if err != nil {
		return "", err
	}
	return s, nil
}

func (i Id) AsInt64() (int64, error) {
	if i.value.Kind() != '0' {
		return 0, errors.New("not an int")
	}
	n := json.Number("")
	err := json.Unmarshal(i.value, &n)
	if err != nil {
		return 0, err
	}
	integer, err := n.Int64()
	if err != nil {
		return 0, err
	}
	return integer, nil
}
