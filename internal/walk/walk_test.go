package walk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNestedRepo(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{".git", "sub/.git/x", "plain"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wt", ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{root: false, filepath.Join(root, "sub"): true, filepath.Join(root, "wt"): true, filepath.Join(root, "plain"): false}
	for dir, want := range cases {
		if got := NestedRepo(root, dir); got != want {
			t.Errorf("NestedRepo(%s) = %v, want %v", dir, got, want)
		}
	}
}
