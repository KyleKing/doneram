package config

import (
	"os"
	"path/filepath"
	"testing"
)

const fixturePkl = `
class Site {
  file: String
  pattern: String
  expect: Int(this > 0) = 1
}

class Tool {
  resolver: String = "mise"
  resolverName: String?
  sites: Listing<Site>
}

afterPatch: String = "./sync.sh"

tools: Mapping<String, Tool> = new {
  ["jq"] = new {
    sites = new {
      new Site {
        file = "conf.toml"
        pattern = #"jq = "([\d.]+)""#
      }
    }
  }
  ["hk"] = new {
    resolverName = "aqua:jdx/hk"
    sites = new {
      new Site {
        file = "hk.pkl"
        pattern = #"hk@([\d.]+)#"#
        expect = 2
      }
    }
  }
}
`

func TestLoadAndCompileSites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(path, []byte(fixturePkl), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.AfterPatch != "./sync.sh" {
		t.Errorf("AfterPatch = %q, want ./sync.sh", cfg.AfterPatch)
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("Tools = %+v, want 2 entries", cfg.Tools)
	}
	if hk := cfg.Tools["hk"]; hk.ResolverName != "aqua:jdx/hk" || len(hk.Sites) != 1 || hk.Sites[0].Expect != 2 {
		t.Errorf("Tools[hk] = %+v, want resolverName aqua:jdx/hk and one site with expect=2", hk)
	}

	sites := cfg.Sites(dir)
	if len(sites) != 2 {
		t.Fatalf("Sites = %+v, want 2", sites)
	}

	byTool := make(map[string]bool)
	for _, s := range sites {
		byTool[s.Tool] = true
		if s.Locator.Glob != filepath.Join(dir, cfg.Tools[s.Tool].Sites[0].File) {
			t.Errorf("Site(%s).Locator.Glob = %q, want it joined with baseDir", s.Tool, s.Locator.Glob)
		}
	}
	if !byTool["jq"] || !byTool["hk"] {
		t.Errorf("Sites = %+v, want jq and hk", sites)
	}
}

func TestToolConstraintFromHold(t *testing.T) {
	max := "3.0.0"
	tool := Tool{Hold: &Hold{Reason: "cgo breaks on 3.0", Max: &max}}

	constraint := tool.constraint()
	if constraint == nil {
		t.Fatal("constraint() = nil, want a ceiling-bounded pattern")
	}
	if constraint.Ceiling != "3.0.0" {
		t.Errorf("Ceiling = %q, want 3.0.0", constraint.Ceiling)
	}
	if constraint.HoldReason != "cgo breaks on 3.0" {
		t.Errorf("HoldReason = %q, want %q", constraint.HoldReason, "cgo breaks on 3.0")
	}

	if (Tool{}).constraint() != nil {
		t.Error("constraint() for a tool without a hold should be nil")
	}
}

func TestSitesEmitsCommandSite(t *testing.T) {
	cfg := &Config{
		Tools: map[string]Tool{
			"eslint": {
				Command:        "npm outdated",
				CommandPattern: `^(?P<name>\S+)\s+(?P<current>\S+)\s+(?P<latest>\S+)$`,
			},
		},
	}

	sites := cfg.Sites("/repo")
	if len(sites) != 1 {
		t.Fatalf("Sites = %+v, want 1", sites)
	}
	if sites[0].Command != "npm outdated" {
		t.Errorf("Command = %q, want npm outdated", sites[0].Command)
	}
	if sites[0].Locator.Glob != "" {
		t.Errorf("Locator.Glob = %q, want empty for a command site", sites[0].Locator.Glob)
	}
}

func TestLoadInvalidPklReportsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(path, []byte("this is not pkl {{{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Error("Load should fail on invalid pkl")
	}
}
