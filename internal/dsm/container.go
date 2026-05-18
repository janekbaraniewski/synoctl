package dsm

import (
	"context"
	"net/url"
)

// Container is one row from SYNO.Docker.Container.list — a Container
// Manager container as DSM models it. Some flag fields arrive as 0/1
// instead of bool on older Container Manager builds; flexBool tolerates
// both. Container Manager replaced the "Docker" package name in DSM 7.2
// but kept the legacy SYNO.Docker.* API namespace.
type Container struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	Status      string   `json:"status"`           // running / stop / paused
	State       string   `json:"state,omitempty"`  // alt: created / running / exited
	IsPackage   flexBool `json:"is_package,omitempty"`
	Command     string   `json:"command,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	StartedAt   string   `json:"started_at,omitempty"`
	FinishedAt  string   `json:"finished_at,omitempty"`
	CPU         float64  `json:"cpu,omitempty"`        // percent
	Memory      int64    `json:"memory,omitempty"`     // bytes
	MemoryPct   float64  `json:"memory_percent,omitempty"`
	NetworkUp   int64    `json:"network_up,omitempty"` // bytes
	NetworkDown int64    `json:"network_down,omitempty"`
	BlockIn     int64    `json:"block_in,omitempty"`
	BlockOut    int64    `json:"block_out,omitempty"`
	Enabled     flexBool `json:"enabled,omitempty"`
}

// Containers lists Container Manager containers via SYNO.Docker.Container
// "list" v1. Returns an empty slice (and nil error) when the device does
// not advertise SYNO.Docker.Container — Container Manager is an optional
// package and may not be installed.
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	const api = "SYNO.Docker.Container"
	if !c.Supports(api) {
		return []Container{}, nil
	}
	params := url.Values{}
	params.Set("limit", "-1")
	params.Set("offset", "0")
	var resp struct {
		Containers []Container `json:"containers"`
		Total      int         `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Containers, nil
}

// Image is one entry from SYNO.Docker.Image.list — a container image
// stored locally on the DSM. Repository/tag are split on DSM 7.x; some
// older firmware returns them joined in `repotag`.
type Image struct {
	ID         string   `json:"id"`
	Repository string   `json:"repository,omitempty"`
	Tag        string   `json:"tag,omitempty"`
	RepoTag    string   `json:"repotag,omitempty"`
	Size       int64    `json:"size,omitempty"` // bytes
	VirtualSize int64   `json:"virtual_size,omitempty"`
	Created    int64    `json:"created,omitempty"` // epoch seconds
	Containers int      `json:"containers,omitempty"`
	InUse      flexBool `json:"in_use,omitempty"`
	Description string  `json:"description,omitempty"`
}

// DockerImages lists locally stored container images via SYNO.Docker.Image
// "list" v1. Returns an empty slice (and nil error) when the device does
// not advertise SYNO.Docker.Image.
func (c *Client) DockerImages(ctx context.Context) ([]Image, error) {
	const api = "SYNO.Docker.Image"
	if !c.Supports(api) {
		return []Image{}, nil
	}
	params := url.Values{}
	params.Set("limit", "-1")
	params.Set("offset", "0")
	params.Set("show_dsm", "false")
	var resp struct {
		Images []Image `json:"images"`
		Total  int     `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Images, nil
}

// DockerNetwork is one entry from SYNO.Docker.Network.list — a Container
// Manager / Docker network. `driver` is typically bridge / host / macvlan.
type DockerNetwork struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Driver     string   `json:"driver,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Subnet     string   `json:"subnet,omitempty"`
	Gateway    string   `json:"gateway,omitempty"`
	IPRange    string   `json:"ip_range,omitempty"`
	EnableIPv6 flexBool `json:"enable_ipv6,omitempty"`
	Internal   flexBool `json:"internal,omitempty"`
	Containers int      `json:"containers,omitempty"`
}

// DockerNetworks lists Container Manager networks via SYNO.Docker.Network
// "list" v1. Returns an empty slice (and nil error) when the device does
// not advertise SYNO.Docker.Network.
func (c *Client) DockerNetworks(ctx context.Context) ([]DockerNetwork, error) {
	const api = "SYNO.Docker.Network"
	if !c.Supports(api) {
		return []DockerNetwork{}, nil
	}
	params := url.Values{}
	params.Set("limit", "-1")
	params.Set("offset", "0")
	var resp struct {
		Networks []DockerNetwork `json:"networks"`
		Total    int             `json:"total"`
	}
	if err := c.Call(ctx, api, 1, "list", params, &resp); err != nil {
		return nil, err
	}
	return resp.Networks, nil
}

