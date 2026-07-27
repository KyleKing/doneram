package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dockerfileWithoutDirectives has no `# doner:` directives, so processing it
// never reaches a resolver and the test stays offline.
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
	return app.Run(context.Background(), append([]string{"doner"}, args...))
}

func TestProcessCheckFileWithoutDirectives(t *testing.T) {
	dir := t.TempDir()
	path := writeDockerfile(t, dir, "Dockerfile")

	result := processCheckFile(context.Background(), path, ProcessorConfig{MaxWorkers: 1})

	if result.ProcessingErr != nil {
		t.Fatalf("ProcessingErr = %v", result.ProcessingErr)
	}
	if result.InstructionCnt != 3 {
		t.Errorf("InstructionCnt = %d, want 3", result.InstructionCnt)
	}
	if len(result.Updates) != 0 {
		t.Errorf("Updates = %+v, want none without directives", result.Updates)
	}
}

func TestProcessCheckFileMissingFile(t *testing.T) {
	result := processCheckFile(context.Background(), filepath.Join(t.TempDir(), "absent"), ProcessorConfig{MaxWorkers: 1})

	if result.ProcessingErr == nil {
		t.Fatal("ProcessingErr = nil, want a read error")
	}
	if !strings.Contains(result.ProcessingErr.Error(), "reading dockerfile") {
		t.Errorf("ProcessingErr = %v, want a read error", result.ProcessingErr)
	}
}

func TestProcessUpdateFileWithoutDirectives(t *testing.T) {
	dir := t.TempDir()
	path := writeDockerfile(t, dir, "Dockerfile")

	result := processUpdateFile(context.Background(), path, ProcessorConfig{MaxWorkers: 1, SkipBuild: true})

	if result.ProcessingErr != nil {
		t.Fatalf("ProcessingErr = %v", result.ProcessingErr)
	}
	if len(result.Updates) != 0 {
		t.Errorf("Updates = %+v, want none without directives", result.Updates)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(after) != dockerfileWithoutDirectives {
		t.Error("processUpdateFile rewrote a Dockerfile that had no updates")
	}
}

func TestProcessUpdateFileMissingFile(t *testing.T) {
	result := processUpdateFile(context.Background(), filepath.Join(t.TempDir(), "absent"), ProcessorConfig{MaxWorkers: 1, SkipBuild: true})

	if result.ProcessingErr == nil {
		t.Fatal("ProcessingErr = nil, want a read error")
	}
}

func TestRunCheckSingleFile(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "Dockerfile")

	if err := runApp(t, "check", "--file", "Dockerfile", "--format", "json"); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestRunCheckMultipleFilesReportsSummary(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "a.Dockerfile")
	writeDockerfile(t, dir, "b.Dockerfile")

	if err := runApp(t, "check", "--file", "*.Dockerfile"); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestRunCheckUnmatchedPattern(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runApp(t, "check", "--file", "*.Dockerfile"); err == nil {
		t.Error("check should fail when no file matches the pattern")
	}
}

func TestRunCheckReportsProcessingFailure(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "good.Dockerfile")
	if err := os.Mkdir(filepath.Join(dir, "bad.Dockerfile"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	err := runApp(t, "check", "--file", "*.Dockerfile")
	if err == nil {
		t.Fatal("check should fail when a matched path cannot be read")
	}
	if !strings.Contains(err.Error(), "failed processing") {
		t.Errorf("err = %v, want a processing failure summary", err)
	}
}

func TestRunUpdateSkipBuild(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "Dockerfile")

	if err := runApp(t, "update", "--file", "Dockerfile", "--skip-build", "--skip-healthcheck"); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestRunUpdateMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "a.Dockerfile")
	writeDockerfile(t, dir, "b.Dockerfile")

	if err := runApp(t, "update", "--file", "*.Dockerfile", "--skip-build", "--format", "github-actions"); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestRunUpdateUnmatchedPattern(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := runApp(t, "update", "--file", "*.Dockerfile", "--skip-build"); err == nil {
		t.Error("update should fail when no file matches the pattern")
	}
}

func TestRunUpdateReportsProcessingFailure(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "good.Dockerfile")
	if err := os.Mkdir(filepath.Join(dir, "bad.Dockerfile"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := runApp(t, "update", "--file", "*.Dockerfile", "--skip-build"); err == nil {
		t.Error("update should fail when a matched path cannot be read")
	}
}

func TestRunCheckVerbose(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "Dockerfile")

	if err := runApp(t, "--verbose", "check", "--file", "Dockerfile"); err != nil {
		t.Fatalf("check: %v", err)
	}
}
