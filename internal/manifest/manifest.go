// Package manifest keeps a project's declared dependency constraints in step
// with what its lockfile resolves. The package manager upgrades the lock
// within the declared ranges; doneram then raises each declared floor
// (`>=X` in pyproject.toml, `^X` in package.json) to the version now locked
// and hands the manifest back to the package manager to relock, so the
// floor never claims support for a release nobody tests against anymore.
//
// Resolving stays with uv, npm, pnpm, and go. This package only finds the
// projects, edits manifest text in place (formatting, comments, and every
// clause it does not own left as written), and names the commands to run.
package manifest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Tool names the package manager that owns a project.
const (
	UV   = "uv"
	NPM  = "npm"
	PNPM = "pnpm"
	Go   = "go"
)

// Project is one directory whose manifest and lockfile a package manager
// owns together.
type Project struct {
	// Dir is the project's absolute directory.
	Dir string
	// Tool is the package manager: uv, npm, pnpm, or go.
	Tool string
	// Manifest and Lock are file names inside Dir. Lock is go.sum for Go,
	// which may not exist yet.
	Manifest string
	Lock     string
}

// skipDirs are never descended into: installed or vendored copies of a
// graph, build output, and fixtures that only look like projects.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "venv": true, "dist": true,
	"build": true, "testdata": true, "target": true,
}

// Find returns every project under root, sorted by directory. A
// pyproject.toml counts only beside a uv.lock, and a package.json only
// beside a package-lock.json or pnpm-lock.yaml: without a lock there is
// no locked version to raise a floor to. When both node locks exist,
// pnpm-lock.yaml wins, since a stray package-lock.json is the usual
// leftover of someone running npm once in a pnpm repo.
func Find(root string) ([]Project, error) {
	var out []Project
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		exists := func(name string) bool {
			info, err := os.Stat(filepath.Join(path, name))
			return err == nil && !info.IsDir()
		}
		if exists("pyproject.toml") && exists("uv.lock") {
			out = append(out, Project{Dir: abs, Tool: UV, Manifest: "pyproject.toml", Lock: "uv.lock"})
		}
		if exists("package.json") {
			switch {
			case exists("pnpm-lock.yaml"):
				out = append(out, Project{Dir: abs, Tool: PNPM, Manifest: "package.json", Lock: "pnpm-lock.yaml"})
			case exists("package-lock.json"):
				out = append(out, Project{Dir: abs, Tool: NPM, Manifest: "package.json", Lock: "package-lock.json"})
			}
		}
		if exists("go.mod") {
			out = append(out, Project{Dir: abs, Tool: Go, Manifest: "go.mod", Lock: "go.sum"})
		}
		return nil
	})
	sort.SliceStable(out, func(a, b int) bool { return out[a].Dir < out[b].Dir })
	return out, err
}

// Ecosystem is the registry a project's declared constraints resolve
// against ("pypi" or "npm"), empty for Go, whose requires are floors
// already.
func (p Project) Ecosystem() string {
	switch p.Tool {
	case UV:
		return "pypi"
	case NPM, PNPM:
		return "npm"
	}
	return ""
}

// UpgradeCommand is the first command Upgrade runs, shown to a user whose
// machine lacks the tool.
func (p Project) UpgradeCommand() []string {
	switch p.Tool {
	case UV:
		return []string{"uv", "lock", "--upgrade"}
	case NPM:
		return []string{"npm", "update", "--package-lock-only"}
	case PNPM:
		return []string{"pnpm", "update", "--lockfile-only"}
	case Go:
		return []string{"go", "get", "-u", "./..."}
	}
	return nil
}

// RelockCommand rewrites the lock to match an edited manifest without
// moving anything the manifest still allows. Go has none: go.mod is both.
func (p Project) RelockCommand() []string {
	switch p.Tool {
	case UV:
		return []string{"uv", "lock"}
	case NPM:
		return []string{"npm", "install", "--package-lock-only", "--ignore-scripts"}
	case PNPM:
		return []string{"pnpm", "install", "--lockfile-only", "--ignore-scripts"}
	}
	return nil
}

// Installed reports whether the project's package manager is on PATH.
func (p Project) Installed() bool {
	_, err := exec.LookPath(p.Tool)
	return err == nil
}

