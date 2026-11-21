package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/actionrisk"
)

// pklGroup is one pull request's worth of a run's changes. A plan run
// splits its changes so one risky bump never holds back the rest: every
// security fix, routine update, and safe major goes in "updates" with the
// lockfile, pinning, and digest changes; each major that needs review gets
// a "review-<tool>" PR of its own; and each bump that broke validation gets
// a "failing-<tool>" PR carrying the failure, to be fixed on its branch.
type pklGroup struct {
	ID     string   `json:"id"`
	Branch string   `json:"branch"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Tools  []string `json:"tools"`
	// Extras carries the changes that are not one tool's bump: lockfile
	// fixes, pinned floating references, and refreshed digests.
	Extras  bool `json:"extras,omitempty"`
	Failing bool `json:"failing,omitempty"`
}

const updatesGroup = "updates"

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(tool string) string {
	return strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(tool), "-"), "-")
}

// buildGroups splits a finished summary into groups. offenders maps a tool
// whose bump fails validation to the failure output.
func buildGroups(summary *pklSummary, offenders map[string]string) {
	review := map[string]bool{}
	var order []string
	seen := map[string]bool{}
	for _, s := range summary.Results {
		if !s.Updated && !s.needsUpdate {
			continue
		}
		if !seen[s.Tool] {
			seen[s.Tool] = true
			order = append(order, s.Tool)
		}
		if !s.Security && (s.Risk == string(actionrisk.Review) || s.Risk == string(actionrisk.Breaking)) {
			review[s.Tool] = true
		}
	}

	var updates []string
	var groups []pklGroup
	for _, tool := range order {
		switch {
		case hasKey(offenders, tool):
			groups = append(groups, pklGroup{ID: "failing-" + slug(tool), Tools: []string{tool}, Failing: true})
		case review[tool]:
			groups = append(groups, pklGroup{ID: "review-" + slug(tool), Tools: []string{tool}})
		default:
			updates = append(updates, tool)
		}
	}
	all := append([]pklGroup{{ID: updatesGroup, Tools: updates, Extras: true}}, groups...)

	summary.Groups = nil
	for _, g := range all {
		sub := groupSummary(*summary, g)
		finishSummary(&sub)
		if !sub.HasUpgrades {
			continue
		}
		g.Branch = "doneram/" + g.ID
		g.Title, g.Body = sub.Title, sub.Body
		if g.ID == updatesGroup && summary.Validation != "" {
			g.Body = fmt.Sprintf("**Validation fails on this branch, and not because of one bump** (it fails without them too, or bisection ran out).\n\n<details><summary>Validation output</summary>\n\n```\n%s\n```\n\n</details>\n", summary.Validation) + g.Body
		}
		if g.Failing {
			tool := g.Tools[0]
			g.Title = fmt.Sprintf("chore: update %s (validation fails)", tool)
			g.Body = fmt.Sprintf("**Validation fails with this update.** It was split out of the weekly PR so the rest could merge; fix it on this branch.\n\n<details><summary>Validation output</summary>\n\n```\n%s\n```\n\n</details>\n", offenders[tool]) + g.Body
		}
		summary.Groups = append(summary.Groups, g)
	}
}

// groupSummary narrows a summary to a group's tools, and to its extras
// when the group carries them.
func groupSummary(summary pklSummary, g pklGroup) pklSummary {
	tools := map[string]bool{}
	for _, t := range g.Tools {
		tools[t] = true
	}
	sub := pklSummary{}
	for _, s := range summary.Results {
		if tools[s.Tool] {
			sub.Results = append(sub.Results, s)
		}
	}
	if g.Extras {
		sub.Lockfiles, sub.Hardened, sub.Lifecycle = summary.Lockfiles, summary.Hardened, summary.Lifecycle
	}
	return sub
}

// loadGroup reads one group out of a plan run's JSON summary.
func loadGroup(planPath, id string) (*pklGroup, error) {
	data, err := os.ReadFile(planPath)
	if err != nil {
		return nil, fmt.Errorf("reading plan: %w", err)
	}
	var plan pklSummary
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parsing plan %s: %w", planPath, err)
	}
	ids := make([]string, 0, len(plan.Groups))
	for i, g := range plan.Groups {
		if g.ID == id {
			return &plan.Groups[i], nil
		}
		ids = append(ids, g.ID)
	}
	sort.Strings(ids)
	return nil, fmt.Errorf("plan %s has no group %q (groups: %s)", planPath, id, strings.Join(ids, ", "))
}

// has reports whether the group includes tool.
func (g *pklGroup) has(tool string) bool {
	for _, t := range g.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

func hasKey(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}
