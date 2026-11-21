package manifest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizePyPI(t *testing.T) {
	for in, want := range map[string]string{
		"Requests":          "requests",
		"typing_extensions": "typing-extensions",
		"zope.interface":    "zope-interface",
		"Foo__Bar-.-baz":    "foo-bar-baz",
		"ruamel.yaml.clib":  "ruamel-yaml-clib",
	} {
		if got := NormalizePyPI(in); got != want {
			t.Errorf("NormalizePyPI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"1.10.0", "1.9.9", 1},
		{"2.0.0rc1", "2.0.0", -1},
		{"1.0.0-beta.1", "1.0.0", -1},
		{"v1.2.4", "1.2.3", 1},
		{"1.2.3+local", "1.2.3", 0},
		{"0.28.1", "0.27", 1},
	} {
		if got := Compare(c.a, c.b); sign(got) != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

const pyproject = `[build-system]
requires = ["hatchling>=1.0"]  # not a dependency array

[project]
name = "demo"
classifiers = [
    "Programming Language :: Python :: 3",
    ["nested", "requests>=0.1"],
]
dependencies = [
    "requests[socks] >= 2.31, <3 ; python_version >= '3.8'",  # comment, "httpx>=0.1"
    'httpx>=0.27',
    "Typing_Extensions>=4",
    "attrs==23.1.0",
    "rich>=99.0",
    "pkg @ https://example.com/pkg.whl",
    "click",
    "esc\"aped>=1.0",
]
description = """
dependencies = ["requests>=0.1"]
"""

[project.optional-dependencies]
dev = ["pytest>=8.0,!=8.1.0"]
docs = [
  "mkdocs>=1.5",  # docs site
]

[dependency-groups]
lint = ["ruff>=0.4", {include-group = "dev"}]

[tool.uv]
dev-dependencies = ["mypy>=1.0"]
`

var lockedPy = map[string]string{
	"requests":          "2.32.3",
	"httpx":             "0.28.1",
	"typing-extensions": "4.12.2",
	"attrs":             "24.2.0",
	"rich":              "13.9.4",
	"pytest":            "8.3.3",
	"mkdocs":            "1.5",
	"ruff":              "0.7.0",
	"mypy":              "1.13.0",
	"hatchling":         "1.25.0",
	"click":             "8.1.7",
	"pkg":               "2.0.0",
	"esc-aped":          "2.0.0",
}

func TestRaisePyProject(t *testing.T) {
	got, raises := RaisePyProject([]byte(pyproject), lockedPy)
	want := strings.NewReplacer(
		"requests[socks] >= 2.31, <3", "requests[socks] >= 2.32.3, <3",
		"'httpx>=0.27'", "'httpx>=0.28.1'",
		`"Typing_Extensions>=4"`, `"Typing_Extensions>=4.12.2"`,
		"pytest>=8.0,!=8.1.0", "pytest>=8.3.3,!=8.1.0",
		"ruff>=0.4", "ruff>=0.7.0",
	).Replace(pyproject)
	if string(got) != want {
		t.Errorf("RaisePyProject:\n%s\nwant:\n%s", got, want)
	}
	wantRaises := []Raise{
		{"requests", ">= 2.31, <3", ">= 2.32.3, <3"},
		{"httpx", ">=0.27", ">=0.28.1"},
		{"Typing_Extensions", ">=4", ">=4.12.2"},
		{"pytest", ">=8.0,!=8.1.0", ">=8.3.3,!=8.1.0"},
		{"ruff", ">=0.4", ">=0.7.0"},
	}
	if !reflect.DeepEqual(raises, wantRaises) {
		t.Errorf("raises = %+v, want %+v", raises, wantRaises)
	}
}

func TestRaisePyProjectDottedKeys(t *testing.T) {
	in := "project.dependencies = [\"a>=1\"]\n[project]\noptional-dependencies.x = [\"b>=1\"]\n"
	got, _ := RaisePyProject([]byte(in), map[string]string{"a": "2", "b": "3"})
	want := "project.dependencies = [\"a>=2\"]\n[project]\noptional-dependencies.x = [\"b>=3\"]\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRaisePyProjectNeverLowers(t *testing.T) {
	in := "[project]\ndependencies = [\"rich>=99.0\", \"httpx>=0.28.1\"]\n"
	got, raises := RaisePyProject([]byte(in), map[string]string{"rich": "13.9.4", "httpx": "0.28.1"})
	if string(got) != in || len(raises) != 0 {
		t.Errorf("got %q (%v), want it untouched", got, raises)
	}
}

const packageJSON = `{
  "name": "demo",
  "version": "1.0.0",
  "scripts": {"dependencies": "not a section"},
  "dependencies": {
    "lodash": "^4.17.0",
    "react":   "~18.2",
    "left-pad": ">=1.0.0",
    "exact": "1.2.3",
    "star": "*",
    "tag": "latest",
    "url": "https://example.com/x.tgz",
    "ws": "workspace:^1.0.0",
    "local": "file:../local",
    "alias": "npm:other@^1.0.0",
    "range": ">=1.0.0 <2.0.0",
    "either": "^1.0.0 || ^2.0.0",
    "newer": "^9.0.0"
  },
  "devDependencies": {
    "@scope/pkg": "^0.2.0"
  },
  "optionalDependencies": {"fsevents": "^2.3.0"},
  "peerDependencies": {"react": "^17.0.0"}
}
`

var lockedNPM = map[string]string{
	"lodash":     "4.17.21",
	"react":      "18.2.5",
	"left-pad":   "1.3.0",
	"exact":      "1.2.3",
	"star":       "5.0.0",
	"tag":        "5.0.0",
	"url":        "5.0.0",
	"ws":         "1.5.0",
	"local":      "1.5.0",
	"alias":      "1.5.0",
	"range":      "1.5.0",
	"either":     "2.1.0",
	"newer":      "8.0.0",
	"@scope/pkg": "0.2.5",
	"fsevents":   "2.3.3",
}

func TestRaisePackageJSON(t *testing.T) {
	got, raises, err := RaisePackageJSON([]byte(packageJSON), lockedNPM)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`"^4.17.0"`, `"^4.17.21"`,
		`"~18.2"`, `"~18.2.5"`,
		`">=1.0.0",`, `">=1.3.0",`,
		`"^0.2.0"`, `"^0.2.5"`,
		`"^2.3.0"`, `"^2.3.3"`,
	).Replace(packageJSON)
	if string(got) != want {
		t.Errorf("RaisePackageJSON:\n%s\nwant:\n%s", got, want)
	}
	if len(raises) != 5 {
		t.Errorf("raises = %+v, want 5", raises)
	}
}

func TestRaisePackageJSONRejectsBadJSON(t *testing.T) {
	if _, _, err := RaisePackageJSON([]byte(`{"dependencies": {"a": "^1"`), nil); err == nil {
		t.Error("err = nil, want one for truncated JSON")
	}
}

func TestLockedUV(t *testing.T) {
	data := `version = 1
requires-python = ">=3.9"

[[package]]
name = "Typing_Extensions"
version = "4.12.2"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "numpy"
version = "2.1.0"
resolution-markers = ["python_full_version >= '3.10'"]

[[package]]
name = "numpy"
version = "2.0.2"
resolution-markers = ["python_full_version < '3.10'"]

[package.optional-dependencies]
x = [{ name = "y" }]

[[package]]
name = "demo"
version = "0.1.0"
source = { editable = "." }
`
	got := LockedUV([]byte(data))
	want := map[string]string{"typing-extensions": "4.12.2", "numpy": "2.0.2", "demo": "0.1.0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockedUV = %v, want %v", got, want)
	}
}

func TestLockedNPM(t *testing.T) {
	data := `{
  "lockfileVersion": 3,
  "packages": {
    "": {"name": "demo", "dependencies": {"lodash": "^4.17.0"}},
    "node_modules/lodash": {"version": "4.17.21"},
    "node_modules/@scope/pkg": {"version": "0.2.5"},
    "node_modules/a/node_modules/lodash": {"version": "3.0.0"},
    "packages/inner": {"version": "1.0.0"}
  }
}`
	got, err := LockedNPM([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"lodash": "4.17.21", "@scope/pkg": "0.2.5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockedNPM = %v, want %v", got, want)
	}
}

func TestLockedPNPM(t *testing.T) {
	v9 := `lockfileVersion: '9.0'

importers:

  .:
    dependencies:
      react-dom:
        specifier: ^18.2.0
        version: 18.3.1(react@18.3.1)
    devDependencies:
      typescript:
        specifier: ~5.4.0
        version: 5.4.5
  packages/inner:
    dependencies:
      lodash:
        specifier: ^4.0.0
        version: 4.17.21

packages:

  react-dom@18.3.1:
    resolution: {integrity: sha512-x}
`
	got, err := LockedPNPM([]byte(v9))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"react-dom": "18.3.1", "typescript": "5.4.5"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LockedPNPM(v9) = %v, want %v", got, want)
	}

	v5 := "lockfileVersion: 5.4\ndependencies:\n  lodash: 4.17.21\n"
	got, err = LockedPNPM([]byte(v5))
	if err != nil {
		t.Fatal(err)
	}
	if got["lodash"] != "4.17.21" {
		t.Errorf("LockedPNPM(v5) = %v", got)
	}
}

func TestWiden(t *testing.T) {
	for _, c := range []struct {
		eco, spec, latest, want string
	}{
		{"pypi", ">=2.31,<3", "3.1.0", ">=3.1.0,<4"},
		{"pypi", ">= 2.31, < 3.0.0", "3.0.0", ">= 3.0.0, < 4.0.0"},
		{"pypi", ">=2.0,<2.5", "2.7.1", ">=2.7.1,<2.8"},
		{"pypi", ">=0.4,<0.5", "0.5.0", ">=0.5.0,<0.6"},
		{"pypi", "<3", "4.2.0", "<5"},
		{"pypi", ">=1,<=2.0", "2.1.0", ">=2.1.0,<=2.1.0"},
		{"pypi", ">=2.31,<3,!=2.32.0", "3.0.0", ">=3.0.0,<4,!=2.32.0"},
		{"pypi", ">=2.31,<3", "2.32.3", ""},
		{"pypi", "~=2.31", "3.0.0", ""},
		{"pypi", "==2.31.0", "3.0.0", ""},
		{"pypi", ">=2.31", "9.0.0", ""},
		{"npm", "^4.17.21", "5.0.0", "^5.0.0"},
		{"npm", "^4.17.21", "4.18.0", ""},
		{"npm", "^0.2.3", "0.3.0", "^0.3.0"},
		{"npm", "^0.2.3", "0.2.9", ""},
		{"npm", "^0.0.3", "0.0.4", "^0.0.4"},
		{"npm", "^0", "0.9.0", ""},
		{"npm", "^4", "5.1.0", "^5.1.0"},
		{"npm", "~1.2.3", "1.3.0", "~1.3.0"},
		{"npm", "~1.2.3", "1.2.9", ""},
		{"npm", "~1", "1.9.0", ""},
		{"npm", ">=1.0.0", "9.0.0", ""},
		{"npm", "^4.17.21", "not-a-version", ""},
	} {
		got, ok := Widen(Constraint{Spec: c.spec, Ecosystem: c.eco}, c.latest)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Widen(%s %q, %s) = %q, %v; want %q", c.eco, c.spec, c.latest, got, ok, c.want)
		}
	}
}