// Fingerprint hashes the manifest and lock, so a caller can tell whether
// an upgrade changed anything.
func (p Project) Fingerprint() string {
	h := sha256.New()
	for _, name := range []string{p.Manifest, p.Lock} {
		data, _ := os.ReadFile(filepath.Join(p.Dir, name))
		_, _ = fmt.Fprintf(h, "%s:%d:", name, len(data))
		_, _ = h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Runner runs argv from dir and returns its combined output. Exec is the
// real one; tests substitute their own.
type Runner func(ctx context.Context, dir string, argv []string) (string, error)

// Exec runs argv as a subprocess.
func Exec(ctx context.Context, dir string, argv []string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("`%s`: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(lastLines(string(out), 20)))
	}
	return string(out), nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Raise is one declared constraint moved up: Old and New are the spec as
// written before and after (">=2.31,<3" to ">=2.32.3,<3", "^4.17.0" to
// "^4.17.21", or a Go require's version).
type Raise struct {
	Name string
	Old  string
	New  string
}

var (
	goRequires = regexp.MustCompile(`requires go >= ([0-9][0-9.]*)`)
	goUpgraded = regexp.MustCompile(`(?m)^go: upgraded (\S+) (\S+) => (\S+)$`)
)

// Upgrade moves every locked dependency to the newest release its declared
// range allows. For Go, whose requires are the floors, the upgraded
// modules come back as raises.
//
// A Go dependency can demand a newer toolchain than the go directive
// names; `go get go@X` raises the directive (and fetches that toolchain
// for the retry), so that one failure is retried once rather than holding
// back every other module.
func Upgrade(ctx context.Context, run Runner, p Project) ([]Raise, error) {
	if p.Tool != Go {
		_, err := run(ctx, p.Dir, p.UpgradeCommand())
		return nil, err
	}
	out, err := run(ctx, p.Dir, p.UpgradeCommand())
	if err != nil {
		ver := requiredGo(out + err.Error())
		if ver == "" {
			return nil, err
		}
		if _, err := run(ctx, p.Dir, []string{"go", "get", "go@" + ver}); err != nil {
			return nil, err
		}
		if out, err = run(ctx, p.Dir, p.UpgradeCommand()); err != nil {
			return nil, err
		}
	}
	if _, err := run(ctx, p.Dir, []string{"go", "mod", "tidy"}); err != nil {
		return nil, err
	}
	var raises []Raise
	for _, m := range goUpgraded.FindAllStringSubmatch(out, -1) {
		raises = append(raises, Raise{Name: m[1], Old: m[2], New: m[3]})
	}
	return raises, nil
}

// requiredGo is the newest toolchain any "requires go >= X" line in a
// failed `go get` asks for, or empty when the failure is something else.
func requiredGo(out string) string {
	best := ""
	for _, m := range goRequires.FindAllStringSubmatch(out, -1) {
		if v := strings.TrimRight(m[1], "."); best == "" || Compare(v, best) > 0 {
			best = v
		}
	}
	return best
}

// Relock runs the project's relock command, a no-op for Go.
func Relock(ctx context.Context, run Runner, p Project) error {
	argv := p.RelockCommand()
	if argv == nil {
		return nil
	}
	_, err := run(ctx, p.Dir, argv)
	return err
}

// Locked reads the version the project's lock resolves for each direct
// dependency, keyed the way the manifest's edit functions look names up.
func (p Project) Locked() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(p.Dir, p.Lock))
	if err != nil {
		return nil, err
	}
	switch p.Tool {
	case UV:
		return LockedUV(data), nil
	case NPM:
		return LockedNPM(data)
	case PNPM:
		return LockedPNPM(data)
	}
	return nil, nil
}

// SyncFloors raises each declared floor in the manifest to the version
// the lock now resolves, rewriting the file only when one moved.
func (p Project) SyncFloors() ([]Raise, error) {
	if p.Tool == Go {
		return nil, nil
	}
	locked, err := p.Locked()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p.Lock, err)
	}
	return p.edit(func(data []byte) ([]byte, []Raise, error) {
		if p.Tool == UV {
			out, raises := RaisePyProject(data, locked)
			return out, raises, nil
		}
		return RaisePackageJSON(data, locked)
	})
}

// Constraints lists the declared constraints with an upper bound a new
// release can fall outside of.
func (p Project) Constraints() ([]Constraint, error) {
	data, err := os.ReadFile(filepath.Join(p.Dir, p.Manifest))
	if err != nil {
		return nil, err
	}
	switch p.Tool {
	case UV:
		return PyProjectConstraints(data), nil
	case NPM, PNPM:
		return PackageJSONConstraints(data)
	}
	return nil, nil
}

// SetSpec replaces the spec of every declaration of name that reads old
// with spec, and reports whether anything changed.
func (p Project) SetSpec(name, old, spec string) (bool, error) {
	raises, err := p.edit(func(data []byte) ([]byte, []Raise, error) {
		if p.Tool == UV {
			out, n := SetPyProjectSpec(data, name, old, spec)
			return out, make([]Raise, n), nil
		}
		out, n, err := SetPackageJSONSpec(data, name, old, spec)
		return out, make([]Raise, n), err
	})
	return len(raises) > 0, err
}

func (p Project) edit(f func([]byte) ([]byte, []Raise, error)) ([]Raise, error) {
	path := filepath.Join(p.Dir, p.Manifest)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out, raises, err := f(data)
	if err != nil {
		return nil, fmt.Errorf("editing %s: %w", path, err)
	}
	if len(raises) == 0 || bytes.Equal(out, data) {
		return raises, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
		return nil, err
	}
	return raises, nil
}

// edit is one replacement of data[start:end].
type edit struct {
	start, end int
	text       string
}

// applyEdits applies non-overlapping edits, in any order.
func applyEdits(data []byte, edits []edit) []byte {
	if len(edits) == 0 {
		return data
	}
	sort.Slice(edits, func(a, b int) bool { return edits[a].start < edits[b].start })
	var buf bytes.Buffer
	prev := 0
	for _, e := range edits {
		buf.Write(data[prev:e.start])
		buf.WriteString(e.text)
		prev = e.end
	}
	buf.Write(data[prev:])
	return buf.Bytes()
}
