package dsm

import (
	"context"
)

// Certificate is one entry from SYNO.Core.Certificate.list — a TLS
// certificate installed on the DSM (system default, package-specific,
// or Let's Encrypt). valid_from / valid_to are formatted strings on
// DSM 7.x; legacy DSM 6 used epoch seconds in unrelated fields.
type Certificate struct {
	ID              string   `json:"id"`
	Description     string   `json:"desc,omitempty"`
	Subject         string   `json:"subject_common_name,omitempty"`
	Issuer          string   `json:"issuer_common_name,omitempty"`
	IssuerOrg       string   `json:"issuer_organization,omitempty"`
	SubjectAltName  []string `json:"subject_alt_name,omitempty"`
	SubjectAltNameS string   `json:"subjectAltName,omitempty"` // joined variant on some builds
	ValidFrom       string   `json:"valid_from"`
	ValidTo         string   `json:"valid_till"`
	SignatureAlgo   string   `json:"signature_algorithm,omitempty"`
	KeyType         string   `json:"key_type,omitempty"`
	Default         flexBool `json:"is_default,omitempty"`
	Broken          flexBool `json:"is_broken,omitempty"`
	UserDeletable   flexBool `json:"user_deletable,omitempty"`
	RenewURL        string   `json:"acme_url,omitempty"`
	Services        []struct {
		Service  string `json:"service"`
		Display  string `json:"display_name,omitempty"`
		Owner    string `json:"owner,omitempty"`
		Subscriber string `json:"subscriber,omitempty"`
	} `json:"services,omitempty"`
}

// Certificates lists installed TLS certificates via SYNO.Core.Certificate
// "list" v1. Includes the system default plus any package-bound certs.
func (c *Client) Certificates(ctx context.Context) ([]Certificate, error) {
	const api = "SYNO.Core.Certificate"
	if !c.Supports(api) {
		return []Certificate{}, nil
	}
	var resp struct {
		Certificates []Certificate `json:"certificates"`
		Total        int           `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Certificates, nil
}