func TestConstraintsAndSetSpec(t *testing.T) {
	py := PyProjectConstraints([]byte(pyproject))
	want := []Constraint{{Name: "requests", Spec: ">= 2.31, <3", Ecosystem: "pypi"}}
	if !reflect.DeepEqual(py, want) {
		t.Errorf("PyProjectConstraints = %+v, want %+v", py, want)
	}
	out, n := SetPyProjectSpec([]byte(pyproject), "Requests", ">= 2.31, <3", ">=3.1.0,<4")
	if n != 1 || !strings.Contains(string(out), `"requests[socks] >=3.1.0,<4 ; python_version >= '3.8'"`) {
		t.Errorf("SetPyProjectSpec changed %d:\n%s", n, out)
	}

	npm, err := PackageJSONConstraints([]byte(packageJSON))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range npm {
		names = append(names, c.Name+" "+c.Spec)
	}
	wantNames := []string{"lodash ^4.17.0", "react ~18.2", "newer ^9.0.0", "@scope/pkg ^0.2.0", "fsevents ^2.3.0"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Errorf("PackageJSONConstraints = %v, want %v", names, wantNames)
	}
	jout, n, err := SetPackageJSONSpec([]byte(packageJSON), "lodash", "^4.17.0", "^5.0.0")
	if err != nil || n != 1 || !strings.Contains(string(jout), `"lodash": "^5.0.0",`) {
		t.Errorf("SetPackageJSONSpec changed %d (%v):\n%s", n, err, jout)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"pyproject.toml", "uv.lock", "go.mod",
		"web/package.json", "web/package-lock.json",
		"app/package.json", "app/pnpm-lock.yaml", "app/package-lock.json",
		"nolock/pyproject.toml", "nolock/package.json",
		"web/node_modules/x/package.json", "web/node_modules/x/package-lock.json",
		".hidden/go.mod", "testdata/go.mod", "vendor/go.mod",
	} {
		write(f)
	}
	got, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, p := range got {
		rel, _ := filepath.Rel(root, p.Dir)
		tools = append(tools, rel+":"+p.Tool)
	}
	want := []string{".:uv", ".:go", "app:pnpm", "web:npm"}
	if !reflect.DeepEqual(tools, want) {
		t.Errorf("Find = %v, want %v", tools, want)
	}
}

