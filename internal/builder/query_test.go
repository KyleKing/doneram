package builder

import (
	"context"
	"testing"
)

func TestDockerQuerier_QueryPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	q := NewDockerQuerier(false)
	ctx := context.Background()

	tests := []struct {
		name     string
		imageID  string
		queryCmd string
		wantErr  bool
	}{
		{
			name:     "query alpine packages",
			imageID:  "alpine:3.19",
			queryCmd: "apk list --installed | head -5",
			wantErr:  false,
		},
		{
			name:     "query echo",
			imageID:  "alpine:3.19",
			queryCmd: "echo hello",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := q.QueryPackages(ctx, tt.imageID, tt.queryCmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("QueryPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && output == "" {
				t.Error("QueryPackages() returned empty output")
			}
			if !tt.wantErr {
				t.Logf("Query output:\n%s", output)
			}
		})
	}
}
