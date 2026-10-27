package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRunVersion(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "plain", args: []string{"version"}, wantCode: exitOK},
		{name: "json", args: []string{"version", "--json"}, wantCode: exitOK},
		{name: "no command", args: nil, wantCode: exitInvalidUsage},
		{name: "unknown command", args: []string{"unknown"}, wantCode: exitInvalidUsage},
		{name: "unknown flag", args: []string{"version", "--yaml"}, wantCode: exitInvalidUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if got := run(tt.args, &out); got != tt.wantCode {
				t.Fatalf("run(%q) = %d, want %d", tt.args, got, tt.wantCode)
			}
		})
	}
}

func TestRunVersionJSONHasVendor(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"version", "--json"}, &out); code != exitOK {
		t.Fatalf("exit code %d", code)
	}
	var info versionInfo
	dec := json.NewDecoder(&out)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Vendor != vendor {
		t.Errorf("vendor = %q, want %q", info.Vendor, vendor)
	}
	if info.Protocol != protocol {
		t.Errorf("protocol = %d, want %d", info.Protocol, protocol)
	}
	if info.Version == "" {
		t.Error("version is empty")
	}
}
