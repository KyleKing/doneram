package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBisect(t *testing.T) {
	units := []string{"a", "b", "c", "d", "e", "f", "g"}
	bad := map[string]bool{"c": true, "f": true}
	runs := 0
	trial := func(tools []string) (bool, string) {
		runs++
		for _, tool := range tools {
			if bad[tool] {
				return false, tool + " broke it"
			}
		}
		return true, ""
	}
	offenders, baseline := bisect(units, trial)
	if baseline != "" || len(offenders) != 2 || offenders["c"] != "c broke it" || offenders["f"] != "f broke it" {
		t.Errorf("bisect() = %v, %q", offenders, baseline)
	}
	if runs > 12 {
		t.Errorf("bisect took %d runs", runs)
	}

	offenders, baseline = bisect(units, func([]string) (bool, string) { return false, "always" })
	if baseline != "always" || offenders != nil {
		t.Errorf("baseline failure = %v, %q", offenders, baseline)
	}
}

// bisectPkl declares two command-backed tools in one file; validate fails
// whenever "bad" reaches 2.
const bisectPkl = `
class Site {
  file: String
  pattern: String
}

class Tool {
  resolver: String = "mise"
  sites: Listing<Site> = new {}
  command: String?
  commandPattern: String?
}

validate = "! grep -q 'bad>=2' deps.txt"
lockfiles = false

tools: Mapping<String, Tool> = new {
  ["good"] = new {
    command = "printf 'good 1.0.0 1.1.0\n'"
    commandPattern = #"^(?<name>\S+) (?<current>\S+) (?<latest>\S+)$"#
    sites { new Site { file = "deps.txt"; pattern = #"good>=([\d.]+)"# } }
  }
  ["bad"] = new {
    command = "printf 'bad 1.0.0 2.0.0\n'"
    commandPattern = #"^(?<name>\S+) (?<current>\S+) (?<latest>\S+)$"#
    sites { new Site { file = "deps.txt"; pattern = #"bad>=([\d.]+)"# } }
  }
}
`

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestPlanSplitsFailingBump(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	deps := filepath.Join(dir, "deps.txt")
	if err := os.WriteFile(cfg, []byte(bisectPkl), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deps, []byte("good>=1.0.0\nbad>=1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")

	plan := filepath.Join(t.TempDir(), "plan.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: plan, format: "json", workers: 1}); err != nil {
		t.Fatalf("plan err = %v", err)
	}
	if data, _ := os.ReadFile(deps); string(data) != "good>=1.1.0\nbad>=1.0.0\n" {
		t.Errorf("plan left deps.txt = %q, want only the passing bump", data)
	}
	var summary pklSummary
	raw, _ := os.ReadFile(plan)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, g := range summary.Groups {
		ids = append(ids, g.ID)
	}
	if strings.Join(ids, " ") != "updates failing-bad" {
		t.Fatalf("groups = %v", ids)
	}
	if !strings.Contains(summary.Groups[1].Body, "Validation fails") || !strings.Contains(summary.Groups[1].Title, "validation fails") {
		t.Errorf("failing group = %+v", summary.Groups[1])
	}

	// The failing group's own run applies only its bump.
	git(t, dir, "checkout", "-q", "--", ".")
	group, err := loadGroup(plan, "failing-bad")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "group.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, group: group}); err != nil {
		t.Fatalf("group err = %v", err)
	}
	if data, _ := os.ReadFile(deps); string(data) != "good>=1.0.0\nbad>=2.0.0\n" {
		t.Errorf("group run left deps.txt = %q", data)
	}
	raw, _ = os.ReadFile(out)
	var groupSummary pklSummary
	if err := json.Unmarshal(raw, &groupSummary); err != nil {
		t.Fatal(err)
	}
	if !groupSummary.HasUpgrades || groupSummary.Title != summary.Groups[1].Title {
		t.Errorf("group summary = %+v", groupSummary)
	}
}

func TestBuildGroupsReview(t *testing.T) {
	summary := pklSummary{Results: []pklSiteSummary{
		{Tool: "actions/checkout", Current: "a # v4.2.0", Latest: "b # v5.0.0", Risk: "breaking", needsUpdate: true},
		{Tool: "actions/setup-go", Current: "c # v5.0.0", Latest: "d # v6.0.0", Risk: "safe", needsUpdate: true},
		{Tool: "jq", Current: "1.7.0", Latest: "1.7.1", needsUpdate: true},
		{Tool: "uv", Current: "0.5.0", Latest: "0.5.0"},
	}}
	buildGroups(&summary, nil)
	var got []string
	for _, g := range summary.Groups {
		got = append(got, g.ID+"="+strings.Join(g.Tools, ","))
	}
	if strings.Join(got, " ") != "updates=actions/setup-go,jq review-actions-checkout=actions/checkout" {
		t.Errorf("groups = %v", got)
	}
	if summary.Groups[1].Branch != "doneram/review-actions-checkout" {
		t.Errorf("branch = %s", summary.Groups[1].Branch)
	}
}

func TestBuildGroupsProvenance(t *testing.T) {
	summary := pklSummary{Results: []pklSiteSummary{
		{Tool: "left-pad", Current: "1.0.0", Latest: "1.0.1", Security: true, Provenance: "left-pad 1.0.1 has no build attestation", needsUpdate: true},
		{Tool: "jq", Current: "1.7.0", Latest: "1.7.1", needsUpdate: true},
	}}
	buildGroups(&summary, nil)
	if len(summary.Groups) != 2 || summary.Groups[1].ID != "review-left-pad" || !strings.Contains(summary.Groups[1].Body, "provenance changed") {
		t.Errorf("groups = %+v", summary.Groups)
	}
}
