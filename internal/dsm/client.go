// Package dsm is a typed client for the Synology DSM Web API.
package dsm

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to a single DSM endpoint.
type Client struct {
	baseURL  *url.URL
	http     *http.Client
	insecure bool

	mu         sync.RWMutex
	sid        string             // session id from login
	deviceID   string             // returned when enable_device_token=yes
	synoToken  string             // CSRF token, if requested
	apiPaths   map[string]apiInfo // populated by Info()
	infoDoneAt time.Time
}

type apiInfo struct {
	Path       string `json:"path"`
	MinVersion int    `json:"minVersion"`
	MaxVersion int    `json:"maxVersion"`
}

// Options configures the client.
type Options struct {
	Scheme   string        // "http" or "https"; defaults to https
	Host     string        // hostname or IP, no port
	Port     int           // DSM port; defaults to 5001 (https) or 5000 (http)
	Insecure bool          // skip TLS verification (self-signed certs)
	Timeout  time.Duration // per-request timeout; default 20s
}

// New constructs a Client. The returned client is unauthenticated; call
// Login before issuing other calls.
func New(opts Options) (*Client, error) {
	scheme := opts.Scheme
	if scheme == "" {
		scheme = "https"
	}
	port := opts.Port
	if port == 0 {
		if scheme == "https" {
			port = 5001
		} else {
			port = 5000
		}
	}
	if opts.Host == "" {
		return nil, fmt.Errorf("dsm: host is required")
	}
	base, err := url.Parse(fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(opts.Host, strconv.Itoa(port))))
	if err != nil {
		return nil, fmt.Errorf("dsm: invalid host: %w", err)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.Insecure}, //nolint:gosec // user opt-in; warned in TUI
		Proxy:           http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &Client{
		baseURL:  base,
		insecure: opts.Insecure,
		http: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
		apiPaths: map[string]apiInfo{},
	}, nil
}

// Host returns the host:port of the configured endpoint.
func (c *Client) Host() string { return c.baseURL.Host }

// Insecure reports whether TLS verification is disabled.
func (c *Client) Insecure() bool { return c.insecure }

// SID returns the current session id (empty if not logged in).
func (c *Client) SID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sid
}

// Authenticated reports whether a SID is in place.
func (c *Client) Authenticated() bool { return c.SID() != "" }

// envelope mirrors the standard DSM response wrapper.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code   int             `json:"code"`
		Errors json.RawMessage `json:"errors,omitempty"`
	} `json:"error,omitempty"`
}

// Call performs a DSM API request and decodes the data field into out.
// Pass nil for out to discard the response body.
func (c *Client) Call(ctx context.Context, api string, version int, method string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("api", api)
	params.Set("version", strconv.Itoa(version))
	params.Set("method", method)
	if sid := c.SID(); sid != "" {
		params.Set("_sid", sid)
	}

	path := c.pathFor(api)
	endpoint := *c.baseURL
	endpoint.Path = path

	// POST keeps long param lists / passwords out of URLs.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if tok := c.token(); tok != "" {
		req.Header.Set("X-SYNO-TOKEN", tok)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dsm: %s.%s: %w", api, method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("dsm: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dsm: %s.%s: http %d: %s", api, method, resp.StatusCode, truncate(string(body), 200))
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("dsm: parse envelope: %w (body: %s)", err, truncate(string(body), 200))
	}
	if !env.Success {
		code := 0
		if env.Error != nil {
			code = env.Error.Code
		}
		return &Error{Code: code, API: api, Method: method}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.synoToken
}

func (c *Client) pathFor(api string) string {
	c.mu.RLock()
	info, ok := c.apiPaths[api]
	c.mu.RUnlock()
	if ok && info.Path != "" {
		return "webapi/" + info.Path
	}
	// Auth is reachable on auth.cgi historically; everything else on entry.cgi.
	if api == "SYNO.API.Auth" {
		return "webapi/auth.cgi"
	}
	return "webapi/entry.cgi"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
