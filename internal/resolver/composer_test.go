package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

func TestComposerResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/p2/symfony/console.json": testutil.FixtureHandler("api/packagist/symfony-console.json"),
	})
	defer server.Close()

	r := NewComposerResolverWithBaseURL(&http.Client{}, server.URL)

	tests := []struct {
		name    string
		pkg     string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "latest version",
			pkg:     "symfony/console",
			pattern: "#.#.#",
			want:    "6.4.1",
			wantErr: false,
		},
		{
			name:    "specific major version",
			pkg:     "symfony/console",
			pattern: "6.#.#",
			want:    "6.4.1",
			wantErr: false,
		},
		{
			name:    "specific minor version",
			pkg:     "symfony/console",
			pattern: "6.4.#",
			want:    "6.4.1",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := parser.ParsePattern(tt.pattern)
			got, err := r.Resolve(context.Background(), tt.pkg, pattern)

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
