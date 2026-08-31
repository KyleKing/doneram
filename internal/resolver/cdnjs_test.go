package resolver

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

func TestCDNJSResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/libraries/jquery": testutil.FixtureHandler("api/cdnjs/jquery.json"),
	})
	defer server.Close()

	r := NewCDNJSResolverWithBaseURL(&http.Client{}, server.URL)

	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{"latest", "#.#.#", "3.7.1"},
		{"pinned minor", "3.6.#", "3.6.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Resolve(context.Background(), "jquery", parser.ParsePattern(tt.pattern))
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCDNJSResolver_NotFound(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/libraries/nonexistent": testutil.ErrorHandler(404),
	})
	defer server.Close()

	r := NewCDNJSResolverWithBaseURL(&http.Client{}, server.URL)
	if _, err := r.Resolve(context.Background(), "nonexistent", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("expected error for nonexistent library")
	}
}

func TestCDNJSResolver_DetailWarnsOnDisagreement(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/libraries/jquery":             testutil.FixtureHandler("api/cdnjs/jquery.json"),
		"/repos/jquery/jquery/releases": testutil.FixtureHandler("api/github/releases.json"),
	})
	defer server.Close()

	r := NewCDNJSResolverWithBaseURL(&http.Client{}, server.URL)
	detail, err := r.Detail(context.Background(), "jquery@jquery/jquery", "3.7.0", "3.7.1")
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if !strings.Contains(detail, "warning") || !strings.Contains(detail, "3.1.0") {
		t.Errorf("Detail() = %q, want a disagreement warning naming GitHub's 3.1.0", detail)
	}
}

func TestCDNJSResolver_DetailNoPeerRepo(t *testing.T) {
	r := NewCDNJSResolver(&http.Client{})
	detail, err := r.Detail(context.Background(), "jquery", "3.7.0", "3.7.1")
	if err != nil || detail != "" {
		t.Errorf("Detail() = (%q, %v), want empty and no error without a paired repo", detail, err)
	}
}

func TestCDNJSResolver_Name(t *testing.T) {
	r := NewCDNJSResolver(&http.Client{})
	if r.Name() != "cdnjs" {
		t.Errorf("Name() = %s, want cdnjs", r.Name())
	}
}
