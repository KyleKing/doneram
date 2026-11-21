package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/resolver"
)

const versionsTF = `terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    google = {
      version = "~> 5.0"
      source  = "hashicorp/google"
    }
    random = { source = "hashicorp/random", version = "3.5.1" }
    local = {
      source  = "hashicorp/local"
      version = ">= 2.0"
    }
  }
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.1.2"
}

module "local" {
  source = "./modules/x"
}

module "git" {
  source  = "git::https://example.com/x.git"
  version = "1.0.0"
}
`

func TestTerraformSites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "versions.tf")
	if err := os.WriteFile(path, []byte(versionsTF), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".terraform"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".terraform", "x.tf"), []byte(versionsTF), 0o644); err != nil {
		t.Fatal(err)
	}

	sites, err := terraformSites(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range sites {
		matches, err := locator.Find(s.Locator)
		if err != nil {
			t.Fatalf("%s: %v", s.Tool, err)
		}
		var vals []string
		for _, m := range matches {
			vals = append(vals, m.Value)
		}
		c := ""
		if s.Constraint != nil {
			c = " " + s.Constraint.Raw
		}
		got = append(got, s.Locator.Resolver+":"+s.Tool+"="+strings.Join(vals, ",")+c)
	}
	want := []string{
		"terraform-provider:hashicorp/aws=5.0 5.#.#",
		"terraform-provider:hashicorp/google=5.0 5.#.#",
		"terraform-provider:hashicorp/random=3.5.1",
		"terraform-module:terraform-aws-modules/vpc/aws=5.1.2",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sites =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// End to end: the aws pin moves within its major at its own precision,
	// and google's identical version line is left alone.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"versions":[{"version":"5.51.0"},{"version":"6.2.0"},{"version":"5.9.1"}]}`))
	}))
	defer server.Close()
	tf := resolver.NewTerraformProviderResolver(&http.Client{}).WithBaseURL(server.URL)
	lookup := func(string) (resolver.Resolver, bool) { return tf, true }
	results := engine.RunSites(context.Background(), sites[:1], lookup)
	if results[0].Err != nil || results[0].Latest != "5.51" {
		t.Fatalf("result = %+v", results[0])
	}
	if _, err := locator.Patch(path, sites[0].Locator.Pattern, results[0].Latest); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "source  = \"hashicorp/aws\"\n      version = \"~> 5.51\"") || strings.Count(string(data), "\"~> 5.0\"") != 1 {
		t.Errorf("patched file =\n%s", data)
	}
}
