package cli

import "os"

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
