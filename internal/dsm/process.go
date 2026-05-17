package dsm

import (
	"context"
)

// Process is one entry from SYNO.Core.System.Process.list.
//
// CPU is reported in ticks (DSM's internal counter — not a percentage of
// total CPU), so it's only useful in relative form. Mem is RSS in KiB.
type Process struct {
	PID       int    `json:"pid"`
	Command   string `json:"command"`
	CPU       int    `json:"cpu"`        // relative ticks
	Mem       int    `json:"mem"`        // RSS in KiB
	MemShared int    `json:"mem_shared"` // KiB
	Status    string `json:"status"`     // R / S / D / Z …
}

// Processes returns the current process list. The list is fetched in
// whatever order DSM returns it — the caller should sort to taste.
func (c *Client) Processes(ctx context.Context) ([]Process, error) {
	var resp struct {
		Process []Process `json:"process"`
	}
	if err := c.Call(ctx, "SYNO.Core.System.Process", 1, "list", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Process, nil
}
