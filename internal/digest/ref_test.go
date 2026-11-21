package digest

import (
	"strings"
	"testing"
)

const sampleDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestParse(t *testing.T) {
	tests := []struct {
		in         string
		registry   string
		repository string
		tag        string
		digest     string
		name       string
	}{
		{"python:3.13-slim", dockerHub, "library/python", "3.13-slim", "", "python"},
		{"user/img:1", dockerHub, "user/img", "1", "", "user/img"},
		{"ghcr.io/o/r:tag", "ghcr.io", "o/r", "tag", "", "ghcr.io/o/r"},
		{"registry:5000/x/y:1", "registry:5000", "x/y", "1", "", "registry:5000/x/y"},
		{"localhost/x:2", "localhost", "x", "2", "", "localhost/x"},
		{"img", dockerHub, "library/img", "latest", "", "img"},
		{"img:tag@" + sampleDigest, dockerHub, "library/img", "tag", sampleDigest, "img"},
		{"img@" + sampleDigest, dockerHub, "library/img", "", sampleDigest, "img"},
		{"docker.io/library/python:3", dockerHub, "library/python", "3", "", "docker.io/library/python"},
		{"docker.io/python:3", dockerHub, "library/python", "3", "", "docker.io/python"},
		{"index.docker.io/user/img", dockerHub, "user/img", "latest", "", "index.docker.io/user/img"},
		{"quay.io/a/b/c:v1.2", "quay.io", "a/b/c", "v1.2", "", "quay.io/a/b/c"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ref, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if ref.Registry != tt.registry || ref.Repository != tt.repository ||
				ref.Tag != tt.tag || ref.Digest != tt.digest || ref.Name != tt.name {
				t.Errorf("Parse(%q) = %+v", tt.in, ref)
			}
			if got := ref.String(); got != tt.in {
				t.Errorf("String() = %q, want %q", got, tt.in)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{
		"",
		"Python:3",
		"img:bad tag",
		"img@sha256:short",
		"${BASE_IMAGE}",
		"img:",
	} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", in)
		} else if in != "" && !strings.Contains(err.Error(), in) {
			t.Errorf("Parse(%q) error %q does not name the image", in, err)
		}
	}
}

func TestStringKeepsWrittenForm(t *testing.T) {
	ref, err := Parse("python:3.13-slim")
	if err != nil {
		t.Fatal(err)
	}
	ref.Digest = sampleDigest
	if got, want := ref.String(), "python:3.13-slim@"+sampleDigest; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	ref, err = Parse("img")
	if err != nil {
		t.Fatal(err)
	}
	ref.Digest = sampleDigest
	if got, want := ref.String(), "img@"+sampleDigest; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStringWithoutName(t *testing.T) {
	tests := []struct {
		ref  Ref
		want string
	}{
		{Ref{Registry: dockerHub, Repository: "library/python", Tag: "3"}, "python:3"},
		{Ref{Registry: dockerHub, Repository: "user/img", Tag: "1"}, "user/img:1"},
		{Ref{Registry: "ghcr.io", Repository: "o/r", Tag: "latest"}, "ghcr.io/o/r:latest"},
	}
	for _, tt := range tests {
		if got := tt.ref.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}
