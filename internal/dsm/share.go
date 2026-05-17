package dsm

import (
	"context"
	"net/url"
)

// Share is a shared folder. Some flag fields arrive as 0/1 instead of
// bool depending on firmware; flexBool tolerates both.
type Share struct {
	Name          string `json:"name"`
	Path          string `json:"vol_path"`
	Desc          string `json:"desc"`
	Encryption    int    `json:"encryption,omitempty"` // 0 = none, 1 = encrypted
	EncStatus     int    `json:"enc_status,omitempty"`
	Hidden        bool   `json:"hidden"`
	EnableRecycle bool   `json:"enable_recycle_bin"`
	// DSM is inconsistent here: some firmware returns this as bool, others
	// as int. We tolerate both via a flexBool decoder.
	RecycleAdminOnly flexBool `json:"recycle_bin_admin_only,omitempty"`
	Readonly         bool     `json:"is_readonly,omitempty"`
	IsUsbShare       bool     `json:"is_usb_share,omitempty"`
	IsSyncShare      bool     `json:"is_sync_share,omitempty"`
	IsCloudSync      bool     `json:"is_cloudsync_share,omitempty"`
	UUID             string   `json:"uuid,omitempty"`
	ShareQuota       int64    `json:"share_quota,omitempty"` // MB
	ShareQuotaUsed   int64    `json:"share_quota_used,omitempty"`
}

// IsEncrypted is a convenience over Share.Encryption / EncStatus.
func (s Share) IsEncrypted() bool { return s.Encryption != 0 || s.EncStatus != 0 }

// Shares returns the list of shared folders visible to the logged-in user.
func (c *Client) Shares(ctx context.Context) ([]Share, error) {
	params := url.Values{}
	params.Set("shareType", "all")
	params.Set("additional", `["hidden","encryption","is_aclmode","unite_permission","is_support_acl","is_sync_share","is_force_readonly","force_readonly_reason","recyclebin","share_quota","enable_share_compress","enable_share_cow","include_cold_storage_share","is_cold_storage_share","include_missing_share","is_missing_share","include_offline_share","is_offline_share","support_snapshot","share_snapshot_info","is_usb_share"]`)
	var resp struct {
		Shares []Share `json:"shares"`
		Total  int     `json:"total"`
	}
	if err := c.Call(ctx, "SYNO.Core.Share", 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Shares, nil
}
