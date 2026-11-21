package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Repo is one repository a fleet run covers.
type Repo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	// Size is in kilobytes. GitHub reports 0 for an empty repo, and for a
	// new one it has not measured yet, so it is a hint, not the test.
	Size int `json:"size"`
}

// PR is the part of a pull request a fleet run reads back.
type PR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Head    struct {
		Ref  string `json:"ref"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

// GitHub is the REST client for the calls a fleet run makes. It is
// written by hand rather than with a library because it needs a dozen
// endpoints, and a test can point BaseURL at an httptest server.
type GitHub struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewGitHub returns a client for baseURL, api.github.com when empty.
func NewGitHub(baseURL, token string) *GitHub {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return &GitHub{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// apiError is a non-2xx response. Message is GitHub's own explanation,
// which is what a report reader needs (a missing scope, a protected
// branch), not the status line alone.
type apiError struct {
	Method, Path string
	Status       int
	Message      string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
}

func isStatus(err error, status int) bool {
	var api *apiError
	return errors.As(err, &api) && api.Status == status
}

// do sends one request and decodes a JSON response into out, returning the
// next page's URL from the Link header when there is one.
func (g *GitHub) do(ctx context.Context, method, path string, body, out any) (string, error) {
	target := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		target = g.BaseURL + path
	} else if !strings.HasPrefix(path, g.BaseURL+"/") {
		// A next-page link is the only absolute URL followed; one on
		// another host would carry the token there.
		return "", fmt.Errorf("refusing to follow %s outside %s", pathOnly(path), g.BaseURL)
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		var msg struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(data, &msg)
		text := msg.Message
		for _, e := range msg.Errors {
			if e.Message != "" {
				text += ": " + e.Message
			}
		}
		return "", &apiError{Method: method, Path: pathOnly(target), Status: resp.StatusCode, Message: text}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return "", fmt.Errorf("%s %s: decoding response: %w", method, pathOnly(target), err)
		}
	}
	return nextLink(resp.Header.Get("Link")), nil
}

// pathOnly drops the host and query from an error's URL, which keeps
// messages short and free of anything a query string might carry.
func pathOnly(target string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	return u.Path
}

var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func nextLink(header string) string {
	if m := linkNext.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	return ""
}

// paginate follows Link: rel="next" from path, appending every page.
func paginate[T any](ctx context.Context, g *GitHub, path string) ([]T, error) {
	var all []T
	for next := path; next != ""; {
		var page []T
		var err error
		if next, err = g.do(ctx, http.MethodGet, next, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
	}
	return all, nil
}

// Repo reads one repository's metadata, for a repo named on the command
// line: its default branch is the base every pull request targets.
func (g *GitHub) Repo(ctx context.Context, fullName string) (Repo, error) {
	var r Repo
	_, err := g.do(ctx, http.MethodGet, "/repos/"+fullName, nil, &r)
	return r, err
}

// OwnerRepos lists every repository an organization or user owns. An
// owner that is not an organization 404s on /orgs, and falls back to the
// user listing; when the token belongs to that same user, /user/repos is
// used instead, since /users/{name}/repos shows public repos only.
func (g *GitHub) OwnerRepos(ctx context.Context, owner string) ([]Repo, error) {
	esc := url.PathEscape(owner)
	repos, err := paginate[Repo](ctx, g, "/orgs/"+esc+"/repos?per_page=100&type=all")
	if err == nil || !isStatus(err, http.StatusNotFound) {
		return repos, err
	}
	var me struct {
		Login string `json:"login"`
	}
	// An installation token cannot read /user; that only means the
	// public listing is used.
	if _, err := g.do(ctx, http.MethodGet, "/user", nil, &me); err == nil && strings.EqualFold(me.Login, owner) {
		return paginate[Repo](ctx, g, "/user/repos?per_page=100&affiliation=owner")
	}
	return paginate[Repo](ctx, g, "/users/"+esc+"/repos?per_page=100&type=owner")
}

// OpenPR returns the open pull request from branch in repo itself, nil
// when there is none.
func (g *GitHub) OpenPR(ctx context.Context, repo, branch string) (*PR, error) {
	owner, _, _ := strings.Cut(repo, "/")
	q := url.Values{"state": {"open"}, "head": {owner + ":" + branch}}
	var prs []PR
	if _, err := g.do(ctx, http.MethodGet, "/repos/"+repo+"/pulls?"+q.Encode(), nil, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// OpenPRs lists every open pull request in repo.
func (g *GitHub) OpenPRs(ctx context.Context, repo string) ([]PR, error) {
	return paginate[PR](ctx, g, "/repos/"+repo+"/pulls?state=open&per_page=100")
}

// CreatePR opens a pull request from head into base. A repo whose plan
// has no draft pull requests (a private repo on a free plan) refuses a
// draft with a 422, so that one is retried as a regular pull request.
func (g *GitHub) CreatePR(ctx context.Context, repo, base, head, title, body string, draft bool) (PR, error) {
	req := map[string]any{"title": title, "body": body, "head": head, "base": base, "draft": draft}
	var pr PR
	_, err := g.do(ctx, http.MethodPost, "/repos/"+repo+"/pulls", req, &pr)
	if err != nil && draft && isStatus(err, http.StatusUnprocessableEntity) && strings.Contains(strings.ToLower(err.Error()), "draft") {
		req["draft"] = false
		_, err = g.do(ctx, http.MethodPost, "/repos/"+repo+"/pulls", req, &pr)
	}
	return pr, err
}

// UpdatePR rewrites an open pull request's title and body to this run's.
func (g *GitHub) UpdatePR(ctx context.Context, repo string, number int, title, body string) (PR, error) {
	var pr PR
	_, err := g.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), map[string]any{"title": title, "body": body}, &pr)
	return pr, err
}

// ClosePR closes a pull request without merging it.
func (g *GitHub) ClosePR(ctx context.Context, repo string, number int) error {
	_, err := g.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/pulls/%d", repo, number), map[string]any{"state": "closed"}, nil)
	return err
}

// DeleteBranch removes a branch. Callers only pass doneram/ branches.
func (g *GitHub) DeleteBranch(ctx context.Context, repo, branch string) error {
	_, err := g.do(ctx, http.MethodDelete, "/repos/"+repo+"/git/refs/heads/"+branch, nil, nil)
	return err
}
