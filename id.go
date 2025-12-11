package jsonrpc

import (
	"encoding/json"
	"errors"

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
		return errors.New("id must be a string or an integer")
	}
	i.raw = append(make([]byte, 0, len(data)), data...)
	return nil
}

func (i Id) IsAbsent() bool {
	return isAbsent(i.raw)
}
