package harden

import (
	"context"
	"os"
	"strings"
	"testing"
)

const dockerfile = `ARG BASE=python:3.12
FROM --platform=$BUILDPLATFORM python:3.13-slim AS build
COPY --from=ghcr.io/astral-sh/uv:0.5.4 /uv /bin/
FROM build AS test
FROM ${BASE}
FROM alpine
FROM scratch
COPY --from=build /app /app
FROM debian:12@sha256:` + "0000000000000000000000000000000000000000000000000000000000000000" + `
FROM node:22@sha256:` + "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" + `
`

func TestImages(t *testing.T) {
	dir := t.TempDir()
	df := write(t, dir, "Dockerfile", dockerfile)
	compose := write(t, dir, "deploy/compose.yaml", "services:\n  db:\n    image: \"postgres:16.4\" # db\n")

	findings, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, string(f.Kind)+":"+f.Name)
	}
	want := "image:python:3.13-slim image:ghcr.io/astral-sh/uv:0.5.4 image:postgres:16.4"
	if strings.Join(got, " ") != want {
		t.Fatalf("Scan() = %v, want %s", got, want)
	}

	Resolve(context.Background(), findings, &fakeResolver{})
	if _, err := Apply(findings); err != nil {
		t.Fatal(err)
	}
	d := "sha256:" + strings.Repeat("d", 64)
	data, _ := os.ReadFile(df)
	for _, line := range []string{
		"FROM --platform=$BUILDPLATFORM python:3.13-slim@" + d + " AS build",
		"COPY --from=ghcr.io/astral-sh/uv:0.5.4@" + d + " /uv /bin/",
		"FROM alpine\n", "FROM build AS test",
	} {
		if !strings.Contains(string(data), line) {
			t.Errorf("Dockerfile missing %q:\n%s", line, data)
		}
	}
	data, _ = os.ReadFile(compose)
	if !strings.Contains(string(data), `image: "postgres:16.4@`+d+`" # db`) {
		t.Errorf("compose =\n%s", data)
	}

	// Every image is pinned now; only debian's digest is stale (node's
	// already matches what the registry returns).
	pinned, err := ScanDigests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 5 {
		t.Fatalf("ScanDigests() found %d, want 5", len(pinned))
	}
	Resolve(context.Background(), pinned, &fakeResolver{})
	n, err := Apply(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("refresh rewrote %d digests, want 1", n)
	}
	data, _ = os.ReadFile(df)
	if strings.Contains(string(data), strings.Repeat("0", 64)) || !strings.Contains(string(data), "FROM debian:12@"+d) {
		t.Errorf("stale digest not refreshed:\n%s", data)
	}
}
