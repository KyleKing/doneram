package eol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLookups(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/claude.json":
			_, _ = w.Write([]byte(`[{"cycle":"claude-opus-4","eol":"2026-05-01"},{"cycle":"claude-opus-4-1","eol":"2027-01-01"}]`))
		case "/api/debian.json":
			_, _ = w.Write([]byte(`[{"cycle":"12","codename":"Bookworm","eol":"2026-06-10"},{"cycle":"9","eol":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)

	s, err := c.LookupID(ctx, "claude", "claude-opus-4-1-20250805")
	if err != nil || s.Cycle != "claude-opus-4-1" || s.Past(now) {
		t.Errorf("LookupID dated id = %+v, %v; want cycle claude-opus-4-1, not past", s, err)
	}
	s, err = c.LookupID(ctx, "claude", "claude-opus-4-20250514")
	if err != nil || s.Cycle != "claude-opus-4" || !s.Past(now) {
		t.Errorf("LookupID = %+v, %v; want claude-opus-4, past", s, err)
	}
	if _, err := c.LookupID(ctx, "claude", "claude-opus-40"); err == nil {
		t.Error("LookupID matched across a non-dash boundary")
	}
	s, err = c.Lookup(ctx, "debian", "bookworm")
	if err != nil || s.Cycle != "12" {
		t.Errorf("Lookup codename = %+v, %v", s, err)
	}
	s, err = c.Lookup(ctx, "debian", "9.13")
	if err != nil || !s.Ended || !s.Past(now) {
		t.Errorf("Lookup ended-without-date = %+v, %v", s, err)
	}
}
