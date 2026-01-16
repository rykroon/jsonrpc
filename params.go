package jsonrpc

import (
	"encoding/json"
	"fmt"
)

type Params json.RawMessage

func NewParams(v any) (Params, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	// check valid params

	return Params(data), nil
}

func (p Params) MarshalJSON() ([]byte, error) {
	return p, nil
}

func (p *Params) UnmarshalJSON(b []byte) error {
	*p = b
	return nil
}
