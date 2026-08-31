package resolver

import (
	"context"
	"reflect"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
)

func TestParseLsRemote(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name:   "typical output",
			output: "1.7.0\n1.7.1\n1.8.0\n",
			want:   []string{"1.7.0", "1.7.1", "1.8.0"},
		},
		{
			name:   "blank lines skipped",
			output: "1.7.0\n\n1.7.1\n\n",
			want:   []string{"1.7.0", "1.7.1"},
		},
		{
			name:   "trailing whitespace trimmed",
			output: "1.7.0 \n 1.7.1\n",
			want:   []string{"1.7.0", "1.7.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLsRemote(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseLsRemote() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMiseResolver_Name(t *testing.T) {
	r := NewMiseResolver()
	if r.Name() != "mise" {
		t.Errorf("Name() = %s, want mise", r.Name())
	}
}

func TestMiseResolver_Resolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test that shells out to mise")
	}

	r := NewMiseResolver()
	ctx := context.Background()

	t.Run("simple case uses mise latest", func(t *testing.T) {
		got, err := r.Resolve(ctx, "jq", parser.ParsePattern("#.#.#"))
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if got == "" {
			t.Error("Resolve() returned empty version")
		}
	})

	t.Run("held ceiling falls back to ls-remote", func(t *testing.T) {
		pattern := parser.ParsePattern("#.#.#")
		pattern.Ceiling = "1.7.0"
		got, err := r.Resolve(ctx, "jq", pattern)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}
		if !pattern.Matches(got) {
			t.Errorf("Resolve() = %s, want a version below the 1.0.0 ceiling", got)
		}
	})
}
