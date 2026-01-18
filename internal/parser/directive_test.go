package parser

import (
	"testing"
)

func TestParseDirective_SinglePackage(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantPackage string
		wantPattern string
	}{
		{
			name:        "simple package with pattern",
			line:        "# doner: python:3.11.#",
			wantPackage: "python",
			wantPattern: "3.11.#",
		},
		{
			name:        "package without pattern",
			line:        "# doner: python",
			wantPackage: "python",
			wantPattern: "#.#.#",
		},
		{
			name:        "package with full semver",
			line:        "# doner: requests:2.31.0",
			wantPackage: "requests",
			wantPattern: "2.31.0",
		},
		{
			name:        "package with wildcard",
			line:        "# doner: flask:*",
			wantPackage: "flask",
			wantPattern: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := ParseDirective(tt.line, 1)
			if d == nil {
				t.Fatal("ParseDirective() returned nil")
			}

			if len(d.Packages) != 1 {
				t.Fatalf("expected 1 package, got %d", len(d.Packages))
			}

			pkg := d.Packages[0]
			if pkg.Name != tt.wantPackage {
				t.Errorf("package name = %s, want %s", pkg.Name, tt.wantPackage)
			}

			if pkg.Pattern.Raw != tt.wantPattern {
				t.Errorf("pattern = %s, want %s", pkg.Pattern.Raw, tt.wantPattern)
			}
		})
	}
}

func TestParseDirective_MultiPackage(t *testing.T) {
	line := "# doner: python:3.11.#, requests:2.#.#, flask:*"
	d := ParseDirective(line, 1)

	if d == nil {
		t.Fatal("ParseDirective() returned nil")
	}

	if len(d.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(d.Packages))
	}

	tests := []struct {
		idx         int
		wantPackage string
		wantPattern string
	}{
		{0, "python", "3.11.#"},
		{1, "requests", "2.#.#"},
		{2, "flask", "*"},
	}

	for _, tt := range tests {
		pkg := d.Packages[tt.idx]
		if pkg.Name != tt.wantPackage {
			t.Errorf("package[%d] name = %s, want %s", tt.idx, pkg.Name, tt.wantPackage)
		}
		if pkg.Pattern.Raw != tt.wantPattern {
			t.Errorf("package[%d] pattern = %s, want %s", tt.idx, pkg.Pattern.Raw, tt.wantPattern)
		}
	}
}

func TestParseDirective_Ignore(t *testing.T) {
	line := "# doner: ignore"
	d := ParseDirective(line, 1)

	if d == nil {
		t.Fatal("ParseDirective() returned nil")
	}

	if !d.Ignore {
		t.Error("expected Ignore = true")
	}

	if len(d.Packages) != 0 {
		t.Errorf("expected 0 packages for ignore directive, got %d", len(d.Packages))
	}
}

func TestParseDirective_PackageIgnore(t *testing.T) {
	line := "# doner: python:ignore"
	d := ParseDirective(line, 1)

	if d == nil {
		t.Fatal("ParseDirective() returned nil")
	}

	if len(d.Packages) != 1 {
		t.Fatalf("expected 1 package, got %d", len(d.Packages))
	}

	pkg := d.Packages[0]
	if pkg.Name != "python" {
		t.Errorf("package name = %s, want python", pkg.Name)
	}
	if !pkg.Ignore {
		t.Error("expected package.Ignore = true")
	}
}

func TestParseDirective_NotDirective(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"regular comment", "# This is a regular comment"},
		{"empty comment", "#"},
		{"no hash", "doner: python:3.11.#"},
		{"empty line", ""},
		{"instruction", "FROM python:3.11"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := ParseDirective(tt.line, 1)
			if d != nil {
				t.Errorf("expected nil for non-directive line, got %v", d)
			}
		})
	}
}

