package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/walk"
)

// isDockerfile matches Dockerfile, Dockerfile.<x>, and <x>.Dockerfile.
func isDockerfile(name string) bool {
	lower := strings.ToLower(name)
	return lower == "dockerfile" || strings.HasPrefix(lower, "dockerfile.") || strings.HasSuffix(lower, ".dockerfile")
}

// directiveSites compiles every `# doneram:` directive in the repo's
// Dockerfiles into a site, so a directive pin runs through the same engine
// as a config site: cooldown, advisories, provenance, and PR groups.
func directiveSites(baseDir string) ([]engine.Site, error) {
	var out []engine.Site
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if walk.Skip(baseDir, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDockerfile(d.Name()) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		df, err := parser.Parse(string(content))
		if err != nil {
			return nil
		}
		for _, c := range parser.CompileLocators(path, df) {
			out = append(out, engine.Site{
				Tool:         c.Tool,
				ResolverName: c.ResolverName,
				Constraint:   c.Constraint,
				Locator:      c.Locator,
			})
		}
		return nil
	})
	return out, err
}
