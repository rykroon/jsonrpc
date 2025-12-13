package jsonrpc

import (
	"encoding/json"
	"reflect"

	"golang.org/x/exp/constraints"
)

// A JSON-RPC Id
type Id struct {
	raw json.RawMessage
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
	return string(id.raw)
}

func (id Id) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.raw)
}

func (i *Id) UnmarshalJSON(data []byte) error {
	if !isNull(data) && !isString(data) && !isInt(data) {
		return &json.UnmarshalTypeError{
			Value: tokenName(data[0]),
			Type:  reflect.TypeOf(i).Elem(),
		}
	}
	i.raw = append(make([]byte, 0, len(data)), data...)
	return nil
}

func (i Id) AsString() (string, error) {
	s := ""
	err := json.Unmarshal(i.raw, &s)
	if err != nil {
		return "", err
	}
	return s, nil
}

func (i Id) AsInt64() (int64, error) {
	n := json.Number("")
	err := json.Unmarshal(i.raw, &n)
	if err != nil {
		return 0, err
	}
	integer, err := n.Int64()
	if err != nil {
		return 0, err
	}
	return integer, nil
}

func (i Id) IsAbsent() bool {
	return isAbsent(i.raw)
}
