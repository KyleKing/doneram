package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pagedRepos serves path in two pages linked by rel="next", the way
// GitHub does.
func pagedRepos(t *testing.T, mux *http.ServeMux, srv **httptest.Server, path, owner string) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("%s queried without per_page=100: %s", path, r.URL.RawQuery)
		}
		if r.URL.Query().Get("page") == "2" {
			_ = json.NewEncoder(w).Encode([]Repo{
				{FullName: owner + "/c", DefaultBranch: "main"},
				{FullName: owner + "/old", Archived: true},
			})
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?per_page=100&page=2>; rel="next", <%s%s?per_page=100&page=2>; rel="last"`, (*srv).URL, path, (*srv).URL, path))
		_ = json.NewEncoder(w).Encode([]Repo{
			{FullName: owner + "/a", DefaultBranch: "main"},
			{FullName: owner + "/b", DefaultBranch: "trunk", Fork: true},
		})
	})
}

func names(repos []Repo) string {
	var out []string
	for _, r := range repos {
		out = append(out, r.FullName)
	}
	return strings.Join(out, " ")
}

func TestOwnerReposOrgPaginates(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	pagedRepos(t, mux, &srv, "/orgs/acme/repos", "acme")
	srv = httptest.NewServer(mux)
	defer srv.Close()

	repos, err := NewGitHub(srv.URL, "tok").OwnerRepos(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(repos); got != "acme/a acme/b acme/c acme/old" {
		t.Errorf("repos = %s", got)
	}
	if repos[1].DefaultBranch != "trunk" || !repos[1].Fork || !repos[3].Archived {
		t.Errorf("metadata lost: %+v", repos)
	}
}

func TestOwnerReposFallsBackToUser(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/orgs/alice/repos", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"someone-else"}`))
	})
	pagedRepos(t, mux, &srv, "/users/alice/repos", "alice")
	srv = httptest.NewServer(mux)
	defer srv.Close()

	repos, err := NewGitHub(srv.URL, "tok").OwnerRepos(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(repos); got != "alice/a alice/b alice/c alice/old" {
		t.Errorf("repos = %s", got)
	}
}

func TestOwnerReposOwnUserIncludesPrivate(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/orgs/alice/repos", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"Alice"}`))
	})
	pagedRepos(t, mux, &srv, "/user/repos", "alice")
	srv = httptest.NewServer(mux)
	defer srv.Close()

	repos, err := NewGitHub(srv.URL, "tok").OwnerRepos(context.Background(), "alice")
	if err != nil || len(repos) != 4 {
		t.Fatalf("repos = %v, %v", names(repos), err)
	}
}

func TestPaginateStaysOnHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://evil.example/orgs/acme/repos?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	if _, err := NewGitHub(srv.URL, "tok").OwnerRepos(context.Background(), "acme"); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("err = %v, want a refusal to follow another host", err)
	}
}

func TestCreatePRFallsBackFromDraft(t *testing.T) {
	var drafts []bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Draft bool }
		_ = json.NewDecoder(r.Body).Decode(&req)
		drafts = append(drafts, req.Draft)
		if req.Draft {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"message":"Draft pull requests are not supported in this repository."}]}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":3,"html_url":"https://github.test/acme/app/pull/3"}`))
	}))
	defer srv.Close()
	pr, err := NewGitHub(srv.URL, "tok").CreatePR(context.Background(), "acme/app", "main", "doneram/updates", "t", "b", true)
	if err != nil || pr.Number != 3 || len(drafts) != 2 || drafts[1] {
		t.Errorf("pr = %+v, err = %v, drafts = %v", pr, err, drafts)
	}
}
