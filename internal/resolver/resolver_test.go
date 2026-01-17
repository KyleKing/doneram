package resolver

import (
	"context"
	"testing"

	"github.com/kyleking/doner/internal/parser"
)

func TestPyPIResolver_Resolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	r := NewPyPIResolver()
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		pattern *parser.VersionPattern
		wantErr bool
	}{
		{
			name:    "resolve requests package",
			pkg:     "requests",
			pattern: parser.ParsePattern("#.#.#"),
			wantErr: false,
		},
		{
			name:    "resolve flask with pattern",
			pkg:     "flask",
			pattern: parser.ParsePattern("3.#.#"),
			wantErr: false,
		},
		{
			name:    "invalid package",
			pkg:     "this-package-does-not-exist-12345",
			pattern: parser.ParsePattern("#.#.#"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := r.Resolve(ctx, tt.pkg, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && version == "" {
				t.Error("Resolve() returned empty version")
			}
			if !tt.wantErr {
				t.Logf("Resolved %s to version %s", tt.pkg, version)
			}
		})
	}
}

func TestNPMResolver_Resolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	r := NewNPMResolver()
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		pattern *parser.VersionPattern
		wantErr bool
	}{
		{
			name:    "resolve express package",
			pkg:     "express",
			pattern: parser.ParsePattern("#.#.#"),
			wantErr: false,
		},
		{
			name:    "resolve lodash with pattern",
			pkg:     "lodash",
			pattern: parser.ParsePattern("4.#.#"),
			wantErr: false,
		},
		{
			name:    "invalid package",
			pkg:     "this-npm-package-does-not-exist-12345",
			pattern: parser.ParsePattern("#.#.#"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := r.Resolve(ctx, tt.pkg, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && version == "" {
				t.Error("Resolve() returned empty version")
			}
			if !tt.wantErr {
				t.Logf("Resolved %s to version %s", tt.pkg, version)
			}
		})
	}
}

func TestAPKResolver_Resolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	r := NewAPKResolver()
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		pattern *parser.VersionPattern
		wantErr bool
	}{
		{
			name:    "resolve bash package",
			pkg:     "bash",
			pattern: parser.ParsePattern("#.#.#"),
			wantErr: false,
		},
		{
			name:    "resolve curl with pattern",
			pkg:     "curl",
			pattern: parser.ParsePattern("8.#.#"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := r.Resolve(ctx, tt.pkg, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && version == "" {
				t.Error("Resolve() returned empty version")
			}
			if !tt.wantErr {
				t.Logf("Resolved %s to version %s", tt.pkg, version)
			}
		})
	}
}

func TestAPTResolver_Resolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	r := NewAPTResolver()
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		pattern *parser.VersionPattern
		wantErr bool
	}{
		{
			name:    "resolve curl package",
			pkg:     "curl",
			pattern: parser.ParsePattern("#.#.#"),
			wantErr: false,
		},
		{
			name:    "resolve wget with pattern",
			pkg:     "wget",
			pattern: parser.ParsePattern("1.#.#"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := r.Resolve(ctx, tt.pkg, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && version == "" {
				t.Error("Resolve() returned empty version")
			}
			if !tt.wantErr {
				t.Logf("Resolved %s to version %s", tt.pkg, version)
			}
		})
	}
}
