package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/internal/testutil"
)

func TestPyPIResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/pypi/requests/json": testutil.FixtureHandler("api/pypi/requests.json"),
	})
	defer server.Close()

	r := NewPyPIResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "latest version",
			pkg:     "requests",
			pattern: "#.#.#",
			want:    "2.31.1",
			wantErr: false,
		},
		{
			name:    "specific major version",
			pkg:     "requests",
			pattern: "2.#.#",
			want:    "2.31.1",
			wantErr: false,
		},
		{
			name:    "specific minor version",
			pkg:     "requests",
			pattern: "2.31.#",
			want:    "2.31.1",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := parser.ParsePattern(tt.pattern)
			got, err := r.Resolve(ctx, tt.pkg, pattern)

			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("Resolve() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNPMResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/express": testutil.FixtureHandler("api/npm/express.json"),
	})
	defer server.Close()

	r := NewNPMResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	tests := []struct {
		name    string
		pkg     string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "latest version",
			pkg:     "express",
			pattern: "#.#.#",
			want:    "4.19.0",
			wantErr: false,
		},
		{
			name:    "specific major version",
			pkg:     "express",
			pattern: "4.#.#",
			want:    "4.19.0",
			wantErr: false,
		},
		{
			name:    "specific minor version",
			pkg:     "express",
			pattern: "4.18.#",
			want:    "4.18.2",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := parser.ParsePattern(tt.pattern)
			got, err := r.Resolve(ctx, tt.pkg, pattern)

			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("Resolve() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPyPIResolver_NotFound(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/pypi/nonexistent/json": testutil.ErrorHandler(404),
	})
	defer server.Close()

	r := NewPyPIResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("#.#.#")
	_, err := r.Resolve(ctx, "nonexistent", pattern)

	if err == nil {
		t.Error("expected error for nonexistent package, got nil")
	}
}

func TestNPMResolver_NotFound(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/nonexistent": testutil.ErrorHandler(404),
	})
	defer server.Close()

	r := NewNPMResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("#.#.#")
	_, err := r.Resolve(ctx, "nonexistent", pattern)

	if err == nil {
		t.Error("expected error for nonexistent package, got nil")
	}
}

func TestPyPIResolver_RateLimit(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/pypi/requests/json": testutil.RateLimitHandler(60),
	})
	defer server.Close()

	r := NewPyPIResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("#.#.#")
	_, err := r.Resolve(ctx, "requests", pattern)

	if err == nil {
		t.Error("expected error for rate limit, got nil")
	}
}

func TestNPMResolver_RateLimit(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/express": testutil.RateLimitHandler(60),
	})
	defer server.Close()

	r := NewNPMResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("#.#.#")
	_, err := r.Resolve(ctx, "express", pattern)

	if err == nil {
		t.Error("expected error for rate limit, got nil")
	}
}

func TestAPKResolver_NotFound(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/api/v1/project/nonexistent": testutil.ErrorHandler(404),
	})
	defer server.Close()

	r := NewAPKResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("#.#.#")
	_, err := r.Resolve(ctx, "nonexistent", pattern)

	if err == nil {
		t.Error("expected error for nonexistent package, got nil")
	}
}

func TestAPTResolver_NotFound(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/api/v1/project/nonexistent": testutil.ErrorHandler(404),
	})
	defer server.Close()

	r := NewAPTResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("#.#.#")
	_, err := r.Resolve(ctx, "nonexistent", pattern)

	if err == nil {
		t.Error("expected error for nonexistent package, got nil")
	}
}

func TestPyPIResolver_NoMatchingVersion(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/pypi/requests/json": testutil.FixtureHandler("api/pypi/requests.json"),
	})
	defer server.Close()

	r := NewPyPIResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("99.#.#")
	_, err := r.Resolve(ctx, "requests", pattern)

	if err == nil {
		t.Error("expected error for no matching version, got nil")
	}
}

func TestNPMResolver_NoMatchingVersion(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/express": testutil.FixtureHandler("api/npm/express.json"),
	})
	defer server.Close()

	r := NewNPMResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	pattern := parser.ParsePattern("99.#.#")
	_, err := r.Resolve(ctx, "express", pattern)

	if err == nil {
		t.Error("expected error for no matching version, got nil")
	}
}

func TestDockerHubResolver_Name(t *testing.T) {
	r := NewDockerHubResolver(&http.Client{})
	if r.Name() != "dockerhub" {
		t.Errorf("Name() = %s, want dockerhub", r.Name())
	}
}

func TestGHCRResolver_Name(t *testing.T) {
	r := NewGHCRResolver(&http.Client{})
	if r.Name() != "ghcr" {
		t.Errorf("Name() = %s, want ghcr", r.Name())
	}
}

func TestPyPIResolver_Name(t *testing.T) {
	r := NewPyPIResolver(&http.Client{})
	if r.Name() != "pypi" {
		t.Errorf("Name() = %s, want pypi", r.Name())
	}
}

func TestNPMResolver_Name(t *testing.T) {
	r := NewNPMResolver(&http.Client{})
	if r.Name() != "npm" {
		t.Errorf("Name() = %s, want npm", r.Name())
	}
}

func TestAPKResolver_Name(t *testing.T) {
	r := NewAPKResolver(&http.Client{})
	if r.Name() != "apk" {
		t.Errorf("Name() = %s, want apk", r.Name())
	}
}

func TestAPTResolver_Name(t *testing.T) {
	r := NewAPTResolver(&http.Client{})
	if r.Name() != "apt" {
		t.Errorf("Name() = %s, want apt", r.Name())
	}
}

func TestCargoResolver_Name(t *testing.T) {
	r := NewCargoResolver(&http.Client{})
	if r.Name() != "cargo" {
		t.Errorf("Name() = %s, want cargo", r.Name())
	}
}

func TestRubyGemsResolver_Name(t *testing.T) {
	r := NewRubyGemsResolver(&http.Client{})
	if r.Name() != "rubygems" {
		t.Errorf("Name() = %s, want rubygems", r.Name())
	}
}

func TestComposerResolver_Name(t *testing.T) {
	r := NewComposerResolver(&http.Client{})
	if r.Name() != "composer" {
		t.Errorf("Name() = %s, want composer", r.Name())
	}
}

func TestYumResolver_Name(t *testing.T) {
	r := NewYumResolver(&http.Client{})
	if r.Name() != "yum" {
		t.Errorf("Name() = %s, want yum", r.Name())
	}
}
