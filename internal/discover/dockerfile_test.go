package discover

import (
	"testing"

	"github.com/kyleking/doneram/internal/locator"
)

func TestDirectiveSites(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Dockerfile", `# doneram: python:3.13.#
FROM python:3.13.11 AS base
FROM alpine:3.20
# doneram: ignore
FROM node:22.1.0
# doneram: uv:hold[breaks the build; <1.0.0]
COPY --from=ghcr.io/astral-sh/uv:0.9.26 /uv /bin/uv
`)
	write(t, dir, "api/api.Dockerfile", "# doneram: golang:1.#.#-alpine*\nFROM golang:1.22.1-alpine3.19\n")
	write(t, dir, "test/fixtures/Dockerfile", "# doneram: python:3.#.#\nFROM python:3.12.0\n")

	sites, err := directiveSites(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range sites {
		matches, err := locator.Find(s.Locator)
		if err != nil || len(matches) != 1 {
			t.Fatalf("%s: %v %v", s.Tool, matches, err)
		}
		got[s.Tool+" "+s.Locator.Resolver] = matches[0].Value
		if s.Tool == "uv" && s.Constraint.Ceiling != "1.0.0" {
			t.Errorf("uv hold = %+v", s.Constraint)
		}
	}
	want := map[string]string{
		"python docker": "3.13.11",
		"golang docker": "1.22.1-alpine3.19",
		"uv ghcr":       "0.9.26",
	}
	if len(got) != len(want) {
		t.Fatalf("sites = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
