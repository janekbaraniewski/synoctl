// Package demo runs an in-process HTTP server that speaks just enough
// of the Synology DSM Web API for the synoctl TUI to render every view
// without a real NAS. The intent is screenshots for docs / README /
// release notes — every view should look populated, busy, and
// realistic so the resulting screenshots sell the product.
//
// The server is mounted at a random localhost port; the regular
// dsm.Client is pointed at it, and the TUI is launched against it as if
// it were a real DSM endpoint. Every code path that talks to DSM in
// production is exercised by the demo too — the only difference is the
// data on the wire.
//
// Adding a new view that hits a new endpoint: extend handlers.go with
// one route and data.go with the canned payload, and the existing TUI
// view will start rendering against the demo without further changes.
package demo

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Options configures the in-process demo backend.
type Options struct {
	// Seed controls the pseudo-random live metrics returned by the demo.
	// Use 0 to seed from the current time.
	Seed uint64
}

// Server is the running demo backend. Caller closes it via Close().
type Server struct {
	httpsrv *httptest.Server

	mu   sync.Mutex // guards rng
	rng  *rand.Rand
	data *state // mutable in-memory state — snapshots created during the demo, etc.
}

// New constructs and starts a demo server bound to a random localhost
// port. The returned URL has the form "127.0.0.1:NNNN" (no scheme),
// matching what dsm.Options.Host expects.
//
// Call Close when done. The server is stateful in-process: snapshots
// created via the TUI are remembered for the lifetime of the demo,
// so a screenshot showing the "after" of a snapshot-create flow
// works without restarting.
func New(options ...Options) *Server {
	opts := Options{Seed: 1}
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.Seed == 0 {
		opts.Seed = uint64(time.Now().UnixNano())
	}
	s := &Server{
		data: newState(),
		rng:  rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/webapi/entry.cgi", s.entryHandler)
	mux.HandleFunc("/webapi/auth.cgi", s.entryHandler) // login routes here
	mux.HandleFunc("/webapi/", s.entryHandler)         // catch-all for path-routed APIs
	s.httpsrv = httptest.NewServer(mux)
	return s
}

func (s *Server) intN(n int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rng.IntN(n)
}

// HostPort returns "host:port" the dsm.Client should be configured
// against (scheme is always http for the demo server).
func (s *Server) HostPort() string {
	u, _ := url.Parse(s.httpsrv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	return fmt.Sprintf("%s:%s", host, port)
}

// Close tears down the listener. Safe to call multiple times.
func (s *Server) Close() {
	if s.httpsrv != nil {
		s.httpsrv.Close()
	}
}

// — request plumbing —

func (s *Server) entryHandler(w http.ResponseWriter, r *http.Request) {
	// DSM uses POST application/x-www-form-urlencoded for almost
	// everything. We parse the form, look up a handler keyed by
	// api:method, and either return its data wrapped in the DSM
	// success envelope or a {success: false, error.code} envelope.
	if err := r.ParseForm(); err != nil {
		writeErr(w, 100)
		return
	}
	api := firstNonEmpty(r.Form.Get("api"), r.URL.Query().Get("api"))
	method := firstNonEmpty(r.Form.Get("method"), r.URL.Query().Get("method"))
	key := api + ":" + method
	handler, ok := s.handlers()[key]
	if !ok {
		// Unknown API — return a DSM-style 102 (API does not exist).
		// The TUI treats this as a soft "feature not available" and
		// renders an empty-state on a lot of views, which is fine for
		// demo purposes.
		writeErr(w, 102)
		return
	}
	resp := handler(s, r.Form)
	writeOK(w, resp)
}

func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	body, err := json.Marshal(map[string]any{
		"success": true,
		"data":    data,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

func writeErr(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	body, _ := json.Marshal(map[string]any{
		"success": false,
		"error":   map[string]any{"code": code},
	})
	_, _ = w.Write(body)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// — state —

// state is the mutable in-memory store. snapshots/users/services can be
// mutated by the demo session (e.g. creating a snapshot via the TUI),
// so they live here behind a mutex. Read-only canned data stays in
// data.go as Go literals.
type state struct {
	mu sync.Mutex

	snapshots map[string][]snapshot // share -> snapshots
	services  map[string]string     // service id -> state (enabled / disabled / always-on)
	pkgState  map[string]string     // package id -> running / stop
	pkgExtra  map[string]map[string]any

	// taskID counter for DirSize.start / .status pair.
	dirSizeTasks map[string]dirSizeTask
	nextTaskID   int

	startedAt time.Time
}

func newState() *state {
	st := &state{
		snapshots:    make(map[string][]snapshot),
		services:     make(map[string]string),
		pkgState:     make(map[string]string),
		pkgExtra:     make(map[string]map[string]any),
		dirSizeTasks: make(map[string]dirSizeTask),
		startedAt:    time.Now(),
	}
	// Seed snapshots for the photo share so the Snapshots overlay has
	// something to render right away.
	st.snapshots["photo"] = []snapshot{
		{Name: "GMT+02-2026.05.18-04.00.00", Time: time.Now().Add(-12 * time.Hour).Unix(), Description: "daily auto"},
		{Name: "GMT+02-2026.05.17-04.00.00", Time: time.Now().Add(-36 * time.Hour).Unix(), Description: "daily auto"},
		{Name: "GMT+02-2026.05.16-04.00.00", Time: time.Now().Add(-60 * time.Hour).Unix(), Description: "daily auto", Schedule: true},
		{Name: "GMT+02-2026.05.10-19.42.11", Time: time.Now().Add(-9 * 24 * time.Hour).Unix(), Description: "before family wedding", Locked: true},
	}
	st.snapshots["video"] = []snapshot{
		{Name: "GMT+02-2026.05.15-04.00.00", Time: time.Now().Add(-3 * 24 * time.Hour).Unix(), Schedule: true},
	}
	for id, on := range demoServicesSeed {
		st.services[id] = on
	}
	for id, st0 := range demoPackagesStateSeed {
		st.pkgState[id] = st0
	}
	return st
}

type dirSizeTask struct {
	finished bool
	at       time.Time
	total    int64
	numDirs  int64
	numFiles int64
}

type snapshot struct {
	Name        string `json:"name"`
	Time        int64  `json:"time,omitempty"`
	Description string `json:"desc,omitempty"`
	Locked      bool   `json:"lock,omitempty"`
	Schedule    bool   `json:"schedule_snapshot,omitempty"`
}

// — uptime helper —

// uptimeStringDSM returns DSM's "dd:hh:mm:ss" format computed from
// startedAt (offset by a hard-coded base so the demo always looks
// like the box has been up for >5 months — more realistic for a
// lab NAS).
func (s *Server) uptimeStringDSM() string {
	const baseDays = 169
	d := time.Since(s.data.startedAt)
	totalSeconds := int64(baseDays*86400) + int64(d.Seconds())
	days := totalSeconds / 86400
	rem := totalSeconds % 86400
	hours := rem / 3600
	rem %= 3600
	mins := rem / 60
	secs := rem % 60
	return fmt.Sprintf("%d:%02d:%02d:%02d", days, hours, mins, secs)
}

// jsonValue is a tiny helper for building inline json.RawMessage from
// Go literals — used by handlers that need to nest unstructured DSM
// shapes (typed structs from internal/dsm/*.go aren't accessible from
// here without an import cycle, so we lean on raw maps).
func jsonValue(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// trimWS reduces whitespace in form-encoded JSON arrays so handlers can
// match user input regardless of how the dsm client encoded the request.
func trimWS(s string) string {
	return strings.Join(strings.Fields(s), "")
}
