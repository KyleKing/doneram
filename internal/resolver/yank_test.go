package resolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
)

func serveJSON(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestPyPIYanked(t *testing.T) {
	srv, hits := serveJSON(t, `{"releases":{
		"1.0.0":[{"yanked":false}],
		"1.1.0":[{"yanked":true,"yanked_reason":"broken wheel"},{"yanked":true}],
		"1.0.1":[{"yanked":true},{"yanked":false}]}}`)
	r := NewPyPIResolverWithBaseURL(srv.Client(), srv.URL)
	ctx := context.Background()

	got, err := r.Resolve(ctx, "pkg", parser.ParsePattern("#.#.#"))
	if err != nil || got != "1.0.1" {
		t.Errorf("Resolve = %q, %v; want 1.0.1 (1.1.0 is yanked, 1.0.1 only partly)", got, err)
	}
	reason, yanked, err := r.Yanked(ctx, "pkg", "1.1.0")
	if err != nil || !yanked || reason != "broken wheel" {
		t.Errorf("Yanked(1.1.0) = %q, %v, %v", reason, yanked, err)
	}
	if _, yanked, _ := r.Yanked(ctx, "pkg", "1.0.0"); yanked {
		t.Error("1.0.0 should not be yanked")
	}
	if *hits != 1 {
		t.Errorf("fetched %d times, want 1", *hits)
	}
}

func TestNPMDeprecated(t *testing.T) {
	srv, _ := serveJSON(t, `{"versions":{
		"2.0.0":{"deprecated":"critical bug, use 2.0.1"},
		"1.9.0":{"deprecated":false},
		"1.8.0":{}}}`)
	r := NewNPMResolverWithBaseURL(srv.Client(), srv.URL)
	ctx := context.Background()

	got, err := r.Resolve(ctx, "pkg", parser.ParsePattern("#.#.#"))
	if err != nil || got != "1.9.0" {
		t.Errorf("Resolve = %q, %v; want 1.9.0", got, err)
	}
	reason, yanked, _ := r.Yanked(ctx, "pkg", "2.0.0")
	if !yanked || reason != "deprecated: critical bug, use 2.0.1" {
		t.Errorf("Yanked(2.0.0) = %q, %v", reason, yanked)
	}

	got, _ = r.Resolve(ctx, "pkg", parser.ParsePattern("2.#.#"))
	if got != "2.0.0" {
		t.Errorf("Resolve(2.#.#) = %q, want the deprecated 2.0.0 when nothing else matches", got)
	}
}

func TestCargoYanked(t *testing.T) {
	srv, _ := serveJSON(t, `{"versions":[{"num":"0.2.0","yanked":true},{"num":"0.1.0","yanked":false}]}`)
	r := NewCargoResolverWithBaseURL(srv.Client(), srv.URL)
	if _, yanked, _ := r.Yanked(context.Background(), "c", "0.2.0"); !yanked {
		t.Error("0.2.0 should be yanked")
	}
}
