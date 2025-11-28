package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// A JSON-RPC Id
type Id struct {
	data json.RawMessage
}

// Create a new JSON-RPC Id
func NewId[T string | int](v T) *Id {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err) // this should never happen
	}
	return &Id{data: data}
}

func (id Id) String() string {
	return string(id.data)
}

func (id Id) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.data)
}

func (i *Id) UnmarshalJSON(data []byte) error {
	err := json.Unmarshal(data, &i.data)
	if err != nil {
		return fmt.Errorf("unmarshal failed: %w", err)
	}
	if !i.IsString() && !i.IsInt() {
		return errors.New("not a valid jsonrpc id")
	}
	return nil
}

// returns true if the id is a string
func (i Id) IsString() bool {
	return len(i.data) != 0 && i.data[0] == '"'
}

// returns true if the id is an integer
func (i Id) IsInt() bool {
	_, err := strconv.ParseInt(string(i.data), 10, 64)
	return err == nil
}
