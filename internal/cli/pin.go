package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/harden"
	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/resolver"
)

// pklHardenSummary is one floating reference: an action used by tag or a
// mise tool given as a prefix, and what it was (or would be) pinned to.
type pklHardenSummary struct {
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	Pinned  string `json:"pinned,omitempty"`
	Updated bool   `json:"updated"`
	Error   string `json:"error,omitempty"`
}

func newPinCommand() *cli.Command {
	return &cli.Command{
		Name:  "pin",
		Usage: "Pin floating references: actions used by tag become <sha> # <tag>, mise prefixes become exact versions",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "check",
				Usage: "only report floating references, and exit 8 when any exist",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			out := report{w: os.Stdout}
			found, failures, err := hardenRepo(ctx, out, ".", !cmd.Bool("check"), nil)
			if err != nil {
				return err
			}
			if failures > 0 {
				return exitf(ExitUnresolved, "%d reference(s) could not be pinned", failures)
			}
			if cmd.Bool("check") && len(found) > 0 {
				return exitf(ExitUnpinned, "%d floating reference(s)", len(found))
			}
			return nil
		},
	}
}

// pinResolver pins actions through the GitHub API and tools through mise.
type pinResolver struct {
	actions *resolver.GitHubActionResolver
}

func (p pinResolver) PinAction(ctx context.Context, repo, ref string) (string, error) {
	return p.actions.PinRef(ctx, repo, ref)
}

func (p pinResolver) PinTool(ctx context.Context, tool, ref string) (string, error) {
	arg := tool
	if ref != "latest" {
		arg += "@" + ref
	}
	res, err := exec.CommandContext(ctx, "mise", "latest", arg).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mise latest %s: %w: %s", arg, err, strings.TrimSpace(string(res)))
	}
	return strings.TrimSpace(string(res)), nil
}

func defaultPinResolver(client *http.Client) harden.Resolver {
	return pinResolver{actions: resolver.NewGitHubActionResolver(client)}
}

// hardenRepo scans baseDir for floating references and, with apply, pins
// them. It returns every finding and how many could not be pinned.
func hardenRepo(ctx context.Context, out report, baseDir string, apply bool, r harden.Resolver) ([]pklHardenSummary, int, error) {
	findings, err := harden.Scan(baseDir)
	if err != nil {
		return nil, 0, fmt.Errorf("scanning for floating references: %w", err)
	}
	if len(findings) == 0 {
		return nil, 0, nil
	}

	failures := 0
	if apply {
		if r == nil {
			r = defaultPinResolver(httpclient.New(httpclient.DefaultConfig()))
		}
		harden.Resolve(ctx, findings, r)
		if _, err := harden.Apply(findings); err != nil {
			return nil, 0, err
		}
	}

	out.printf("\nFloating references:\n")
	summaries := make([]pklHardenSummary, len(findings))
	for i, f := range findings {
		rel, err := filepath.Rel(baseDir, f.File)
		if err != nil {
			rel = f.File
		}
		s := pklHardenSummary{Kind: string(f.Kind), File: filepath.ToSlash(rel), Line: f.Line, Name: f.Name, Ref: f.Ref}
		switch {
		case f.Err != nil:
			s.Error = f.Err.Error()
			failures++
			out.printf("✗ %s:%d %s@%s: %v\n", s.File, s.Line, f.Name, f.Ref, f.Err)
		case f.Replacement != "":
			s.Pinned, s.Updated = f.Replacement, true
			out.printf("  pinned %s@%s → %s (%s:%d)\n", f.Name, f.Ref, f.Replacement, s.File, s.Line)
		default:
			out.printf("⚠ %s:%d %s@%s floats; `doneram pin` pins it\n", s.File, s.Line, f.Name, f.Ref)
		}
		summaries[i] = s
	}
	return summaries, failures, nil
}

func writeHardenSection(body *strings.Builder, items []pklHardenSummary) {
	var rows []pklHardenSummary
	for _, h := range items {
		if h.Updated {
			rows = append(rows, h)
		}
	}
	if len(rows) == 0 {
		return
	}
	body.WriteString("\n### Pinned floating references\n\n")
	body.WriteString("A tag or version prefix can move to new code without a commit here. Each is now pinned to what it resolves to today, so later updates arrive as reviewed changes.\n\n")
	body.WriteString("| Reference | Was | Pinned | Where |\n")
	body.WriteString("| --- | --- | --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(body, "| `%s` | `%s` | `%s` | `%s:%d` |\n", r.Name, r.Ref, r.Pinned, r.File, r.Line)
	}
}
