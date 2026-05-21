package cli

import "testing"

func TestDemoCommandRecordingDefaults(t *testing.T) {
	cmd := newDemoCmd()
	hostFlag := cmd.Flags().Lookup("host-label")
	if hostFlag == nil {
		t.Fatal("missing --host-label flag")
	}
	if hostFlag.DefValue != defaultDemoHostLabel {
		t.Fatalf("--host-label default = %q, want %q", hostFlag.DefValue, defaultDemoHostLabel)
	}
	seedFlag := cmd.Flags().Lookup("seed")
	if seedFlag == nil {
		t.Fatal("missing --seed flag")
	}
	if seedFlag.DefValue != "1" {
		t.Fatalf("--seed default = %q, want 1", seedFlag.DefValue)
	}
}
