// Package walk holds the rule every repo scan shares about which
// directories belong to the repo being checked.
package walk

import (
	"os"
	"path/filepath"
)

// NestedRepo reports whether dir, below root, is the top of another git
// checkout: a nested repository, a submodule, or a worktree (whose .git is
// a file). Its files belong to that checkout, which is checked on its own,
// so a scan of root stops there.
func NestedRepo(root, dir string) bool {
	if filepath.Clean(root) == filepath.Clean(dir) {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// skipNames are directories that hold someone else's code (vendored
// dependencies, virtualenvs), build output, or test inputs, none of which
// pins this repo ships.
var skipNames = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"target": true, "dist": true, "build": true, "testdata": true, "fixtures": true,
	"tests": true, "__tests__": true,
}

// Skip reports whether a scan of root should not descend into dir.
func Skip(root, dir string) bool {
	if filepath.Clean(root) == filepath.Clean(dir) {
		return false
	}
	return skipNames[filepath.Base(dir)] || NestedRepo(root, dir)
}
