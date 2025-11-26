package jsonrpc

import (
	"encoding/json"
	"errors"
	"strconv"
)

type Id json.RawMessage

type IdConstraint interface {
	string | int
}

func NewId[T IdConstraint](v T) Id {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err) // this should never happen
	}
	return Id(data)
}

func NoId() Id {
	return Id([]byte{})
}

func NullId() Id {
	return Id("null")
}

func (id Id) String() string {
	return string(id)
}

func (id Id) MarshalJSON() ([]byte, error) {
	// overrides default behavior of base64 encoded bytes.
	return id, nil
}

func (i *Id) UnmarshalJSON(data []byte) error {
	*i = append((*i)[:0], data...)
	if i.IsEmpty() || i.IsNull() || i.IsString() || i.IsInt() {
		return nil
	}
	return errors.New("not a valid jsonrpc id")
}

func (i Id) IsNull() bool {
	return string(i) == "null"
}

func (i Id) IsEmpty() bool {
	return len(i) == 0
}

func (i Id) IsString() bool {
	return len(i) != 0 && i[0] == '"'
}

func (i Id) IsInt() bool {
	_, err := strconv.ParseInt(string(i), 10, 64)
	return err == nil
}

func (i Id) IsValidForResponse() bool {
	return i.IsNull() || i.IsString() || i.IsInt()
}

func (i Id) IsValidForRequest() bool {
	return i.IsEmpty() || i.IsString() || i.IsInt()
}
