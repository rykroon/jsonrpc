package jsonrpc

import (
	"encoding/json/jsontext"
	"strconv"
)

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type integer interface {
	signed | unsigned
}

type ID struct {
	raw string
}

func newNullID() ID {
	return ID{"null"}
}

func NewStringID(v string) ID {
	b, _ := jsontext.AppendQuote(nil, v)
	return ID{string(b)}
}

func NewIntID[T integer](v T) ID {
	if v < 0 {
		return ID{strconv.FormatInt(int64(v), 10)}
	}
	return ID{strconv.FormatUint(uint64(v), 10)}
}

func (i ID) IsZero() bool {
	return i.raw == ""
}

func (i ID) IsNull() bool {
	return i.raw == "null"
}

func (i ID) String() string {
	return i.raw
}
