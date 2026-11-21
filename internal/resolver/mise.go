package resolver

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/support"
	"github.com/kyleking/doneram/pkg/version"
)

// MiseResolver shells out to mise for any tool in its registry, covering
// core, aqua, go:, npm:, and pipx: backends without doneram maintaining a
// name-to-upstream table of its own.
type MiseResolver struct {
	// Upstreams answer History for the tool behind a mise backend: GitHub
	// for aqua:, github:, ubi:, and Go modules on github.com; npm and PyPI
	// for npm: and pipx:. A nil one leaves that backend without history.
	GitHub, NPM, PyPI Historian
}

func NewMiseResolver() *MiseResolver {
	return &MiseResolver{}
}

// History lists the releases of the project behind a mise tool, found
// through the backend mise maps it to (`mise tool <name> --backend`). A
// core: tool (node, python, go) has an endoflife.date entry instead, and
// a backend with no release history to read returns none.
func (r *MiseResolver) History(ctx context.Context, tool string) ([]support.Release, error) {
	backend, err := miseBackend(ctx, tool)
	if err != nil {
		return nil, err
	}
	hist, pkg := r.upstream(backend)
	if hist == nil {
		return nil, nil
	}
	return hist.History(ctx, pkg)
}

// upstream maps a mise backend ("aqua:rhysd/actionlint",
// "go:github.com/golangci/golines", "pipx:commitizen") to the source that
// has its release history and the package name there.
func (r *MiseResolver) upstream(backend string) (Historian, string) {
	kind, name, ok := strings.Cut(backend, ":")
	if !ok {
		return nil, ""
	}
	repo := func(s string) string {
		s = strings.TrimPrefix(s, "github.com/")
		parts := strings.Split(s, "/")
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "/" + parts[1]
	}
	switch kind {
	case "aqua", "github", "ubi":
		if rp := repo(name); rp != "" && r.GitHub != nil {
			return r.GitHub, rp
		}
	case "go", "packslip":
		if strings.HasPrefix(name, "github.com/") && r.GitHub != nil {
			if rp := repo(name); rp != "" {
				return r.GitHub, rp
			}
		}
	case "npm":
		if r.NPM != nil {
			return r.NPM, name
		}
	case "pipx":
		if r.PyPI != nil {
			return r.PyPI, name
		}
	}
	return nil, ""
}

func miseBackend(ctx context.Context, tool string) (string, error) {
	// A tool already written as backend:name is its own backend.
	if strings.Contains(tool, ":") {
		return tool, nil
	}
	out, err := exec.CommandContext(ctx, "mise", "tool", tool, "--backend").Output()
	if err != nil {
		return "", fmt.Errorf("mise tool %s --backend: %w", tool, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *MiseResolver) Name() string {
	return "mise"
}

// Resolve tries `mise latest`, the cheap path, first. When its result
// doesn't satisfy pattern (a hold ceiling or a pinned major/minor), it
// falls back to `mise ls-remote` for the full version list to filter.
func (r *MiseResolver) Resolve(ctx context.Context, tool string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving tool", "resolver", "mise", "tool", tool)

	latest, err := miseLatest(ctx, tool)
	if err == nil && pattern.Matches(latest) {
		logger.Info("resolved tool", "resolver", "mise", "tool", tool, "version", latest)
		return latest, nil
	}

	versions, lsErr := miseLsRemote(ctx, tool)
	if lsErr != nil {
		if err != nil {
			return "", err
		}
		return "", lsErr
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved tool", "resolver", "mise", "tool", tool, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for tool %s with pattern %v", tool, pattern)
}

func (r *MiseResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}

func miseLatest(ctx context.Context, tool string) (string, error) {
	cmd := exec.CommandContext(ctx, "mise", "latest", tool)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mise latest %s: %w: %s", tool, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func miseLsRemote(ctx context.Context, tool string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "mise", "ls-remote", tool)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mise ls-remote %s: %w: %s", tool, err, strings.TrimSpace(string(out)))
	}
	return parseLsRemote(string(out)), nil
}

func parseLsRemote(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	versions := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			versions = append(versions, line)
		}
	}
	return versions
}
