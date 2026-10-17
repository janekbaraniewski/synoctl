package dsm

import (
	"bytes"
	"strconv"
	"time"
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

// flexEpoch decodes a timestamp sent either as epoch seconds (number or
// quoted number) or as a "2006-01-02 15:04[:05]" string in the NAS's
// local time — DSM 7.4 Task Scheduler sends the latter. Anything else
// decodes to 0 rather than failing the whole response.
type flexEpoch int64

func (e *flexEpoch) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	*e = 0
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	s := string(data)
	if data[0] == '"' {
		unq, err := strconv.Unquote(s)
		if err != nil {
			return nil
		}
		s = unq
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		*e = flexEpoch(n)
		return nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			*e = flexEpoch(t.Unix())
			return nil
		}
	}
	return nil
}

func (e flexEpoch) Int64() int64 { return int64(e) }
