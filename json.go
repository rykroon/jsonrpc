package jsonrpc

import (
	"encoding/json"
)

func isString(r json.RawMessage) bool {
	return len(r) != 0 && r[0] == '"'
}

func isInt(r json.RawMessage) bool {
	if isString(r) {
		return false
	}
	n := json.Number("")
	err := json.Unmarshal(r, &n)
	if err != nil {
		return false
	}
	_, err = n.Int64()
	return err == nil
}

func isAbsent(r json.RawMessage) bool {
	return len(r) == 0
}

func isNull(r json.RawMessage) bool {
	return string(r) == "null"
}

func isObject(r json.RawMessage) bool {
	return len(r) != 0 && r[0] == '{'
}

func isArray(r json.RawMessage) bool {
	return len(r) != 0 && r[0] == '['
}

func tokenName(b byte) string {
	switch b {
	case '"':
		return "string"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	case '{':
		return "object"
	case '[':
		return "array"
	default:
		return "number"
	}
}
