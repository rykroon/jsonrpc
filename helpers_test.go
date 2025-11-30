package jsonrpc

import "encoding/json"

type NestedArray []any

// UnmarshalJSON method to recursively decode JSON
func (a *NestedArray) UnmarshalJSON(data []byte) error {
	// Create a temporary map
	tempArray := make([]json.RawMessage, 0)

	// Unmarshal into the temporary map
	if err := json.Unmarshal(data, &tempArray); err != nil {
		return err
	}

	// Iterate over the temporary map
	for _, rawValue := range tempArray {
		if len(rawValue) > 0 && rawValue[0] == '{' {
			subMap := make(NestedMap)
			if err := json.Unmarshal(rawValue, &subMap); err != nil {
				return err
			}
			*a = append(*a, subMap)
		} else if len(rawValue) > 0 && rawValue[0] == '[' {
			subArray := make(NestedArray, 0)
			if err := json.Unmarshal(rawValue, &subArray); err != nil {
				return err
			}
			*a = append(*a, subArray)

		} else {
			*a = append(*a, rawValue)
		}
	}
	return nil
}

type NestedMap map[string]any

// UnmarshalJSON method to recursively decode JSON
func (nm *NestedMap) UnmarshalJSON(data []byte) error {
	// Create a temporary map
	tempMap := make(map[string]json.RawMessage)

	// Unmarshal into the temporary map
	if err := json.Unmarshal(data, &tempMap); err != nil {
		return err
	}

	// Iterate over the temporary map
	for key, rawValue := range tempMap {
		if len(rawValue) > 0 && rawValue[0] == '{' {
			subMap := make(NestedMap)
			if err := json.Unmarshal(rawValue, &subMap); err != nil {
				return err
			}
			(*nm)[key] = subMap
		} else if len(rawValue) > 0 && rawValue[0] == '[' {
			subArray := make(NestedArray, 0)
			if err := json.Unmarshal(rawValue, &subArray); err != nil {
				return err
			}
			(*nm)[key] = subArray
		} else {
			(*nm)[key] = rawValue
		}
	}
	return nil
}
