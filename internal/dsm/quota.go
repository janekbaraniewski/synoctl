package dsm

import (
	"context"
	"net/url"
)

// ShareQuota is a per-share quota row derived from SYNO.Core.Share.list.
//
// We deliberately do *not* call SYNO.Core.Share.Quota — that dedicated
// endpoint is fussy across firmwares (different method names, mandatory
// volume params, occasional 105/permission denials) and the same
// information is already present in the Share.list payload we
// already fetch for the Shares view. Post-processing that list keeps
// us on a single, well-tested DSM endpoint.
//
// QuotaMiB is the configured quota in mebibytes (DSM's native unit
// for share_quota). UsedMiB is the current usage in the same unit.
// PercentUsed is a 0..100 convenience computed in code.
type ShareQuota struct {
	Name        string
	Path        string
	QuotaMiB    int64
	UsedMiB     int64
	PercentUsed int
	HardLimit   bool // DSM treats configured share_quota > 0 as hard by default
	Hidden      bool
	Description string
}

// QuotaBytes returns the configured quota in bytes (MiB → bytes).
func (q ShareQuota) QuotaBytes() uint64 { return uint64(q.QuotaMiB) * 1024 * 1024 }

// UsedBytes returns the current usage in bytes.
func (q ShareQuota) UsedBytes() uint64 { return uint64(q.UsedMiB) * 1024 * 1024 }

// Ratio returns the 0..1 usage ratio (0 when no quota is configured).
func (q ShareQuota) Ratio() float64 {
	if q.QuotaMiB <= 0 {
		return 0
	}
	r := float64(q.UsedMiB) / float64(q.QuotaMiB)
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// ShareQuotas returns per-share quota rows derived from Shares(). Only
// shares with a non-zero configured quota are returned — DSM stores 0
// for "unlimited", and a list of unlimited shares isn't actionable. The
// list is sorted by descending usage percentage so heavy users float
// to the top.
func (c *Client) ShareQuotas(ctx context.Context) ([]ShareQuota, error) {
	shares, err := c.Shares(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ShareQuota, 0, len(shares))
	for _, s := range shares {
		if s.ShareQuota <= 0 {
			continue
		}
		pct := 0
		if s.ShareQuota > 0 {
			pct = int(s.ShareQuotaUsed * 100 / s.ShareQuota)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
		}
		out = append(out, ShareQuota{
			Name:        s.Name,
			Path:        s.Path,
			QuotaMiB:    s.ShareQuota,
			UsedMiB:     s.ShareQuotaUsed,
			PercentUsed: pct,
			HardLimit:   true,
			Hidden:      s.Hidden,
			Description: s.Desc,
		})
	}
	// Descending by percentage used; ties broken by name for stability.
	sortShareQuotas(out)
	return out, nil
}

// sortShareQuotas sorts in-place by PercentUsed desc, then Name asc.
// Implemented without sort.Slice to avoid an additional import — the
// list is small enough that an insertion sort is fine.
func sortShareQuotas(s []ShareQuota) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && lessShareQuota(s[j], s[j-1]) {
			s[j], s[j-1] = s[j-1], s[j]
			j--
		}
	}
}

func lessShareQuota(a, b ShareQuota) bool {
	if a.PercentUsed != b.PercentUsed {
		return a.PercentUsed > b.PercentUsed
	}
	return a.Name < b.Name
}

// UserQuotaVolume is one per-volume row inside a UserQuota — DSM lets
// administrators set per-user quotas on each volume independently, so
// a single user can have different limits on /volume1 vs /volume2.
type UserQuotaVolume struct {
	Volume      string `json:"share"`
	QuotaMiB    int64  `json:"user_quota,omitempty"`
	UsedMiB     int64  `json:"used_quota,omitempty"`
	PercentUsed int    `json:"-"`
}

// UserQuota is one entry from SYNO.Core.User.Quota.list — a single
// local user's quota allocation across the volumes that have one
// configured.
//
// Some DSM builds return only the aggregate Total* fields and skip the
// per-volume breakdown. We populate Volumes when DSM provides it and
// fall back to the aggregates otherwise.
type UserQuota struct {
	Name        string            `json:"name"`
	UID         int               `json:"uid,omitempty"`
	TotalQuota  int64             `json:"quota,omitempty"`
	TotalUsed   int64             `json:"used,omitempty"`
	Volumes     []UserQuotaVolume `json:"volumes,omitempty"`
	PercentUsed int               `json:"-"`
	HasLimit    bool              `json:"-"`
}

// UserQuotas lists per-user quotas via SYNO.Core.User.Quota "list" v1.
// Returns an empty slice (and nil error) when the API is not advertised
// or the response is empty — that's expected on stock DSM installs
// where no admin has touched quotas.
func (c *Client) UserQuotas(ctx context.Context) ([]UserQuota, error) {
	const api = "SYNO.Core.User.Quota"
	if !c.Supports(api) {
		return []UserQuota{}, nil
	}
	params := url.Values{}
	params.Set("offset", "0")
	params.Set("limit", "-1")
	var resp struct {
		Users []UserQuota `json:"users"`
		Items []UserQuota `json:"items"`
		Total int         `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		if isAPIMissing(err) {
			return []UserQuota{}, nil
		}
		return nil, err
	}
	rows := resp.Users
	if len(rows) == 0 {
		rows = resp.Items
	}
	for i := range rows {
		rows[i].PercentUsed = pctOf(rows[i].TotalUsed, rows[i].TotalQuota)
		rows[i].HasLimit = rows[i].TotalQuota > 0
		for j := range rows[i].Volumes {
			rows[i].Volumes[j].PercentUsed = pctOf(rows[i].Volumes[j].UsedMiB, rows[i].Volumes[j].QuotaMiB)
		}
		// If the aggregate is missing but per-volume rows have quotas,
		// roll them up so the row has something to show.
		if rows[i].TotalQuota == 0 && len(rows[i].Volumes) > 0 {
			var q, u int64
			for _, v := range rows[i].Volumes {
				q += v.QuotaMiB
				u += v.UsedMiB
			}
			rows[i].TotalQuota = q
			rows[i].TotalUsed = u
			rows[i].PercentUsed = pctOf(u, q)
			rows[i].HasLimit = q > 0
		}
	}
	return rows, nil
}

func pctOf(used, quota int64) int {
	if quota <= 0 {
		return 0
	}
	p := int(used * 100 / quota)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}
