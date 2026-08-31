package resolver

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
)

// GitHubBranchResolver resolves a tracked branch's HEAD commit as a SHA
// pin. A SHA has no version ordering, so pattern is unused: Resolve always
// answers with the branch's current HEAD, and Detail carries the drift and
// age a plain version diff can't express.
type GitHubBranchResolver struct {
	client  *http.Client
	baseURL string
}

func NewGitHubBranchResolver(client *http.Client) *GitHubBranchResolver {
	return &GitHubBranchResolver{
		client:  client,
		baseURL: "https://api.github.com",
	}
}

func NewGitHubBranchResolverWithBaseURL(client *http.Client, baseURL string) *GitHubBranchResolver {
	return &GitHubBranchResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *GitHubBranchResolver) Name() string {
	return "github-branch"
}

type githubCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

type githubCompare struct {
	AheadBy int            `json:"ahead_by"`
	Commits []githubCommit `json:"commits"`
}

type githubTag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// splitOwnerRepoBranch parses "owner/repo@branch" or "owner/repo", the
// latter defaulting to branch "main".
func splitOwnerRepoBranch(s string) (owner, repo, branch string, err error) {
	repoPart, branch, hasBranch := strings.Cut(s, "@")
	if !hasBranch {
		branch = "main"
	}
	owner, repo, err = splitOwnerRepo(repoPart)
	return owner, repo, branch, err
}

func (r *GitHubBranchResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	owner, repo, branch, err := splitOwnerRepoBranch(pkg)
	if err != nil {
		return "", err
	}

	commit, err := r.headCommit(ctx, owner, repo, branch)
	if err != nil {
		logger.Warn("failed to fetch branch head", "repo", pkg, "error", err)
		return "", fmt.Errorf("fetching %s/%s@%s head: %w", owner, repo, branch, err)
	}

	logger.Info("resolved branch head", "resolver", "github-branch", "repo", pkg, "sha", commit.SHA)
	return commit.SHA, nil
}

func (r *GitHubBranchResolver) headCommit(ctx context.Context, owner, repo, branch string) (githubCommit, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", r.baseURL, owner, repo, branch)
	return getGitHubJSON[githubCommit](ctx, r.client, url)
}

func (r *GitHubBranchResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}

// Detail reports how many commits and how much time the pinned SHA is
// behind the branch, how old the pinned commit itself is, and warns when
// the repo has tags newer than the pin (never when it has no tags at all).
func (r *GitHubBranchResolver) Detail(ctx context.Context, pkg string, current, latest string) (string, error) {
	if current == "" || current == latest {
		return "", nil
	}
	owner, repo, branch, err := splitOwnerRepoBranch(pkg)
	if err != nil {
		return "", err
	}

	pinned, err := getGitHubJSON[githubCommit](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/commits/%s", r.baseURL, owner, repo, current))
	if err != nil {
		return "", fmt.Errorf("fetching pinned commit %s: %w", current, err)
	}

	compare, err := getGitHubJSON[githubCompare](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/compare/%s...%s", r.baseURL, owner, repo, current, branch))
	if err != nil {
		return "", fmt.Errorf("comparing %s...%s: %w", current, branch, err)
	}

	pinnedDate := pinned.Commit.Committer.Date
	headDate := pinnedDate
	if len(compare.Commits) > 0 {
		headDate = compare.Commits[len(compare.Commits)-1].Commit.Committer.Date
	}

	text := fmt.Sprintf("%d commits, %s behind %s; pinned commit is %s old",
		compare.AheadBy, formatDuration(headDate.Sub(pinnedDate)), branch, formatDuration(time.Since(pinnedDate)))

	tags, err := getGitHubJSON[[]githubTag](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/tags?per_page=1", r.baseURL, owner, repo))
	if err == nil && len(tags) > 0 && tags[0].Commit.SHA != current {
		newer, cmpErr := getGitHubJSON[githubCompare](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/compare/%s...%s", r.baseURL, owner, repo, current, tags[0].Commit.SHA))
		if cmpErr == nil && newer.AheadBy > 0 {
			text += fmt.Sprintf("; warning: %s has tags newer than the pinned commit, e.g. %s", repo, tags[0].Name)
		}
	}

	return text, nil
}

func formatDuration(d time.Duration) string {
	switch {
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%d months", int(d.Hours()/24/30))
	}
}
