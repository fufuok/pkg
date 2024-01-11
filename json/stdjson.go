//go:build std_json

package json

import (
	"encoding/json"
)

var (
	Name          = "std_json"
	Marshal       = json.Marshal
	Unmarshal     = json.Unmarshal
	MarshalIndent = json.MarshalIndent
	NewDecoder    = json.NewDecoder
	NewEncoder    = json.NewEncoder
)