// fakeRunner records every command and answers from a script.
type fakeRunner struct {
	calls  []string
	answer func(cmd string) (string, error)
}

func (f *fakeRunner) run(_ context.Context, _ string, argv []string) (string, error) {
	cmd := strings.Join(argv, " ")
	f.calls = append(f.calls, cmd)
	if f.answer == nil {
		return "", nil
	}
	return f.answer(cmd)
}

func TestUpgradeGoRetriesToolchain(t *testing.T) {
	attempts := 0
	f := &fakeRunner{answer: func(cmd string) (string, error) {
		if cmd != "go get -u ./..." {
			return "", nil
		}
		attempts++
		if attempts == 1 {
			return "go: example.com/a@v1.2.0 requires go >= 1.23.1\ngo: example.com/b@v1.0.0 requires go >= 1.24.0", errors.New("exit status 1")
		}
		return "go: upgraded example.com/a v1.1.0 => v1.2.0\ngo: added example.com/c v0.1.0\n", nil
	}}
	raises, err := Upgrade(context.Background(), f.run, Project{Tool: Go})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"go get -u ./...", "go get go@1.24.0", "go get -u ./...", "go mod tidy"}
	if !reflect.DeepEqual(f.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", f.calls, wantCalls)
	}
	if want := []Raise{{"example.com/a", "v1.1.0", "v1.2.0"}}; !reflect.DeepEqual(raises, want) {
		t.Errorf("raises = %+v, want %+v", raises, want)
	}
}

