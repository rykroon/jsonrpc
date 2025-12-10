package jsonrpc

import (
	"encoding/json"
)

func isString(r json.RawMessage) bool {
	return len(r) != 0 && r[0] == '"' && r[len(r)-1] == '"'
}

func isInt(r json.RawMessage) bool {
	if isString(r) {
		return false
	}
	var n json.Number
	err := json.Unmarshal(r, &n)
	if err != nil {
		return false
	}
	_, err = n.Int64()
	return err == nil
}

func isEmpty(r json.RawMessage) bool {
	return len(r) == 0
}

func isNull(r json.RawMessage) bool {
	return string(r) == "null"
}

func isObject(r json.RawMessage) bool {
	return len(r) != 0 && r[0] == '{' && r[len(r)-1] == '}'
}

func isArray(r json.RawMessage) bool {
	return len(r) != 0 && r[0] == '[' && r[len(r)-1] == ']'
}
