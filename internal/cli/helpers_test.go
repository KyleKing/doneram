package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// dockerfileWithoutDirectives has no `# doneram:` directives, so a run over
// it never reaches a resolver for them and the test stays offline.
const dockerfileWithoutDirectives = `FROM alpine:3.19 AS base
RUN apk add --no-cache curl
COPY --from=builder /app /app
`

func writeDockerfile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(dockerfileWithoutDirectives), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func runApp(t *testing.T, args ...string) error {
	t.Helper()
	app := NewApp("test", "test", "test")
	return app.Run(context.Background(), append([]string{"doneram"}, args...))
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restoring cwd: %v", err)
		}
	})
}
