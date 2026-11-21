// Package actionrisk judges whether moving a GitHub Action across a major
// version can break the workflow step that uses it. A major bump is not
// risky in itself: what breaks a step is an input it sets being removed, a
// newly required input it does not set, or an output it reads going away.
// Those are read from the action's own action.yml at both tags and checked
// against the step's `with:` keys and the workflow's `steps.<id>.outputs`
// references, so a reviewer sees "safe for how you use it" or the specific
// conflict rather than a bare version number.
package actionrisk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/kyleking/doneram/pkg/version"
)

// Level orders how much attention a bump needs.
type Level string

const (
	// Safe: nothing the step uses changed.
	Safe Level = "safe"
	// Review: something the step relies on implicitly changed (a default
	// it does not override, a deprecation of an input it sets).
	Review Level = "review"
	// Breaking: the step sets a removed input, misses a newly required
	// one, or reads a removed output.
	Breaking Level = "breaking"
)

// Assessment is the verdict for one site's bump.
type Assessment struct {
	Level Level
	Notes []string
}

// Usage is how one workflow step uses an action.
type Usage struct {
	// Path is the action's path inside its repo ("" or "init" for
	// github/codeql-action/init).
	Path string
	// With holds the input names the step sets.
	With map[string]bool
	// Outputs holds the output names the workflow reads from this step.
	Outputs map[string]bool
}

// IsMajor reports whether moving from one tag to another crosses a major
// version.
func IsMajor(from, to string) bool {
	f, t := version.Parse(strings.TrimPrefix(from, "v")), version.Parse(strings.TrimPrefix(to, "v"))
	return t.Major > f.Major
}

