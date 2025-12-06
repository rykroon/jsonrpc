package jsonrpc

import (
	"encoding/json"
	"errors"

	"golang.org/x/exp/constraints"
)

var ErrInvalidId = errors.New("id must be a string or integer")

// A JSON-RPC Id
type Id struct {
	raw json.RawMessage
}

// Create a new JSON-RPC Id
func NewId[T string | constraints.Integer](v T) *Id {
	data, _ := json.Marshal(v)
	id := &Id{}
	_ = id.setRaw(data)
	return id
}

func (id Id) String() string {
	return string(id.raw)
}

func (id Id) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.raw)
}

func (i *Id) UnmarshalJSON(data []byte) error {
	if err := i.setRaw(data); err != nil {
		return err
	}
	return nil
}

func (i *Id) setRaw(r json.RawMessage) error {
	if !isString(r) && !isInt(r) {
		return ErrInvalidId
	}
	i.raw = r
	return nil
}
