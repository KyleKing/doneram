package resolver

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

// MiseResolver shells out to mise for any tool in its registry, covering
// core, aqua, go:, npm:, and pipx: backends without doneram maintaining a
// name-to-upstream table of its own.
type MiseResolver struct{}

func NewMiseResolver() *MiseResolver {
	return &MiseResolver{}
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
