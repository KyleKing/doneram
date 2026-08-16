package cli

import "testing"

func TestParseImageFromArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        string
		wantImage   string
		wantVersion string
	}{
		{"plain", "alpine:3.19", "alpine", "3.19"},
		{"upper alias", "alpine:3.19 AS base", "alpine", "3.19"},
		{"lower alias", "alpine:3.19 as base", "alpine", "3.19"},
		{"registry with port", "registry:5000/alpine:3.19", "registry:5000/alpine", "3.19"},
		{"ghcr", "ghcr.io/kyleking/doneram:v1.2.3", "ghcr.io/kyleking/doneram", "v1.2.3"},
		{"no tag", "alpine", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image, version := parseImageFromArgs(tt.args)
			if image != tt.wantImage || version != tt.wantVersion {
				t.Errorf("parseImageFromArgs(%q) = (%q, %q), want (%q, %q)",
					tt.args, image, version, tt.wantImage, tt.wantVersion)
			}
		})
	}
}

func TestParseImageFromCopyArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        string
		wantImage   string
		wantVersion string
	}{
		{"trailing paths", "--from=alpine:3.19 /bin/sh /bin/sh", "alpine", "3.19"},
		{"end of line", "--from=alpine:3.19", "alpine", "3.19"},
		{"ghcr", "--from=ghcr.io/kyleking/doneram:v1 /a /b", "ghcr.io/kyleking/doneram", "v1"},
		{"stage alias", "--from=builder /a /b", "", ""},
		{"no from flag", "/a /b", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image, version := parseImageFromCopyArgs(tt.args)
			if image != tt.wantImage || version != tt.wantVersion {
				t.Errorf("parseImageFromCopyArgs(%q) = (%q, %q), want (%q, %q)",
					tt.args, image, version, tt.wantImage, tt.wantVersion)
			}
		})
	}
}

func TestContainsGHCR(t *testing.T) {
	if !containsGHCR("ghcr.io/kyleking/doneram") {
		t.Error("containsGHCR should match a ghcr.io image")
	}
	if containsGHCR("alpine") {
		t.Error("containsGHCR should not match a Docker Hub image")
	}
}