func TestUpgradeGoFailsWithoutToolchainHint(t *testing.T) {
	f := &fakeRunner{answer: func(string) (string, error) { return "network down", errors.New("exit status 1") }}
	if _, err := Upgrade(context.Background(), f.run, Project{Tool: Go}); err == nil {
		t.Fatal("err = nil, want the go get failure")
	}
	if len(f.calls) != 1 {
		t.Errorf("calls = %v, want a single attempt", f.calls)
	}
}

func TestUpgradeAndRelockCommands(t *testing.T) {
	for tool, want := range map[string][]string{
		UV:   {"uv lock --upgrade", "uv lock"},
		NPM:  {"npm update --package-lock-only", "npm install --package-lock-only --ignore-scripts"},
		PNPM: {"pnpm update --lockfile-only", "pnpm install --lockfile-only --ignore-scripts"},
	} {
		f := &fakeRunner{}
		p := Project{Tool: tool}
		if _, err := Upgrade(context.Background(), f.run, p); err != nil {
			t.Fatal(err)
		}
		if err := Relock(context.Background(), f.run, p); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.calls, want) {
			t.Errorf("%s calls = %v, want %v", tool, f.calls, want)
		}
	}
}

func TestSyncFloorsWritesOnlyOnChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies": {"lodash": "^4.17.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"packages": {"node_modules/lodash": {"version": "4.17.21"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Project{Dir: dir, Tool: NPM, Manifest: "package.json", Lock: "package-lock.json"}
	before := p.Fingerprint()
	raises, err := p.SyncFloors()
	if err != nil || len(raises) != 1 {
		t.Fatalf("raises = %v, err = %v", raises, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "package.json"))
	if string(data) != `{"dependencies": {"lodash": "^4.17.21"}}` {
		t.Errorf("package.json = %s", data)
	}
	if p.Fingerprint() == before {
		t.Error("fingerprint did not change")
	}
	again, _ := p.SyncFloors()
	if len(again) != 0 {
		t.Errorf("second sync raised %v, want nothing", again)
	}
}
