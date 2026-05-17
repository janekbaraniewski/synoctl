package cli

import (
	"fmt"
	"os"
	"strings"
)

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func hostnameOr(fallback string) string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return fallback
	}
	return h
}

func notEmpty(field string) func(s string) error {
	return func(s string) error {
		if s == "" {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
}

// isProtocolMismatch detects the two common cross-scheme errors a DSM client
// produces when http and https are accidentally swapped.
func isProtocolMismatch(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "server gave HTTP response to HTTPS client"):
		return true
	case strings.Contains(s, "tls: first record does not look like a TLS handshake"):
		return true
	case strings.Contains(s, "malformed HTTP response"):
		return true
	}
	return false
}

// flipScheme returns the alternate scheme+default-port pair.
func flipScheme(scheme string, port int) (string, int) {
	if scheme == "http" {
		newPort := port
		if port == 5000 {
			newPort = 5001
		}
		return "https", newPort
	}
	newPort := port
	if port == 5001 {
		newPort = 5000
	}
	return "http", newPort
}
func splitN(s, sep string, n int) []string {
	parts := strings.SplitN(s, sep, n)
	for len(parts) < n {
		parts = append(parts, "")
	}
	return parts
}
