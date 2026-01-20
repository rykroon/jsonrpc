package jsonrpc

import (
	"encoding/json"
	"slices"
)

type Id interface {
	String() (string, bool)
	Int() (int, bool)
	IsNull() bool
}

type idString string

func NewIdString(s string) Id {
	return idString(s)
}

func (i idString) String() (string, bool) {
	return string(i), true
}

func (i idString) Int() (int, bool) {
	return 0, false
}

func (i idString) IsNull() bool {
	return false
}

type idInt int

func NewIdInt(i int) Id {
	return idInt(i)
}

func (i idInt) String() (string, bool) {
	return "", false
}

func (i idInt) Int() (int, bool) {
	return int(i), true
}

func (i idInt) IsNull() bool {
	return false
}

type idBytes []byte

func NullId() Id {
	return idBytes("null")
}

func (i idBytes) String() (string, bool) {
	if len(i) != 0 && i[0] == '"' {
		s := ""
		err := json.Unmarshal(i, &s)
		if err == nil {
			return s, true
		}
	}
	return "", false
}

func (id idBytes) Int() (int, bool) {
	if len(id) != 0 && id[0] == '-' || ('0' <= id[0] && id[0] <= '9') {
		i := 0
		err := json.Unmarshal(id, &i)
		if err == nil {
			return i, true
		}
	}
	return 0, false
}

func (id idBytes) IsNull() bool {
	return slices.Equal(id, []byte("null"))
}
