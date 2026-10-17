package dsm

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScheduledTaskTriggerTimeFormats(t *testing.T) {
	want := time.Date(2026, 10, 18, 0, 0, 0, 0, time.Local).Unix()
	cases := map[string]int64{
		`{"next_trigger_time": "2026-10-18 00:00"}`:    want,
		`{"next_trigger_time": "2026-10-18 00:00:00"}`: want,
		`{"next_trigger_time": 1760745600}`:            1760745600,
		`{"next_trigger_time": "1760745600"}`:          1760745600,
		`{"next_trigger_time": ""}`:                    0,
		`{"next_trigger_time": null}`:                  0,
	}
	for in, exp := range cases {
		var tk ScheduledTask
		if err := json.Unmarshal([]byte(in), &tk); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := tk.NextTriggerTime.Int64(); got != exp {
			t.Errorf("%s: got %d, want %d", in, got, exp)
		}
	}
}
