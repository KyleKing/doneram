// Package config loads a repo's .doneram.pkl and compiles it into locator
// sites, matching the schema documented in doneram.md.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
)

// Site is one location a tool's version literal appears at.
type Site struct {
	File    string `json:"file"`
	Pattern string `json:"pattern"`
	Expect  int    `json:"expect"`
	// Window is how many consecutive lines Pattern matches against at
	// once, for a version tied to context on another line (a pre-commit
	// hook's rev under its repo URL). Defaults to 1.
	Window int `json:"window"`
}

// Hold narrows a tool's constraint with a ceiling and the reason for it.
type Hold struct {
	Reason string  `json:"reason"`
	Max    *string `json:"max"`
}

// Tool is one dependency tracked across one or more sites, or, when Command
// is set, a single command-resolver site with no file to locate a pin in.
type Tool struct {
	Resolver       string `json:"resolver"`
	ResolverName   string `json:"resolverName"`
	Sites          []Site `json:"sites"`
	Hold           *Hold  `json:"hold"`
	Command        string `json:"command"`
	CommandPattern string `json:"commandPattern"`
	// Ecosystem names the OSV ecosystem this tool's pin belongs to (e.g.
	// "PyPI", "Alpine:v3.19"), needed whenever the resolver name alone
	// doesn't determine it (a distro package, or a Go module).
	Ecosystem string `json:"ecosystem"`
}

// Config is a repo's .doneram.pkl, evaluated to JSON.
type Config struct {
	AfterPatch string          `json:"afterPatch"`
	Tools      map[string]Tool `json:"tools"`
}

// Load shells out to `pkl eval -f json` and parses the result.
func Load(path string) (*Config, error) {
	cmd := exec.Command("pkl", "eval", "-f", "json", path)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("pkl eval %s: %w: %s", path, err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("pkl eval %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(out, &cfg); err != nil {
		return nil, fmt.Errorf("parsing pkl output for %s: %w", path, err)
	}

	return &cfg, nil
}

// Sites compiles every tool's sites into engine.Sites, with file paths
// resolved relative to baseDir, in a stable order.
func (c *Config) Sites(baseDir string) []engine.Site {
	var out []engine.Site

	names := make([]string, 0, len(c.Tools))
	for name := range c.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tool := c.Tools[name]
		constraint := tool.constraint()

		if tool.Command != "" {
			out = append(out, engine.Site{
				Tool:           name,
				ResolverName:   tool.ResolverName,
				Constraint:     constraint,
				Command:        tool.Command,
				CommandPattern: tool.CommandPattern,
				Ecosystem:      tool.Ecosystem,
			})
			continue
		}

		for _, site := range tool.Sites {
			out = append(out, engine.Site{
				Tool:         name,
				ResolverName: tool.ResolverName,
				Constraint:   constraint,
				Ecosystem:    tool.Ecosystem,
				Locator: locator.Locator{
					Glob:     filepath.Join(baseDir, site.File),
					Pattern:  site.Pattern,
					Resolver: tool.Resolver,
					Expect:   site.Expect,
					Window:   site.Window,
				},
			})
		}
	}

	return out
}

// constraint builds the version constraint a hold narrows, or nil when the
// tool has no hold and should take engine's unconstrained default.
func (t Tool) constraint() *parser.VersionPattern {
	if t.Hold == nil || t.Hold.Max == nil {
		return nil
	}
	p := parser.ParsePattern("#.#.#")
	p.Ceiling = *t.Hold.Max
	p.HoldReason = t.Hold.Reason
	return p
}
