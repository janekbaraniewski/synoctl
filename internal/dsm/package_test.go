package dsm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestPackageInstallUsesQueueFlow(t *testing.T) {
	t.Parallel()

	var installForm url.Values
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		api := r.Form.Get("api")
		method := r.Form.Get("method")
		methods = append(methods, api+":"+method)

		switch api + ":" + method {
		case "SYNO.Core.Package.Installation:check":
			writeDSMError(t, w, 120)
		case "SYNO.Core.Package.Installation:get_queue":
			writeDSMData(t, w, map[string]any{
				"queue": []map[string]any{
					{"pkg": "AntiVirus", "beta": false, "volume": ""},
				},
				"broken_pkgs":        []string{},
				"cause_pausing_pkgs": []string{},
				"conflicted_pkgs":    []string{},
				"non_exist_pkgs":     []string{},
				"paused_pkgs":        []string{},
			})
		case "SYNO.Core.Package.Installation:install":
			installForm = cloneValues(r.Form)
			writeDSMData(t, w, map[string]any{"progress": 1})
		case "SYNO.Core.Package:list":
			writeDSMData(t, w, map[string]any{
				"packages": []map[string]any{
					{
						"id":      "AntiVirus",
						"name":    "AntiVirus Essential",
						"version": "1.5.4-3099",
						"additional": map[string]any{
							"status":        "running",
							"maintainer":    "Synology Inc.",
							"ctl_uninstall": true,
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected DSM call %s.%s", api, method)
		}
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	err := c.PackageInstall(context.Background(), ServerPackage{
		ID:      "AntiVirus",
		Version: "1.5.4-3099",
		Source:  "syno",
		Start:   true,
		QStart:  true,
	}, nil, InstallOpts{
		PollInterval: time.Millisecond,
		Timeout:      time.Second,
	})
	if err != nil {
		t.Fatalf("PackageInstall returned error: %v", err)
	}

	wantMethods := []string{
		"SYNO.Core.Package.Installation:check",
		"SYNO.Core.Package.Installation:get_queue",
		"SYNO.Core.Package.Installation:install",
		"SYNO.Core.Package:list",
	}
	if got := jsonString(methods); got != jsonString(wantMethods) {
		t.Fatalf("methods = %s, want %s", got, jsonString(wantMethods))
	}
	if installForm.Get("name") != "AntiVirus" {
		t.Fatalf("install name = %q", installForm.Get("name"))
	}
	if installForm.Get("volume_path") != "/volume1" {
		t.Fatalf("install volume_path = %q", installForm.Get("volume_path"))
	}
	if installForm.Get("is_syno") != "true" {
		t.Fatalf("install is_syno = %q", installForm.Get("is_syno"))
	}
	if installForm.Get("installrunpackage") != "true" {
		t.Fatalf("install installrunpackage = %q", installForm.Get("installrunpackage"))
	}
}

func testClient(t *testing.T, rawURL string) *Client {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{Scheme: u.Scheme, Host: host, Port: port, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeDSMData(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data}); err != nil {
		t.Fatal(err)
	}
}

func writeDSMError(t *testing.T, w http.ResponseWriter, code int) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   map[string]any{"code": code},
	}); err != nil {
		t.Fatal(err)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for key, values := range v {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func FuzzFlexBoolUnmarshal(f *testing.F) {
	for _, seed := range []string{
		"true",
		"false",
		"1",
		"0",
		`"yes"`,
		`"no"`,
		`""`,
		"null",
		"{",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var b flexBool
		_ = json.Unmarshal([]byte(input), &b)
	})
}
