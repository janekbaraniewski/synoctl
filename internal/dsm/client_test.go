package dsm

import (
	"testing"
)

func TestClient_Options(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"default", Options{Host: "nas"}, "https://nas:5001/"},
		{"http", Options{Host: "nas", Scheme: "http"}, "http://nas:5000/"},
		{"custom_port", Options{Host: "nas", Port: 8080}, "https://nas:8080/"},
	}
	for _, tt := range tests {
		c, err := New(tt.opts)
		if err != nil {
			t.Fatal(err)
		}
		if c.baseURL.String() != tt.want {
			t.Errorf("New() = %s, want %s", c.baseURL.String(), tt.want)
		}
	}
}

func TestClient_HostDisplayOverride(t *testing.T) {
	c, err := New(Options{Host: "127.0.0.1", Port: 49152, DisplayHost: "demo-nas.local:5000"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Host(); got != "demo-nas.local:5000" {
		t.Fatalf("Host() = %q, want demo-nas.local:5000", got)
	}
	if got := c.baseURL.Host; got != "127.0.0.1:49152" {
		t.Fatalf("baseURL.Host = %q, want 127.0.0.1:49152", got)
	}
}

func TestClient_Authenticated(t *testing.T) {
	c, _ := New(Options{Host: "nas"})
	if c.Authenticated() {
		t.Error("Expected unauthenticated client")
	}
	c.sid = "secret"
	if !c.Authenticated() {
		t.Error("Expected authenticated client")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 3) != "hel…" {
		t.Errorf("got %s", truncate("hello", 3))
	}
	if truncate("hi", 5) != "hi" {
		t.Errorf("got %s", truncate("hi", 5))
	}
}
