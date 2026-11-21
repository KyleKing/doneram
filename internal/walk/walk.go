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
