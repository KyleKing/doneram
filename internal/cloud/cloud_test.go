package cloud

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeCLI(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "aws"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestRun(t *testing.T) {
	fakeCLI(t, "echo ' ami-1 '\n")
	if out, err := Run(context.Background(), "aws", "ssm"); err != nil || out != "ami-1" {
		t.Fatalf("Run = %q, %v", out, err)
	}

	fakeCLI(t, "echo 'Unable to locate credentials. You can configure credentials by running \"aws configure\".' >&2\nexit 255\n")
	if _, err := Run(context.Background(), "aws", "ssm"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no credentials: %v, want ErrUnavailable", err)
	}

	fakeCLI(t, "echo 'ERROR: (gcloud.container.get-server-config) The required property [project] is not currently set.' >&2\nexit 1\n")
	if _, err := Run(context.Background(), "aws", "ssm"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no project: %v, want ErrUnavailable", err)
	}

	// A real API error is a failure, not a skip.
	fakeCLI(t, "echo 'An error occurred (ParameterNotFound) when calling the GetParameter operation' >&2\nexit 254\n")
	if _, err := Run(context.Background(), "aws", "ssm"); err == nil || errors.Is(err, ErrUnavailable) {
		t.Errorf("API error: %v, want a plain failure", err)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Run(context.Background(), "aws", "ssm"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("missing CLI: %v, want ErrUnavailable", err)
	}
}
