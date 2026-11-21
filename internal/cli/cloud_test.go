package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/cloud"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

type amiResolver struct {
	id  string
	err error
}

func (amiResolver) Name() string { return "aws-ami" }
func (a amiResolver) Resolve(context.Context, string, *parser.VersionPattern) (string, error) {
	return a.id, a.err
}
func (amiResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}

// A cloud-backed pin moves when the CLI answers, and is skipped (exit 0)
// when this machine has no CLI or credentials for it.
func TestCloudSiteSkipsWithoutCredentials(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	config := `discover = false
lockfiles = false
manifests = false
tools {
  ["base-ami"] {
    resolver = "aws-ami"
    resolverName = "amazon/al2023-ami-2023.*-x86_64@us-east-1"
    sites { new { file = "main.tf"; pattern = #"ami\s*=\s*"(ami-[0-9a-f]+)""# } }
  }
}
`
	if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	tf := filepath.Join(dir, "main.tf")
	if err := os.WriteFile(tf, []byte(`ami = "ami-0aaaaaaaaaaaaaaaa"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "summary.json")
	unavailable := amiResolver{err: fmt.Errorf("%w: aws is not installed", cloud.ErrUnavailable)}
	if err := runCheckPkl(context.Background(), pklRun{
		path: cfg, output: out, format: "json", workers: 1, failOnDrift: true,
		resolvers: map[string]resolver.Resolver{"aws-ami": unavailable},
	}); err != nil {
		t.Fatalf("skipped site failed the run: %v", err)
	}
	if s := readSummary(t, out); len(s.Results) != 1 || !strings.Contains(s.Results[0].Skipped, "not installed") || s.Results[0].Error != "" {
		t.Fatalf("results = %+v", s.Results)
	}

	if err := runCheckPkl(context.Background(), pklRun{
		path: cfg, apply: true, output: out, format: "json", workers: 1,
		resolvers: map[string]resolver.Resolver{"aws-ami": amiResolver{id: "ami-0bbbbbbbbbbbbbbbb"}},
	}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(tf); !strings.Contains(string(data), "ami-0bbbbbbbbbbbbbbbb") {
		t.Errorf("main.tf = %s", data)
	}
}
