package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func names(pkgs []Package) []string {
	out := make([]string, len(pkgs))
	for i, p := range pkgs {
		out[i] = p.Name + "@" + p.Version
	}
	sort.Strings(out)
	return out
}

func TestParse(t *testing.T) {
	tests := []struct {
		file, content string
		eco           string
		want          []string
	}{
		{"uv.lock", `version = 1

[[package]]
name = "requests"
version = "2.19.0"
source = { registry = "https://pypi.org/simple" }

[package.optional-dependencies]
socks = [{ name = "pysocks" }]

[[package]]
name = "myapp"
version = "0.1.0"
source = { editable = "." }

[[package]]
name = "urllib3"
version = "1.24.1"
`, "PyPI", []string{"requests@2.19.0", "urllib3@1.24.1"}},
		{"Cargo.lock", `[[package]]
name = "time"
version = "0.1.43"
source = "registry+https://github.com/rust-lang/crates.io-index"
`, "crates.io", []string{"time@0.1.43"}},
		{"package-lock.json", `{"lockfileVersion": 3, "packages": {
  "": {"name": "root", "version": "1.0.0"},
  "node_modules/lodash": {"version": "4.17.15"},
  "node_modules/@scope/pkg": {"version": "1.0.0"},
  "node_modules/a/node_modules/lodash": {"version": "4.17.20"},
  "packages/local": {"link": true}
}}`, "npm", []string{"@scope/pkg@1.0.0", "lodash@4.17.15", "lodash@4.17.20"}},
		{"pnpm-lock.yaml", `lockfileVersion: '9.0'
packages:
  lodash@4.17.15:
    resolution: {integrity: sha512-x}
  '@babel/core@7.0.0(supports-color@5.5.0)':
    resolution: {integrity: sha512-y}
`, "npm", []string{"@babel/core@7.0.0", "lodash@4.17.15"}},
		{"go.mod", `module example.com/x

go 1.23

require github.com/a/b v1.2.3

require (
	golang.org/x/net v0.17.0 // indirect
	// comment
	golang.org/x/text v0.3.7
)
`, "Go", []string{"github.com/a/b@v1.2.3", "golang.org/x/net@v0.17.0", "golang.org/x/text@v0.3.7"}},
		{"Gemfile.lock", `GEM
  remote: https://rubygems.org/
  specs:
    nokogiri (1.13.0-x86_64-linux)
      racc (~> 1.4)
    rack (2.2.3)

PLATFORMS
  ruby
`, "RubyGems", []string{"nokogiri@1.13.0", "rack@2.2.3"}},
		{"composer.lock", `{"packages": [{"name": "guzzlehttp/guzzle", "version": "7.4.0"}], "packages-dev": [{"name": "phpunit/phpunit", "version": "v9.5.0"}]}`,
			"Packagist", []string{"guzzlehttp/guzzle@7.4.0", "phpunit/phpunit@9.5.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := write(t, t.TempDir(), tt.file, tt.content)
			got, err := Parse(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(names(got), tt.want) {
				t.Errorf("Parse() = %v, want %v", names(got), tt.want)
			}
			for _, p := range got {
				if p.Ecosystem != tt.eco || p.File != path {
					t.Errorf("package %+v, want ecosystem %s from %s", p, tt.eco, path)
				}
			}
		})
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "uv.lock", "")
	write(t, dir, "web/package-lock.json", "{}")
	write(t, dir, "web/node_modules/x/package-lock.json", "{}")
	write(t, dir, ".venv/uv.lock", "")
	write(t, dir, "README.md", "")

	got, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "uv.lock"), filepath.Join(dir, "web/package-lock.json")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Find() = %v, want %v", got, want)
	}
}

func TestFixCommand(t *testing.T) {
	tests := []struct {
		file, name, version string
		want                []string
	}{
		{"uv.lock", "urllib3", "1.26.5", []string{"uv", "lock", "--upgrade-package", "urllib3==1.26.5"}},
		{"Cargo.lock", "time", "0.2.23", []string{"cargo", "update", "--package", "time", "--precise", "0.2.23"}},
		{"go.mod", "golang.org/x/net", "0.23.0", []string{"go", "get", "golang.org/x/net@v0.23.0"}},
		{"unknown.lock", "x", "1", nil},
	}
	for _, tt := range tests {
		if got := FixCommand(filepath.Join("a", tt.file), tt.name, tt.version); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("FixCommand(%s) = %v, want %v", tt.file, got, tt.want)
		}
	}
}
