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

	"github.com/kyleking/doneram/internal/locator"
)

// Site is one location a tool's version literal appears at.
type Site struct {
	File    string `json:"file"`
	Pattern string `json:"pattern"`
	Expect  int    `json:"expect"`
}

// Hold narrows a tool's constraint with a ceiling and the reason for it.
type Hold struct {
	Reason string  `json:"reason"`
	Max    *string `json:"max"`
}

// Tool is one dependency tracked across one or more sites.
type Tool struct {
	Resolver     string `json:"resolver"`
	ResolverName string `json:"resolverName"`
	Sites        []Site `json:"sites"`
	Hold         *Hold  `json:"hold"`
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

// Sites compiles every tool's sites into locator.Sites, with file paths
// resolved relative to baseDir, in a stable order.
func (c *Config) Sites(baseDir string) []locator.Site {
	var out []locator.Site

	names := make([]string, 0, len(c.Tools))
	for name := range c.Tools {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		tool := c.Tools[name]
		for _, site := range tool.Sites {
			out = append(out, locator.Site{
				Tool:         name,
				ResolverName: tool.ResolverName,
				Locator: locator.Locator{
					Glob:     filepath.Join(baseDir, site.File),
					Pattern:  site.Pattern,
					Resolver: tool.Resolver,
					Expect:   site.Expect,
				},
			})
		}
	}

	return out
}
