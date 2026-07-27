package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("FROM alpine:3.19\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
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

func TestExpandFilesDefaultsToDockerfile(t *testing.T) {
	got, err := expandFiles(nil)
	if err != nil {
		t.Fatalf("expandFiles(nil): %v", err)
	}
	if len(got) != 1 || got[0] != "Dockerfile" {
		t.Errorf("expandFiles(nil) = %v, want [Dockerfile]", got)
	}
}

func TestExpandFilesExactPath(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "Dockerfile"))

	got, err := expandFiles([]string{"Dockerfile"})
	if err != nil {
		t.Fatalf("expandFiles: %v", err)
	}
	if len(got) != 1 || got[0] != "Dockerfile" {
		t.Errorf("got %v, want [Dockerfile]", got)
	}
}

func TestExpandFilesGlob(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "a.Dockerfile"))
	writeFile(t, filepath.Join(dir, "b.Dockerfile"))

	got, err := expandFiles([]string{"*.Dockerfile"})
	if err != nil {
		t.Fatalf("expandFiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2 matches", got)
	}
}

func TestExpandFilesDeduplicates(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "Dockerfile"))

	got, err := expandFiles([]string{"Dockerfile", "Dockerfile", "*.Dockerfile"})
	if err != nil {
		t.Fatalf("expandFiles: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want a single deduplicated match", got)
	}
}

func TestExpandFilesRecursive(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "Dockerfile"))
	writeFile(t, filepath.Join(dir, "nested", "deep", "Dockerfile"))
	writeFile(t, filepath.Join(dir, "nested", "notes.txt"))

	got, err := expandFiles([]string{"**/Dockerfile"})
	if err != nil {
		t.Fatalf("expandFiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2 Dockerfiles", got)
	}
}

func TestExpandFilesRecursiveWithBaseDir(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "Dockerfile"))
	writeFile(t, filepath.Join(dir, "nested", "Dockerfile"))

	got, err := expandFiles([]string{"nested/**/Dockerfile"})
	if err != nil {
		t.Fatalf("expandFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %v, want 1 match under nested/", got)
	}
	if filepath.Base(filepath.Dir(got[0])) != "nested" {
		t.Errorf("got %v, want a match under nested/", got)
	}
}

func TestExpandFilesRecursiveEmptySuffixMatchesEverything(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "Dockerfile"))
	writeFile(t, filepath.Join(dir, "notes.txt"))

	got, err := expandFiles([]string{"**"})
	if err != nil {
		t.Fatalf("expandFiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want both files", got)
	}
}

func TestExpandFilesRejectsMultipleRecursiveWildcards(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if _, err := expandFiles([]string{"**/a/**/Dockerfile"}); err == nil {
		t.Error("expandFiles should reject a pattern with two ** segments")
	}
}

func TestExpandFilesNoMatches(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if _, err := expandFiles([]string{"missing.Dockerfile"}); err == nil {
		t.Error("expandFiles should error when nothing matches")
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	writeFile(t, path)

	if !fileExists(path) {
		t.Error("fileExists should report true for a regular file")
	}
	if fileExists(dir) {
		t.Error("fileExists should report false for a directory")
	}
	if fileExists(filepath.Join(dir, "missing")) {
		t.Error("fileExists should report false for a missing path")
	}
}
