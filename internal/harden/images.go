package harden

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/digest"
)

const (
	// Image is a container image named by tag alone.
	Image Kind = "image"
	// Digest is an image pinned as tag@digest, checked for a rebuild of
	// the same tag or a tag the run just moved.
	Digest Kind = "digest"
)

var (
	fromLine    = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--\S+\s+)*(\S+)(?:\s+AS\s+(\S+))?`)
	copyFrom    = regexp.MustCompile(`(?i)^\s*COPY\s.*--from=(\S+)`)
	composeLine = regexp.MustCompile(`^\s*image:\s*['"]?([^'"\s#]+)`)
)

// imageSkipDirs are never descended into when looking for Dockerfiles.
var imageSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
}

func isImageFile(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case lower == "dockerfile", strings.HasPrefix(lower, "dockerfile."), strings.HasSuffix(lower, ".dockerfile"):
		return true
	case (strings.HasPrefix(lower, "compose") || strings.HasPrefix(lower, "docker-compose")) &&
		(strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")):
		return true
	}
	return false
}

func imageFiles(baseDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != baseDir && imageSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if isImageFile(d.Name()) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// scanImages returns every image reference with an explicit tag: Image
// findings for those with no digest, and, with digests set, Digest findings
// for those pinned already. An image built from a variable, scratch, or an
// earlier stage has nothing to pin.
func scanImages(baseDir string, digests bool) ([]Finding, error) {
	files, err := imageFiles(baseDir)
	if err != nil {
		return nil, err
	}
	return scanImageFiles(files, digests)
}

func scanImageFiles(files []string, digests bool) ([]Finding, error) {
	var out []Finding
	for _, file := range files {
		lines, err := readLines(file)
		if err != nil {
			return nil, err
		}
		stages := map[string]bool{}
		for i, line := range lines {
			var token string
			var at int
			if m := fromLine.FindStringSubmatchIndex(line); m != nil {
				token, at = line[m[2]:m[3]], m[2]
				if m[4] >= 0 {
					stages[strings.ToLower(line[m[4]:m[5]])] = true
				}
			} else if m := copyFrom.FindStringSubmatchIndex(line); m != nil {
				token, at = line[m[2]:m[3]], m[2]
			} else if m := composeLine.FindStringSubmatchIndex(line); m != nil && !strings.HasSuffix(strings.ToLower(file), "dockerfile") {
				token, at = line[m[2]:m[3]], m[2]
			} else {
				continue
			}
			if strings.Contains(token, "$") || strings.EqualFold(token, "scratch") || stages[strings.ToLower(token)] {
				continue
			}
			ref, err := digest.Parse(token)
			if err != nil || !ref.ExplicitTag() {
				continue
			}
			tagged := ref
			tagged.Digest = ""
			f := Finding{File: file, Line: i + 1, Name: tagged.String()}
			switch {
			case ref.Digest == "":
				f.Kind, f.Ref = Image, ref.Tag
				f.start, f.end = at, at+len(token)
			case digests:
				f.Kind, f.Ref = Digest, ref.Digest
				f.start = at + strings.Index(token, "@") + 1
				f.end = at + len(token)
			default:
				continue
			}
			out = append(out, f)
		}
	}
	return out, nil
}

// ScanDigests returns every image already pinned as tag@digest under
// baseDir. Resolving them finds a digest that no longer matches its tag,
// because the tag was rebuilt upstream or a run just moved it.
func ScanDigests(baseDir string) ([]Finding, error) {
	return scanImages(baseDir, true)
}

// DigestsIn is ScanDigests over the given files only.
func DigestsIn(files []string) ([]Finding, error) {
	return scanImageFiles(files, true)
}
