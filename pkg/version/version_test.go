package version

import (
	"testing"
)

func TestParse_SemVer(t *testing.T) {
	tests := []struct {
		input       string
		wantMajor   int
		wantMinor   int
		wantPatch   int
		wantSuffix  string
	}{
		{"1.2.3", 1, 2, 3, ""},
		{"0.0.0", 0, 0, 0, ""},
		{"10.20.30", 10, 20, 30, ""},
		{"1.0.0", 1, 0, 0, ""},
		{"0.1.0", 0, 1, 0, ""},
		{"0.0.1", 0, 0, 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v := Parse(tt.input)

			if v.Major != tt.wantMajor {
				t.Errorf("Major = %d, want %d", v.Major, tt.wantMajor)
			}
			if v.Minor != tt.wantMinor {
				t.Errorf("Minor = %d, want %d", v.Minor, tt.wantMinor)
			}
			if v.Patch != tt.wantPatch {
				t.Errorf("Patch = %d, want %d", v.Patch, tt.wantPatch)
			}
			if v.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %s, want %s", v.Suffix, tt.wantSuffix)
			}
			if v.Raw != tt.input {
				t.Errorf("Raw = %s, want %s", v.Raw, tt.input)
			}
		})
	}
}

func TestParse_WithSuffix(t *testing.T) {
	tests := []struct {
		input       string
		wantMajor   int
		wantMinor   int
		wantPatch   int
		wantSuffix  string
	}{
		{"1.2.3-alpha", 1, 2, 3, "alpha"},
		{"1.2.3-beta", 1, 2, 3, "beta"},
		{"1.2.3-rc1", 1, 2, 3, "rc1"},
		{"1.2.3-alpha.1", 1, 2, 3, "alpha.1"},
		{"1.2.3-r0", 1, 2, 3, "r0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v := Parse(tt.input)

			if v.Major != tt.wantMajor {
				t.Errorf("Major = %d, want %d", v.Major, tt.wantMajor)
			}
			if v.Minor != tt.wantMinor {
				t.Errorf("Minor = %d, want %d", v.Minor, tt.wantMinor)
			}
			if v.Patch != tt.wantPatch {
				t.Errorf("Patch = %d, want %d", v.Patch, tt.wantPatch)
			}
			if v.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %s, want %s", v.Suffix, tt.wantSuffix)
			}
		})
	}
}

func TestParse_PartialVersions(t *testing.T) {
	tests := []struct {
		input       string
		wantMajor   int
		wantMinor   int
		wantPatch   int
	}{
		{"1.2", 1, 2, 0},
		{"1", 1, 0, 0},
		{"10.20", 10, 20, 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v := Parse(tt.input)

			if v.Major != tt.wantMajor {
				t.Errorf("Major = %d, want %d", v.Major, tt.wantMajor)
			}
			if v.Minor != tt.wantMinor {
				t.Errorf("Minor = %d, want %d", v.Minor, tt.wantMinor)
			}
			if v.Patch != tt.wantPatch {
				t.Errorf("Patch = %d, want %d", v.Patch, tt.wantPatch)
			}
		})
	}
}

func TestParse_InvalidVersions(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"non-numeric", "abc"},
		{"mixed", "1.2.abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Parse(tt.input)
			if v == nil {
				t.Error("Parse() returned nil")
			}
			if v.Raw != tt.input {
				t.Errorf("Raw = %s, want %s", v.Raw, tt.input)
			}
		})
	}
}

func TestCompare_Major(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.0.0", "1.0.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := Compare(Parse(tt.a), Parse(tt.b))
			if (result > 0 && tt.want <= 0) || (result < 0 && tt.want >= 0) || (result == 0 && tt.want != 0) {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, result, tt.want)
			}
		})
	}
}

func TestCompare_Minor(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"1.2.0", "1.1.0", 1},
		{"1.1.0", "1.2.0", -1},
		{"1.1.0", "1.1.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := Compare(Parse(tt.a), Parse(tt.b))
			if (result > 0 && tt.want <= 0) || (result < 0 && tt.want >= 0) || (result == 0 && tt.want != 0) {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, result, tt.want)
			}
		})
	}
}

func TestCompare_Patch(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"1.1.2", "1.1.1", 1},
		{"1.1.1", "1.1.2", -1},
		{"1.1.1", "1.1.1", 0},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := Compare(Parse(tt.a), Parse(tt.b))
			if (result > 0 && tt.want <= 0) || (result < 0 && tt.want >= 0) || (result == 0 && tt.want != 0) {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, result, tt.want)
			}
		})
	}
}

func TestCompare_Suffix(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"1.0.0-beta", "1.0.0-alpha", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-alpha", "1.0.0-alpha", 0},
		{"1.0.0", "1.0.0-alpha", -1},
		{"1.0.0-alpha", "1.0.0", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := Compare(Parse(tt.a), Parse(tt.b))
			if (result > 0 && tt.want <= 0) || (result < 0 && tt.want >= 0) || (result == 0 && tt.want != 0) {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.a, tt.b, result, tt.want)
			}
		})
	}
}

func TestLatest_SingleVersion(t *testing.T) {
	versions := []string{"1.0.0"}
	result := Latest(versions)
	if result != "1.0.0" {
		t.Errorf("Latest() = %s, want 1.0.0", result)
	}
}

func TestLatest_MultipleVersions(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{
			name:     "ascending order",
			versions: []string{"1.0.0", "1.1.0", "1.2.0"},
			want:     "1.2.0",
		},
		{
			name:     "descending order",
			versions: []string{"1.2.0", "1.1.0", "1.0.0"},
			want:     "1.2.0",
		},
		{
			name:     "random order",
			versions: []string{"1.1.0", "1.2.0", "1.0.0"},
			want:     "1.2.0",
		},
		{
			name:     "with major differences",
			versions: []string{"1.0.0", "2.0.0", "3.0.0"},
			want:     "3.0.0",
		},
		{
			name:     "with minor differences",
			versions: []string{"1.0.0", "1.1.0", "1.2.0"},
			want:     "1.2.0",
		},
		{
			name:     "with patch differences",
			versions: []string{"1.0.0", "1.0.1", "1.0.2"},
			want:     "1.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Latest(tt.versions)
			if result != tt.want {
				t.Errorf("Latest() = %s, want %s", result, tt.want)
			}
		})
	}
}

func TestLatest_WithSuffix(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{
			name:     "all with suffix",
			versions: []string{"1.0.0-alpha", "1.0.0-beta", "1.0.0-rc"},
			want:     "1.0.0-rc",
		},
		{
			name:     "mixed with and without suffix",
			versions: []string{"1.0.0-alpha", "1.0.0", "1.0.0-beta"},
			want:     "1.0.0-beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Latest(tt.versions)
			if result != tt.want {
				t.Errorf("Latest() = %s, want %s", result, tt.want)
			}
		})
	}
}

func TestLatest_EmptyList(t *testing.T) {
	versions := []string{}
	result := Latest(versions)
	if result != "" {
		t.Errorf("Latest() = %s, want empty string", result)
	}
}

func TestLatest_MixedFormats(t *testing.T) {
	versions := []string{"1.0", "1.0.0", "1.0.1"}
	result := Latest(versions)
	if result != "1.0.1" {
		t.Errorf("Latest() = %s, want 1.0.1", result)
	}
}
