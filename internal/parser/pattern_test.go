package parser

import (
	"testing"
)

func TestParsePattern_SemVer(t *testing.T) {
	tests := []struct {
		input      string
		wantMajor  string
		wantMinor  string
		wantPatch  string
		wantSuffix string
	}{
		{"#.#.#", "#", "#", "#", ""},
		{"3.11.#", "3", "11", "#", ""},
		{"3.#.#", "3", "#", "#", ""},
		{"#.11.#", "#", "11", "#", ""},
		{"3.11.5", "3", "11", "5", ""},
		{"#.#", "#", "#", "", ""},
		{"3.#", "3", "#", "", ""},
		{"#", "#", "", "", ""},
		{"3", "3", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := ParsePattern(tt.input)

			if p.Major != tt.wantMajor {
				t.Errorf("Major = %s, want %s", p.Major, tt.wantMajor)
			}
			if p.Minor != tt.wantMinor {
				t.Errorf("Minor = %s, want %s", p.Minor, tt.wantMinor)
			}
			if p.Patch != tt.wantPatch {
				t.Errorf("Patch = %s, want %s", p.Patch, tt.wantPatch)
			}
			if p.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %s, want %s", p.Suffix, tt.wantSuffix)
			}
			if p.Raw != tt.input {
				t.Errorf("Raw = %s, want %s", p.Raw, tt.input)
			}
		})
	}
}

func TestParsePattern_WithSuffix(t *testing.T) {
	tests := []struct {
		input      string
		wantMajor  string
		wantMinor  string
		wantPatch  string
		wantSuffix string
	}{
		{"#.#.#-alpha", "#", "#", "#", "alpha"},
		{"3.11.#-rc1", "3", "11", "#", "rc1"},
		{"#.#.#-beta*", "#", "#", "#", "beta*"},
		{"3.11.5-r0", "3", "11", "5", "r0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p := ParsePattern(tt.input)

			if p.Major != tt.wantMajor {
				t.Errorf("Major = %s, want %s", p.Major, tt.wantMajor)
			}
			if p.Minor != tt.wantMinor {
				t.Errorf("Minor = %s, want %s", p.Minor, tt.wantMinor)
			}
			if p.Patch != tt.wantPatch {
				t.Errorf("Patch = %s, want %s", p.Patch, tt.wantPatch)
			}
			if p.Suffix != tt.wantSuffix {
				t.Errorf("Suffix = %s, want %s", p.Suffix, tt.wantSuffix)
			}
		})
	}
}

func TestVersionPattern_String(t *testing.T) {
	tests := []string{
		"#.#.#",
		"3.11.#",
		"#.#.#-alpha",
		"*",
	}

	for _, input := range tests {
		p := ParsePattern(input)
		if p.String() != input {
			t.Errorf("String() = %s, want %s", p.String(), input)
		}
	}
}

