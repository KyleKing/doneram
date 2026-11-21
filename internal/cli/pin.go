package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/cloud"
	"github.com/kyleking/doneram/internal/digest"
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
	Skipped string `json:"skipped,omitempty"`
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
			found, failures, err := hardenRepo(ctx, out, ".", !cmd.Bool("check"), nil, nil)
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

// pinResolver pins actions through the GitHub API, tools through mise, and
// images through their registry.
type pinResolver struct {
	actions *resolver.GitHubActionResolver
	images  *digest.Client
}

func (p pinResolver) ImageDigest(ctx context.Context, image string) (string, error) {
	ref, err := digest.Parse(image)
	if err != nil {
		return "", err
	}
	return p.images.Resolve(ctx, ref)
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
	return pinResolver{actions: resolver.NewGitHubActionResolver(client), images: digest.New(client)}
}

// hardenRepo scans baseDir for floating references and, with apply, pins
// them. It returns every finding and how many could not be pinned.
//
// owned is the set of template-maintained files (see templateOwnership),
// whose references are the template's to pin.
func hardenRepo(ctx context.Context, out report, baseDir string, apply bool, r harden.Resolver, owned map[string]string) ([]pklHardenSummary, int, error) {
	findings, err := harden.Scan(baseDir)
	if err != nil {
		return nil, 0, fmt.Errorf("scanning for floating references: %w", err)
	}
	findings = notOwned(findings, owned)
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
	summaries, failures := summarizeHardening(out, baseDir, findings, failures)
	return summaries, failures, nil
}

// refreshDigests re-resolves every image pinned as tag@digest and rewrites
// the ones whose tag now points elsewhere: a tag this run moved, or one
// rebuilt upstream with the same name, which is how a base image ships its
// OS security patches.
//
// only, when set, limits the refresh to those absolute file:line spots.
func refreshDigests(ctx context.Context, out report, baseDir string, r harden.Resolver, only map[string]bool, owned map[string]string) ([]pklHardenSummary, int, error) {
	findings, err := harden.ScanDigests(baseDir)
	if err != nil {
		return nil, 0, err
	}
	if findings = notOwned(findings, owned); len(findings) == 0 {
		return nil, 0, nil
	}
	if only != nil {
		kept := findings[:0]
		for _, f := range findings {
			abs, _ := filepath.Abs(f.File)
			if only[fmt.Sprintf("%s:%d", abs, f.Line)] {
				kept = append(kept, f)
			}
		}
		if findings = kept; len(findings) == 0 {
			return nil, 0, nil
		}
	}
	if r == nil {
		r = defaultPinResolver(httpclient.New(httpclient.DefaultConfig()))
	}
	harden.Resolve(ctx, findings, r)
	if _, err := harden.Apply(findings); err != nil {
		return nil, 0, err
	}
	var changed []harden.Finding
	for _, f := range findings {
		if f.Err != nil || f.Replacement != "" {
			changed = append(changed, f)
		}
	}
	if len(changed) == 0 {
		return nil, 0, nil
	}
	out.printf("\nImage digests:\n")
	summaries, failures := summarizeHardening(out, baseDir, changed, 0)
	return summaries, failures, nil
}

func summarizeHardening(out report, baseDir string, findings []harden.Finding, failures int) ([]pklHardenSummary, int) {
	summaries := make([]pklHardenSummary, len(findings))
	for i, f := range findings {
		rel, err := filepath.Rel(baseDir, f.File)
		if err != nil {
			rel = f.File
		}
		s := pklHardenSummary{Kind: string(f.Kind), File: filepath.ToSlash(rel), Line: f.Line, Name: f.Name, Ref: f.Ref}
		switch {
		case errors.Is(f.Err, cloud.ErrUnavailable):
			// A private cloud registry with no CLI or login here is
			// skipped, as a cloud-backed site is.
			s.Skipped = f.Err.Error()
			out.printf("· %s:%d %s: skipped, %v\n", s.File, s.Line, label(f), f.Err)
		case f.Err != nil:
			s.Error = f.Err.Error()
			failures++
			out.printf("✗ %s:%d %s: %v\n", s.File, s.Line, label(f), f.Err)
		case f.Replacement != "":
			s.Pinned, s.Updated = f.Replacement, true
			out.printf("  pinned %s → %s (%s:%d)\n", label(f), f.Replacement, s.File, s.Line)
		default:
			out.printf("⚠ %s:%d %s floats; `doneram pin` pins it\n", s.File, s.Line, label(f))
		}
		summaries[i] = s
	}
	return summaries, failures
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
		was, pinned := r.Ref, r.Pinned
		switch r.Kind {
		case string(harden.Image):
			was = "tag only"
		case string(harden.Digest):
			was = shortDigest(was) + " (rebuilt or retagged)"
		}
		if r.Kind == string(harden.Image) || r.Kind == string(harden.Digest) {
			pinned = shortDigest(pinned)
		}
		fmt.Fprintf(body, "| `%s` | %s | `%s` | `%s:%d` |\n", r.Name, code(was), pinned, r.File, r.Line)
	}
}

func shortDigest(d string) string {
	algo, hex, ok := strings.Cut(d, ":")
	if !ok || len(hex) < 12 {
		return d
	}
	return algo + ":" + hex[:12]
}

func code(s string) string {
	if strings.HasSuffix(s, ")") || s == "tag only" {
		return s
	}
	return "`" + s + "`"
}

// label names a finding as it reads in the file: an action or tool with its
// ref, an image by its tag (which already carries the ref).
func label(f harden.Finding) string {
	if f.Kind == harden.Image || f.Kind == harden.Digest {
		return f.Name
	}
	return f.Name + "@" + f.Ref
}

// notOwned drops findings in files a copier template maintains.
func notOwned(findings []harden.Finding, owned map[string]string) []harden.Finding {
	if len(owned) == 0 {
		return findings
	}
	kept := findings[:0:0]
	for _, f := range findings {
		if _, ok := ownedBy(owned, f.File); !ok {
			kept = append(kept, f)
		}
	}
	return kept
}
