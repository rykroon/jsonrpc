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
func NewId[T string | constraints.Integer](v T) *Id {
	data, _ := json.Marshal(v)
	return &Id{data}
}

func (id Id) String() string {
	return string(id.raw)
}

func (id *Id) IsZero() bool {
	return id == nil || isAbsent(id.raw) || isNull(id.raw)
}

func (id Id) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.raw)
}

func (i *Id) UnmarshalJSON(data []byte) error {
	if !isString(data) && !isInt(data) {
		return errors.New("id must be a string or an integer")
	}
	i.raw = append(make([]byte, 0, len(data)), data...)
	return nil
}
