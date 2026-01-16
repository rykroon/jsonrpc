package jsonrpc

import (
	"encoding/json"

	"golang.org/x/exp/constraints"
)

// A JSON-RPC Id
type Id json.RawMessage

// Create a new JSON-RPC Id
func NewId[T string | constraints.Integer](v T) Id {
	data, _ := json.Marshal(v)
	return Id(data)
}

func NullId() Id {
	return Id("null")
}

func (i Id) MarshalJSON() ([]byte, error) {
	return i, nil
}

func (i *Id) UnmarshalJSON(b []byte) error {
	*i = b
	return nil
}
