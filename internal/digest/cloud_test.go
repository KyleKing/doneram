package digest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/cloud"
)

const cloudDigest = "sha256:" + "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"

func stubRun(out string, err error, calls *[]string) cloud.Runner {
	return func(_ context.Context, name string, args ...string) (string, error) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return out, err
	}
}

func TestECRBasicLogin(t *testing.T) {
	const host = "123456789012.dkr.ecr.us-west-2.amazonaws.com"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "AWS" || pass != "ecr-pass" {
			w.Header().Set("WWW-Authenticate", `Basic realm="https://`+host+`/",service="ecr.amazonaws.com"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", cloudDigest)
	}))
	defer srv.Close()

	var calls []string
	c := New(srv.Client()).WithRegistryURL(host, srv.URL)
	c.run = stubRun("ecr-pass", nil, &calls)
	got, err := c.Resolve(context.Background(), Ref{Registry: host, Repository: "app", Tag: "1.2.3"})
	if err != nil || got != cloudDigest {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if len(calls) != 1 || calls[0] != "aws ecr get-login-password --region us-west-2" {
		t.Errorf("calls = %v", calls)
	}

	// With no aws CLI or login, the image is skipped, not failed.
	c = New(srv.Client()).WithRegistryURL(host, srv.URL)
	c.run = stubRun("", fmt.Errorf("%w: aws is not installed", cloud.ErrUnavailable), &calls)
	if _, err := c.Resolve(context.Background(), Ref{Registry: host, Repository: "app", Tag: "1.2.3"}); !errors.Is(err, cloud.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}

func TestArtifactRegistryLogin(t *testing.T) {
	const host = "us-docker.pkg.dev"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/token":
			if user, pass, ok := r.BasicAuth(); ok && user == "oauth2accesstoken" && pass == "gcp-token" {
				_, _ = fmt.Fprint(w, `{"token":"private"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"token":"anonymous"}`)
		case r.Header.Get("Authorization") == "Bearer private",
			r.Header.Get("Authorization") == "Bearer anonymous" && strings.Contains(r.URL.Path, "/public/"):
			w.Header().Set("Docker-Content-Digest", cloudDigest)
		default:
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/v2/token",service="`+host+`"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	var calls []string
	c := New(srv.Client()).WithRegistryURL(host, srv.URL)
	c.run = stubRun("gcp-token", nil, &calls)
	if got, err := c.Resolve(context.Background(), Ref{Registry: host, Repository: "proj/private/app", Tag: "1"}); err != nil || got != cloudDigest {
		t.Fatalf("private = %q, %v", got, err)
	}

	// No gcloud: a public image still pulls anonymously, a private one is
	// skipped.
	c = New(srv.Client()).WithRegistryURL(host, srv.URL)
	c.run = stubRun("", fmt.Errorf("%w: gcloud is not installed", cloud.ErrUnavailable), &calls)
	if got, err := c.Resolve(context.Background(), Ref{Registry: host, Repository: "proj/public/app", Tag: "1"}); err != nil || got != cloudDigest {
		t.Errorf("public = %q, %v", got, err)
	}
	if _, err := c.Resolve(context.Background(), Ref{Registry: host, Repository: "proj/private/app", Tag: "1"}); !errors.Is(err, cloud.ErrUnavailable) {
		t.Errorf("private err = %v, want ErrUnavailable", err)
	}
}