func TestParseDirective_WithWhitespace(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantPackage string
		wantPattern string
	}{
		{
			name:        "extra spaces",
			line:        "#   doner:   python:3.11.#   ",
			wantPackage: "python",
			wantPattern: "3.11.#",
		},
		{
			name:        "tabs",
			line:        "#\tdoner:\tpython:3.11.#",
			wantPackage: "python",
			wantPattern: "3.11.#",
		},
		{
			name:        "mixed whitespace",
			line:        "#  doner:  python : 3.11.#  ",
			wantPackage: "python ",
			wantPattern: " 3.11.#",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := ParseDirective(tt.line, 1)
			if d == nil {
				t.Fatal("ParseDirective() returned nil")
			}

			if len(d.Packages) != 1 {
				t.Fatalf("expected 1 package, got %d", len(d.Packages))
			}

			pkg := d.Packages[0]
			if pkg.Name != tt.wantPackage {
				t.Errorf("package name = %q, want %q", pkg.Name, tt.wantPackage)
			}
			if pkg.Pattern.Raw != tt.wantPattern {
				t.Errorf("pattern = %q, want %q", pkg.Pattern.Raw, tt.wantPattern)
			}
		})
	}
}

func TestParseDirective_LineNumber(t *testing.T) {
	line := "# doner: python:3.11.#"

	tests := []int{1, 5, 10, 100}
	for _, lineNum := range tests {
		d := ParseDirective(line, lineNum)
		if d == nil {
			t.Fatal("ParseDirective() returned nil")
		}

		if d.Line != lineNum {
			t.Errorf("Line = %d, want %d", d.Line, lineNum)
		}
	}
}

func TestParseDirective_RawContent(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantRaw string
	}{
		{
			name:    "single package",
			line:    "# doner: python:3.11.#",
			wantRaw: "python:3.11.#",
		},
		{
			name:    "multi package",
			line:    "# doner: python:3.11.#, requests:2.#.#",
			wantRaw: "python:3.11.#, requests:2.#.#",
		},
		{
			name:    "ignore",
			line:    "# doner: ignore",
			wantRaw: "ignore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := ParseDirective(tt.line, 1)
			if d == nil {
				t.Fatal("ParseDirective() returned nil")
			}

			if d.Raw != tt.wantRaw {
				t.Errorf("Raw = %q, want %q", d.Raw, tt.wantRaw)
			}
		})
	}
}

func TestParsePackageDirective_NoColon(t *testing.T) {
	pkg := parsePackageDirective("python")

	if pkg.Name != "python" {
		t.Errorf("Name = %s, want python", pkg.Name)
	}

	if pkg.Pattern.Raw != "#.#.#" {
		t.Errorf("Pattern = %s, want #.#.#", pkg.Pattern.Raw)
	}
}

func TestParsePackageDirective_WithPattern(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantPattern string
	}{
		{"python:3.11.#", "python", "3.11.#"},
		{"requests:2.#.#", "requests", "2.#.#"},
		{"flask:*", "flask", "*"},
		{"numpy:1.24.0", "numpy", "1.24.0"},
	}

	for _, tt := range tests {
		pkg := parsePackageDirective(tt.input)

		if pkg.Name != tt.wantName {
			t.Errorf("Name = %s, want %s", pkg.Name, tt.wantName)
		}

		if pkg.Pattern.Raw != tt.wantPattern {
			t.Errorf("Pattern = %s, want %s", pkg.Pattern.Raw, tt.wantPattern)
		}
	}
}

func TestParsePackageDirective_Ignore(t *testing.T) {
	pkg := parsePackageDirective("python:ignore")

	if pkg.Name != "python" {
		t.Errorf("Name = %s, want python", pkg.Name)
	}

	if !pkg.Ignore {
		t.Error("expected Ignore = true")
	}
}

func TestParseDirective_EmptyPackages(t *testing.T) {
	line := "# doner: python:3.11.#, , , requests:2.#.#"
	d := ParseDirective(line, 1)

	if d == nil {
		t.Fatal("ParseDirective() returned nil")
	}

	// Empty parts should be skipped
	if len(d.Packages) != 2 {
		t.Errorf("expected 2 packages (empty parts skipped), got %d", len(d.Packages))
	}
}

func TestParseDirective_TrailingComma(t *testing.T) {
	line := "# doner: python:3.11.#,"
	d := ParseDirective(line, 1)

	if d == nil {
		t.Fatal("ParseDirective() returned nil")
	}

	if len(d.Packages) != 1 {
		t.Errorf("expected 1 package, got %d", len(d.Packages))
	}
}
