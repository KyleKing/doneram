package updater

import (
	"testing"
)

func TestUpdater_Apply(t *testing.T) {
	tests := []struct {
		name    string
		content string
		updates []Update
		want    string
		wantErr bool
	}{
		{
			name: "update single FROM instruction",
			content: `# doner: python:3.13.#
FROM python:3.13.0

COPY . .`,
			updates: []Update{
				{
					Package:    "python",
					Source:     "docker",
					OldVersion: "3.13.0",
					NewVersion: "3.13.11",
					Line:       2,
				},
			},
			want: `# doner: python:3.13.#
FROM python:3.13.11

COPY . .`,
		},
		{
			name: "update multiple instructions",
			content: `# doner: python:3.13.#
FROM python:3.13.0 AS builder

# doner: python:3.13.#
FROM python:3.13.0`,
			updates: []Update{
				{
					Package:    "python",
					Source:     "docker",
					OldVersion: "3.13.0",
					NewVersion: "3.13.11",
					Line:       2,
				},
				{
					Package:    "python",
					Source:     "docker",
					OldVersion: "3.13.0",
					NewVersion: "3.13.11",
					Line:       5,
				},
			},
			want: `# doner: python:3.13.#
FROM python:3.13.11 AS builder

# doner: python:3.13.#
FROM python:3.13.11`,
		},
		{
			name: "update COPY --from instruction",
			content: `# doner: ignore
COPY --from=ghcr.io/astral-sh/uv:0.9.24 /uv /bin/`,
			updates: []Update{
				{
					Package:    "ghcr.io/astral-sh/uv",
					Source:     "ghcr",
					OldVersion: "0.9.24",
					NewVersion: "0.9.30",
					Line:       2,
				},
			},
			want: `# doner: ignore
COPY --from=ghcr.io/astral-sh/uv:0.9.30 /uv /bin/`,
		},
		{
			name: "update with suffix",
			content: `# doner: golang:1.#.#-alpine*
FROM golang:1.22-alpine3.19 as builder`,
			updates: []Update{
				{
					Package:    "golang",
					Source:     "docker",
					OldVersion: "1.22-alpine3.19",
					NewVersion: "1.23.5-alpine3.21",
					Line:       2,
				},
			},
			want: `# doner: golang:1.#.#-alpine*
FROM golang:1.23.5-alpine3.21 as builder`,
		},
		{
			name:    "error on line out of range",
			content: `FROM python:3.13.0`,
			updates: []Update{
				{Line: 10, OldVersion: "3.13.0", NewVersion: "3.13.11"},
			},
			wantErr: true,
		},
		{
			name:    "error when version not found",
			content: `FROM python:3.13.0`,
			updates: []Update{
				{Line: 1, OldVersion: "3.12.0", NewVersion: "3.12.5"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := NewUpdater(tt.content)
			err := u.Apply(tt.updates)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Apply() error = %v, wantErr = %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				got := u.Content()
				if got != tt.want {
					t.Errorf("Content() mismatch:\ngot:\n%s\nwant:\n%s", got, tt.want)
				}
			}
		})
	}
}

func TestParseFromInstruction(t *testing.T) {
	tests := []struct {
		name        string
		args        string
		wantImage   string
		wantVersion string
	}{
		{
			name:        "simple image",
			args:        "python:3.13.0",
			wantImage:   "python",
			wantVersion: "3.13.0",
		},
		{
			name:        "with AS alias uppercase",
			args:        "python:3.13.0 AS builder",
			wantImage:   "python",
			wantVersion: "3.13.0",
		},
		{
			name:        "with AS alias lowercase",
			args:        "golang:1.22-alpine3.19 as builder",
			wantImage:   "golang",
			wantVersion: "1.22-alpine3.19",
		},
		{
			name:        "full registry path",
			args:        "public.ecr.aws/lambda/python:3.13",
			wantImage:   "public.ecr.aws/lambda/python",
			wantVersion: "3.13",
		},
		{
			name:        "with extra whitespace",
			args:        "  python:3.13.0  ",
			wantImage:   "python",
			wantVersion: "3.13.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotImage, gotVersion := parseFromInstruction(tt.args)
			if gotImage != tt.wantImage {
				t.Errorf("image = %q, want %q", gotImage, tt.wantImage)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, tt.wantVersion)
			}
		})
	}
}

func TestParseCopyFromInstruction(t *testing.T) {
	tests := []struct {
		name        string
		args        string
		wantImage   string
		wantVersion string
	}{
		{
			name:        "COPY --from with image",
			args:        "--from=ghcr.io/astral-sh/uv:0.9.24 /uv /bin/",
			wantImage:   "ghcr.io/astral-sh/uv",
			wantVersion: "0.9.24",
		},
		{
			name:        "COPY --from with simple image",
			args:        "--from=python:3.13.0 /app /app",
			wantImage:   "python",
			wantVersion: "3.13.0",
		},
		{
			name:        "invalid format",
			args:        "some-file /dest",
			wantImage:   "",
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotImage, gotVersion := parseCopyFromInstruction(tt.args)
			if gotImage != tt.wantImage {
				t.Errorf("image = %q, want %q", gotImage, tt.wantImage)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("version = %q, want %q", gotVersion, tt.wantVersion)
			}
		})
	}
}
