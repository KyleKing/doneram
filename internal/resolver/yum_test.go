package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/internal/testutil"
)

func TestYumResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/api/v1/project/curl": testutil.FixtureHandler("api/yum/curl.json"),
	})
	defer server.Close()

	r := NewYumResolverWithBaseURL(&http.Client{}, server.URL)

	tests := []struct {
		name    string
		pkg     string
		pattern string
		want    string
		wantErr bool
	}{
		{
			name:    "latest version",
			pkg:     "curl",
			pattern: "#.#.#",
			want:    "8.2.0",
			wantErr: false,
		},
		{
			name:    "specific major version",
			pkg:     "curl",
			pattern: "8.#.#",
			want:    "8.2.0",
			wantErr: false,
		},
		{
			name:    "specific minor version",
			pkg:     "curl",
			pattern: "7.#.#",
			want:    "7.76.1",
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