func TestVersionPattern_Matches_ExactVersion(t *testing.T) {
	p := ParsePattern("3.11.5")

	tests := []struct {
		version string
		want    bool
	}{
		{"3.11.5", true},
		{"3.11.4", false},
		{"3.11.6", false},
		{"3.10.5", false},
		{"2.11.5", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_WildcardPatch(t *testing.T) {
	p := ParsePattern("3.11.#")

	tests := []struct {
		version string
		want    bool
	}{
		{"3.11.0", true},
		{"3.11.1", true},
		{"3.11.99", true},
		{"3.10.5", false},
		{"3.12.0", false},
		{"2.11.5", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_WildcardMinor(t *testing.T) {
	p := ParsePattern("3.#.#")

	tests := []struct {
		version string
		want    bool
	}{
		{"3.0.0", true},
		{"3.11.5", true},
		{"3.99.99", true},
		{"2.11.5", false},
		{"4.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_WildcardMajor(t *testing.T) {
	p := ParsePattern("#.#.#")

	tests := []struct {
		version string
		want    bool
	}{
		{"0.0.0", true},
		{"1.2.3", true},
		{"99.99.99", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_WithSuffix(t *testing.T) {
	tests := []struct {
		pattern string
		version string
		want    bool
	}{
		{"3.11.#-rc1", "3.11.5-rc1", true},
		{"3.11.#-rc1", "3.11.5-rc2", false},
		{"3.11.#-rc1", "3.11.5", false},
		{"#.#.#-alpha", "1.2.3-alpha", true},
		{"#.#.#-alpha", "1.2.3-beta", false},
		{"#.#.#-alpha", "1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.version, func(t *testing.T) {
			p := ParsePattern(tt.pattern)
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("pattern %s matches %s = %v, want %v", tt.pattern, tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_WildcardSuffix(t *testing.T) {
	p := ParsePattern("#.#.#-*")

	tests := []struct {
		version string
		want    bool
	}{
		{"1.2.3-alpha", true},
		{"1.2.3-beta", true},
		{"1.2.3-rc1", true},
		{"1.2.3", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_PrefixSuffix(t *testing.T) {
	p := ParsePattern("#.#.#-rc*")

	tests := []struct {
		version string
		want    bool
	}{
		{"1.2.3-rc1", true},
		{"1.2.3-rc2", true},
		{"1.2.3-rc", true},
		{"1.2.3-beta", false},
		{"1.2.3-alpha", false},
		{"1.2.3", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_NoSuffix(t *testing.T) {
	p := ParsePattern("3.11.#")

	tests := []struct {
		version string
		want    bool
	}{
		{"3.11.5", true},
		{"3.11.5-rc1", false},
		{"3.11.5-alpha", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_Matches_PartialVersions(t *testing.T) {
	tests := []struct {
		pattern string
		version string
		want    bool
	}{
		{"#.#", "3.11", true},
		{"#.#", "3.11.5", true},
		{"3.#", "3.11", true},
		{"3.#", "3.11.5", true},
		{"#", "3", true},
		{"#", "3.11", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.version, func(t *testing.T) {
			p := ParsePattern(tt.pattern)
			got := p.Matches(tt.version)
			if got != tt.want {
				t.Errorf("pattern %s matches %s = %v, want %v", tt.pattern, tt.version, got, tt.want)
			}
		})
	}
}

func TestVersionPattern_ToRegex(t *testing.T) {
	tests := []struct {
		pattern     string
		wantContain string
	}{
		{"#.#.#", `^\d+\.\d+\.\d+$`},
		{"3.#.#", `^3\.\d+\.\d+$`},
		{"3.11.#", `^3\.11\.\d+$`},
		{"*", `^.*$`},
		{"#.#.#-alpha", `^\d+\.\d+\.\d+-alpha$`},
		{"#.#.#-*", `^\d+\.\d+\.\d+-.*$`},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			p := ParsePattern(tt.pattern)
			got := p.ToRegex()
			if got != tt.wantContain {
				t.Errorf("ToRegex() = %s, want %s", got, tt.wantContain)
			}
		})
	}
}

func TestMatchSegment(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"", "", true},
		{"", "3", true},
		{"#", "", true},
		{"#", "3", true},
		{"#", "11", true},
		{"3", "3", true},
		{"3", "4", false},
		{"11", "11", true},
		{"11", "12", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.value, func(t *testing.T) {
			got := matchSegment(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("matchSegment(%s, %s) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestMatchSuffix(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"*", "", true},
		{"*", "alpha", true},
		{"*", "rc1", true},
		{"alpha", "alpha", true},
		{"alpha", "beta", false},
		{"rc*", "rc1", true},
		{"rc*", "rc2", true},
		{"rc*", "rc", true},
		{"rc*", "beta", false},
		{"alpha*", "alpha1", true},
		{"alpha*", "beta1", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.value, func(t *testing.T) {
			got := matchSuffix(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("matchSuffix(%s, %s) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestGetIndex(t *testing.T) {
	slice := []string{"a", "b", "c"}

	tests := []struct {
		idx  int
		want string
	}{
		{0, "a"},
		{1, "b"},
		{2, "c"},
		{3, ""},
		{4, ""},
	}

	for _, tt := range tests {
		got := getIndex(slice, tt.idx)
		if got != tt.want {
			t.Errorf("getIndex(slice, %d) = %s, want %s", tt.idx, got, tt.want)
		}
	}
}

func TestGetIndex_EmptySlice(t *testing.T) {
	slice := []string{}
	got := getIndex(slice, 0)
	if got != "" {
		t.Errorf("getIndex(empty, 0) = %s, want empty", got)
	}
}
