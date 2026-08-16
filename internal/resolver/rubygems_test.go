package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

func TestRubyGemsResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/api/v1/versions/rails.json": testutil.FixtureHandler("api/rubygems/rails.json"),
	})
	defer server.Close()

	r := NewRubyGemsResolverWithBaseURL(&http.Client{}, server.URL)

	tests := []struct {
		name    string
		gem     string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "latest version",
			gem:     "rails",
			pattern: "#.#.#",
			want:    "7.1.3",
			wantErr: false,
		},
		{
			name:    "specific major version",
			gem:     "rails",
			pattern: "7.#.#",
			want:    "7.1.3",
			wantErr: false,
		},
		{
			name:    "specific minor version",
			gem:     "rails",
			pattern: "7.0.#",
			want:    "7.0.2",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := parser.ParsePattern(tt.pattern)
			got, err := r.Resolve(context.Background(), tt.gem, pattern)

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
