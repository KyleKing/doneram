package resolver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
)

func TestDockerHubFiltersAndPages(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "3.13" {
			t.Errorf("name filter = %q", got)
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = fmt.Fprint(w, `{"results":[{"name":"3.13.18-slim"},{"name":"3.13.18"}]}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"next":"%s/v2/repositories/library/python/tags?name=3.13&page=2","results":[{"name":"3.13.17-slim-trixie"},{"name":"3.13.2-slim"}]}`, server.URL)
	}))
	defer server.Close()

	r := &DockerHubResolver{client: server.Client(), baseURL: server.URL}
	p := parser.ParsePattern("3.13.#-slim")
	got, err := r.Resolve(context.Background(), "python", p)
	if err != nil || got != "3.13.18-slim" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

func TestLiteralPrefix(t *testing.T) {
	for pattern, want := range map[string]string{"3.13.#": "3.13", "1.#.#-alpine*": "1", "#.#.#": "", "3.13.11": "3.13.11"} {
		if got := literalPrefix(parser.ParsePattern(pattern)); got != want {
			t.Errorf("%s: %q, want %q", pattern, got, want)
		}
	}
}
