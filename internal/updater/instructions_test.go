package updater

import (
	"testing"

	"github.com/kyleking/doner/internal/parser"
)

func TestUpdateFromInstructions(t *testing.T) {
	instructions := []parser.Instruction{
		{Command: "FROM", Args: "alpine:3.19 AS base", Line: 2},
		{Command: "COPY", Args: "--from=ghcr.io/kyleking/doner:1.0.0 /a /b", Line: 4},
	}
	directives := map[int]*parser.Directive{
		1: {Packages: []parser.PackageDirective{{Name: "alpine"}}},
		3: {Packages: []parser.PackageDirective{{Name: "doner"}}},
	}
	resolved := map[int]string{2: "3.20.0", 4: "1.2.0"}

	updates := UpdateFromInstructions(instructions, directives, resolved)
	if len(updates) != 2 {
		t.Fatalf("got %d updates, want 2: %+v", len(updates), updates)
	}

	if updates[0] != (Update{Package: "alpine", Source: "docker", OldVersion: "3.19", NewVersion: "3.20.0", Line: 2}) {
		t.Errorf("FROM update = %+v", updates[0])
	}
	if updates[1].Source != "ghcr" {
		t.Errorf("COPY --from update source = %q, want ghcr", updates[1].Source)
	}
	if updates[1].OldVersion != "1.0.0" || updates[1].NewVersion != "1.2.0" {
		t.Errorf("COPY --from update versions = %q -> %q, want 1.0.0 -> 1.2.0",
			updates[1].OldVersion, updates[1].NewVersion)
	}
}

func TestUpdateFromInstructionsSkips(t *testing.T) {
	instructions := []parser.Instruction{
		{Command: "FROM", Args: "alpine:3.19", Line: 2},
		{Command: "FROM", Args: "debian:12", Line: 4},
		{Command: "FROM", Args: "ubuntu:24.04", Line: 6},
		{Command: "FROM", Args: "busybox:1.36", Line: 8},
		{Command: "RUN", Args: "apk add curl", Line: 10},
	}
	directives := map[int]*parser.Directive{
		1: nil,
		3: {Ignore: true},
		5: {Packages: []parser.PackageDirective{{Name: "ubuntu"}}},
		7: {Packages: []parser.PackageDirective{{Name: "busybox"}}},
		9: {Packages: []parser.PackageDirective{{Name: "curl"}}},
	}
	resolved := map[int]string{
		2:  "3.20.0",
		4:  "13",
		6:  "",
		8:  "1.36",
		10: "8.5.0",
	}

	if updates := UpdateFromInstructions(instructions, directives, resolved); len(updates) != 0 {
		t.Errorf("got %+v, want no updates", updates)
	}
}
