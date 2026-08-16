package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

func TestCargoResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/api/v1/crates/ripgrep/versions": testutil.FixtureHandler("api/cargo/ripgrep.json"),
	})
	defer server.Close()

	r := NewCargoResolverWithBaseURL(&http.Client{}, server.URL)

	tests := []struct {
		name    string
		crate   string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "latest version",
			crate:   "ripgrep",
			pattern: "#.#.#",
			want:    "14.2.0",
			wantErr: false,
		},
		{
			name:    "specific major version",
			crate:   "ripgrep",
			pattern: "14.#.#",
			want:    "14.2.0",
			wantErr: false,
		},
		{
			name:    "specific minor version",
			crate:   "ripgrep",
			pattern: "14.1.#",
			want:    "14.1.0",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := parser.ParsePattern(tt.pattern)
			got, err := r.Resolve(context.Background(), tt.crate, pattern)

			if (err != nil) != tt.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("Resolve() = %v, want %v", got, tt.want)
			}
		})
	}
}
