package dsm

import (
	"bytes"
	"strconv"
)

// flexBool decodes a JSON bool, number (0/1), or string ("true"/"1"/"yes")
// — DSM uses all three across firmware versions for the same field.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		*b = false
		return nil
	}
	switch data[0] {
	case 't', 'T':
		*b = true
		return nil
	case 'f', 'F':
		*b = false
		return nil
	case '"':
		// Quoted variant — try the unquoted forms.
		if len(data) >= 2 {
			s := string(data[1 : len(data)-1])
			switch s {
			case "true", "1", "yes":
				*b = true
				return nil
			case "false", "0", "no", "":
				*b = false
				return nil
			}
		}
	}
	// Numeric variant.
	if n, err := strconv.ParseFloat(string(data), 64); err == nil {
		*b = n != 0
		return nil
	}
	*b = false
	return nil
}

func (b flexBool) Bool() bool { return bool(b) }
