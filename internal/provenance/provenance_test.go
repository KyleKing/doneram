package provenance

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJudge(t *testing.T) {
	attested := Evidence{Attested: true, Repository: "npm/node-semver", Workflow: ".github/workflows/release.yml"}
	tests := []struct {
		name          string
		before, after Evidence
		changed       bool
		note          string
	}{
		{"both attested, same repo", attested, attested, false, ""},
		{"lost attestation", attested, Evidence{Publisher: "someone"}, true, "has no build attestation; 1.0.0 was built by npm/node-semver"},
		{"repo changed", attested, Evidence{Attested: true, Repository: "evil/fork"}, true, "was built from evil/fork; 1.0.0 came from npm/node-semver"},
		{"gained attestation", Evidence{Publisher: "a"}, attested, false, ""},
		{"publisher changed, unattested", Evidence{Publisher: "a"}, Evidence{Publisher: "b"}, true, "published by b; 1.0.0 by a"},
		{"neither attested, same publisher", Evidence{Publisher: "a"}, Evidence{Publisher: "a"}, false, ""},
	}
	for _, tt := range tests {
		v := Judge("pkg", "1.0.0", "1.0.1", tt.before, tt.after)
		if v.Changed != tt.changed || !strings.Contains(v.Note, tt.note) {
			t.Errorf("%s: Judge() = %+v, want changed=%v note containing %q", tt.name, v, tt.changed, tt.note)
		}
	}
}

func TestEvidence(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"predicate":{"buildDefinition":{"externalParameters":{"workflow":{"repository":"https://github.com/npm/node-semver","path":".github/workflows/release.yml"}}}}}`))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/semver/7.6.3":
			_, _ = w.Write([]byte(`{"_npmUser":{"name":"npm-cli-ops"},"dist":{"attestations":{"url":"` + server.URL + `/att/semver@7.6.3","provenance":{"predicateType":"https://slsa.dev/provenance/v1"}}}}`))
		case "/att/semver@7.6.3":
			_, _ = w.Write([]byte(`{"attestations":[{"predicateType":"https://github.com/npm/attestation/tree/main/specs/publish/v0.1","bundle":{"dsseEnvelope":{"payload":"e30="}}},{"predicateType":"https://slsa.dev/provenance/v1","bundle":{"dsseEnvelope":{"payload":"` + payload + `"}}}]}`))
		case "/semver/7.6.4":
			_, _ = w.Write([]byte(`{"_npmUser":{"name":"someone-else"},"dist":{}}`))
		case "/pypi/sampleproject/4.0.0/json":
			_, _ = w.Write([]byte(`{"urls":[{"filename":"a.tar.gz"},{"filename":"a.whl"}]}`))
		case "/integrity/sampleproject/4.0.0/a.whl/provenance":
			_, _ = w.Write([]byte(`{"attestation_bundles":[{"publisher":{"kind":"GitHub","repository":"pypa/sampleproject","workflow":"release.yml"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewWithBaseURLs(&http.Client{}, server.URL, server.URL)
	ctx := context.Background()

	ev, err := c.Evidence(ctx, "npm", "semver", "7.6.3")
	if err != nil || !ev.Attested || ev.Repository != "npm/node-semver" || ev.Workflow != ".github/workflows/release.yml" {
		t.Errorf("npm evidence = %+v, %v", ev, err)
	}
	v, err := c.Compare(ctx, "npm", "semver", "7.6.3", "7.6.4")
	if err != nil || !v.Changed {
		t.Errorf("Compare() = %+v, %v; want a lost attestation", v, err)
	}
	ev, err = c.Evidence(ctx, "pypi", "sampleproject", "4.0.0")
	if err != nil || !ev.Attested || ev.Repository != "pypa/sampleproject" {
		t.Errorf("pypi evidence = %+v, %v", ev, err)
	}
}
