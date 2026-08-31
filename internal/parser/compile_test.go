package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/doneram/internal/locator"
)

func writeDockerfile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestCompileLocatorsFromMultiStageDockerfile(t *testing.T) {
	content := `# doneram: golang:1.21.#
FROM golang:1.21.5 AS builder
RUN go build -o app

# doneram: alpine:3.19.#
FROM alpine:3.19.0
COPY --from=builder /build/app /app
`
	path := writeDockerfile(t, content)

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	sites := CompileLocators(path, df)
	if len(sites) != 2 {
		t.Fatalf("sites = %+v, want 2", sites)
	}

	for _, want := range []struct {
		tool, resolverName string
	}{
		{"golang", "golang"},
		{"alpine", "alpine"},
	} {
		found := false
		for _, s := range sites {
			if s.Tool == want.tool {
				found = true
				if s.ResolverName != want.resolverName {
					t.Errorf("site %q ResolverName = %q, want %q", want.tool, s.ResolverName, want.resolverName)
				}
				if s.Locator.Resolver != "docker" {
					t.Errorf("site %q Resolver = %q, want docker", want.tool, s.Locator.Resolver)
				}

				matches, err := locator.Find(s.Locator)
				if err != nil {
					t.Fatalf("Find(%q): %v", want.tool, err)
				}
				if err := locator.CheckExpect(s.Locator, matches); err != nil {
					t.Errorf("CheckExpect(%q): %v", want.tool, err)
				}
			}
		}
		if !found {
			t.Errorf("no compiled site for tool %q", want.tool)
		}
	}
}

func TestCompileLocatorsSkipsIgnoredAndUndirected(t *testing.T) {
	content := `# doneram: ignore
FROM legacy-image:1.0.0
WORKDIR /app
RUN echo hi
`
	path := writeDockerfile(t, content)

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	sites := CompileLocators(path, df)
	if len(sites) != 0 {
		t.Errorf("sites = %+v, want none for an ignored directive", sites)
	}
}

func TestCompileLocatorsUsesGHCRResolver(t *testing.T) {
	content := `# doneram: doneram:v#.#.#
FROM ghcr.io/kyleking/doneram:v1.2.3
`
	path := writeDockerfile(t, content)

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	sites := CompileLocators(path, df)
	if len(sites) != 1 {
		t.Fatalf("sites = %+v, want 1", sites)
	}
	if sites[0].Locator.Resolver != "ghcr" {
		t.Errorf("Resolver = %q, want ghcr", sites[0].Locator.Resolver)
	}
	if sites[0].ResolverName != "ghcr.io/kyleking/doneram" {
		t.Errorf("ResolverName = %q, want ghcr.io/kyleking/doneram", sites[0].ResolverName)
	}
}