var outputRef = regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs\.([A-Za-z0-9_-]+)`)

// UsageAt reads the step whose `uses:` sits on line of file.
func UsageAt(file string, line int) (Usage, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return Usage{}, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return Usage{}, fmt.Errorf("parsing %s: %w", file, err)
	}

	step := findStep(&root, line)
	if step == nil {
		return Usage{}, fmt.Errorf("%s:%d: no step uses an action on this line", file, line)
	}

	u := Usage{With: map[string]bool{}, Outputs: map[string]bool{}}
	var id string
	for i := 0; i+1 < len(step.Content); i += 2 {
		key, val := step.Content[i].Value, step.Content[i+1]
		switch key {
		case "uses":
			ref, _, _ := strings.Cut(val.Value, "@")
			if parts := strings.SplitN(ref, "/", 3); len(parts) == 3 {
				u.Path = parts[2]
			}
		case "id":
			id = val.Value
		case "with":
			for j := 0; j+1 < len(val.Content); j += 2 {
				u.With[val.Content[j].Value] = true
			}
		}
	}

	if id != "" {
		for _, m := range outputRef.FindAllStringSubmatch(string(content), -1) {
			if m[1] == id {
				u.Outputs[m[2]] = true
			}
		}
	}
	return u, nil
}

func findStep(n *yaml.Node, line int) *yaml.Node {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "uses" && n.Content[i+1].Line == line {
				return n
			}
		}
	}
	for _, c := range n.Content {
		if found := findStep(c, line); found != nil {
			return found
		}
	}
	return nil
}

// Client fetches action metadata and release notes from GitHub.
type Client struct {
	client  *http.Client
	baseURL string

	mu    sync.Mutex
	cache map[string]cached
}

// cached is one GET's outcome. Several sites of one tool make the same bump
// and ask for the same files, so each is fetched once per run.
type cached struct {
	body []byte
	err  error
}

func New(client *http.Client) *Client {
	return NewWithBaseURL(client, "https://api.github.com")
}

func NewWithBaseURL(client *http.Client, baseURL string) *Client {
	return &Client{client: client, baseURL: baseURL, cache: map[string]cached{}}
}

type input struct {
	Required           any    `yaml:"required"`
	Default            any    `yaml:"default"`
	DeprecationMessage string `yaml:"deprecationMessage"`
}

func (i input) required() bool {
	switch v := i.Required.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

func (i input) defaultValue() (string, bool) {
	if i.Default == nil {
		return "", false
	}
	return fmt.Sprint(i.Default), true
}

type metadata struct {
	Inputs  map[string]input `yaml:"inputs"`
	Outputs map[string]any   `yaml:"outputs"`
	Runs    struct {
		Using string `yaml:"using"`
	} `yaml:"runs"`
}

// Assess compares repo's action metadata at from and to against every
// usage, and pulls the lines release notes flag as breaking for each major
// crossed.
func (c *Client) Assess(ctx context.Context, repo, from, to string, usages []Usage) (Assessment, error) {
	paths := map[string]bool{}
	for _, u := range usages {
		paths[u.Path] = true
	}
	if len(paths) == 0 {
		paths[""] = true
	}

	a := Assessment{Level: Safe}
	seen := map[string]bool{}
	note := func(level Level, format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if seen[msg] {
			return
		}
		seen[msg] = true
		a.Notes = append(a.Notes, msg)
		if rank(level) > rank(a.Level) {
			a.Level = level
		}
	}

	for _, path := range sortedKeys(paths) {
		before, err := c.metadata(ctx, repo, path, from)
		if err != nil {
			return Assessment{}, err
		}
		after, err := c.metadata(ctx, repo, path, to)
		if err != nil {
			return Assessment{}, err
		}
		label := repo
		if path != "" {
			label += "/" + path
		}

		for _, u := range usages {
			if u.Path != path {
				continue
			}
			for name := range u.With {
				newInput, ok := after.Inputs[name]
				switch {
				case !ok:
					if _, existed := before.Inputs[name]; existed {
						note(Breaking, "%s: input `%s` was removed and this workflow sets it", label, name)
					}
				case newInput.DeprecationMessage != "" && before.Inputs[name].DeprecationMessage == "":
					note(Review, "%s: input `%s` is now deprecated: %s", label, name, oneLine(newInput.DeprecationMessage))
				}
			}
			for name, in := range after.Inputs {
				_, hasDefault := in.defaultValue()
				if !in.required() || hasDefault || u.With[name] {
					continue
				}
				if old, existed := before.Inputs[name]; existed && old.required() {
					continue
				}
				note(Breaking, "%s: input `%s` is newly required and this workflow does not set it", label, name)
			}
			for name, in := range after.Inputs {
				if u.With[name] {
					continue
				}
				old, existed := before.Inputs[name]
				if !existed {
					continue
				}
				was, _ := old.defaultValue()
				now, _ := in.defaultValue()
				if was != now {
					note(Review, "%s: default of `%s` changed from %q to %q, and this workflow relies on it", label, name, oneLine(was), oneLine(now))
				}
			}
			for name := range u.Outputs {
				if _, ok := after.Outputs[name]; !ok {
					if _, existed := before.Outputs[name]; existed {
						note(Breaking, "%s: output `%s` was removed and this workflow reads it", label, name)
					}
				}
			}
		}

		if before.Runs.Using != after.Runs.Using && after.Runs.Using != "" {
			note(Safe, "%s: runtime %s → %s (self-hosted runners need a release that supports it)", label, before.Runs.Using, after.Runs.Using)
		}
	}

	for _, line := range c.breakingNotes(ctx, repo, from, to) {
		note(Safe, "release notes: %s", line)
	}
	return a, nil
}

func rank(l Level) int {
	switch l {
	case Breaking:
		return 2
	case Review:
		return 1
	}
	return 0
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

func (c *Client) metadata(ctx context.Context, repo, path, ref string) (metadata, error) {
	dir := ""
	if path != "" {
		dir = strings.Trim(path, "/") + "/"
	}
	var lastErr error
	for _, name := range []string{"action.yml", "action.yaml"} {
		body, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/contents/%s%s?ref=%s", c.baseURL, repo, dir, name, ref), "application/vnd.github.raw")
		if err != nil {
			lastErr = err
			continue
		}
		var m metadata
		if err := yaml.Unmarshal(body, &m); err != nil {
			return metadata{}, fmt.Errorf("parsing %s %s@%s: %w", repo, name, ref, err)
		}
		return m, nil
	}
	return metadata{}, fmt.Errorf("fetching %s action metadata at %s: %w", repo, ref, lastErr)
}

var breakingLine = regexp.MustCompile(`(?i)\bbreaking\b`)

// breakingNotes returns up to three lines the release notes of each new
// major's .0.0 release flag as breaking. Missing releases are skipped: the
// interface diff above is the verdict, and the notes only explain it.
func (c *Client) breakingNotes(ctx context.Context, repo, from, to string) []string {
	f, t := version.Parse(strings.TrimPrefix(from, "v")), version.Parse(strings.TrimPrefix(to, "v"))
	prefix := ""
	if strings.HasPrefix(to, "v") {
		prefix = "v"
	}
	var out []string
	for major := f.Major + 1; major <= t.Major && len(out) < 3; major++ {
		body, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s%d.0.0", c.baseURL, repo, prefix, major), "application/vnd.github+json")
		if err != nil {
			continue
		}
		var rel struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(body, &rel); err != nil {
			continue
		}
		for _, line := range strings.Split(rel.Body, "\n") {
			line = strings.TrimLeft(strings.TrimSpace(line), "-*# ")
			if line == "" || !breakingLine.MatchString(line) {
				continue
			}
			out = append(out, fmt.Sprintf("v%d: %s", major, oneLine(line)))
			if len(out) == 3 {
				break
			}
		}
	}
	return out
}

func (c *Client) get(ctx context.Context, url, accept string) ([]byte, error) {
	c.mu.Lock()
	hit, ok := c.cache[url]
	c.mu.Unlock()
	if ok {
		return hit.body, hit.err
	}
	body, err := c.fetch(ctx, url, accept)
	c.mu.Lock()
	c.cache[url] = cached{body, err}
	c.mu.Unlock()
	return body, err
}

func (c *Client) fetch(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API status %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
