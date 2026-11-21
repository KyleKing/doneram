package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/snapshot"
)

// appVersion is the running doneram version, recorded as the snapshot's
// detector version.
var appVersion = "dev"

// writeSnapshot renders the pins on disk as a dependency submission
// snapshot at path. The commit and ref come from the Actions environment,
// falling back to git, since the API rejects a snapshot without them.
func writeSnapshot(ctx context.Context, path, root string, results []engine.SiteResult) error {
	sha := os.Getenv("GITHUB_SHA")
	if sha == "" {
		sha = gitOutput(ctx, root, "rev-parse", "HEAD")
	}
	ref := os.Getenv("GITHUB_REF")
	if ref == "" {
		ref = gitOutput(ctx, root, "symbolic-ref", "HEAD")
	}
	if sha == "" || ref == "" {
		return fmt.Errorf("writing snapshot: no commit or ref (set GITHUB_SHA and GITHUB_REF or run inside a git checkout)")
	}

	jobID := os.Getenv("GITHUB_RUN_ID")
	if attempt := os.Getenv("GITHUB_RUN_ATTEMPT"); jobID != "" && attempt != "" {
		jobID += "-" + attempt
	}

	snap := snapshot.Build(results, snapshot.Options{
		Root:    root,
		SHA:     sha,
		Ref:     ref,
		JobID:   jobID,
		Version: appVersion,
		Scanned: time.Now(),
	})
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
