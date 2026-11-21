package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/cloud"
	"github.com/kyleking/doneram/internal/parser"
)

// recorder answers every CLI call with out and keeps the command line.
type recorder struct {
	out   string
	err   error
	calls []string
}

func (r *recorder) run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return r.out, r.err
}

func TestAWSSSM(t *testing.T) {
	rec := &recorder{out: "ami-0123456789abcdef0"}
	r := &AWSSSMResolver{Run: rec.run}
	got, err := r.Resolve(context.Background(), "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64@eu-west-1", nil)
	if err != nil || got != "ami-0123456789abcdef0" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	want := "aws ssm get-parameter --name /aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 --query Parameter.Value --output text --region eu-west-1"
	if rec.calls[0] != want {
		t.Errorf("call = %s", rec.calls[0])
	}
	rec.out = "None"
	if _, err := r.Resolve(context.Background(), "/missing", nil); err == nil {
		t.Error("an empty parameter resolved")
	}
}

func TestAWSAMI(t *testing.T) {
	rec := &recorder{out: "ami-0fedcba9876543210"}
	r := &AWSAMIResolver{Run: rec.run}
	got, err := r.Resolve(context.Background(), "amazon/al2023-ami-2023.*-x86_64", nil)
	if err != nil || got != "ami-0fedcba9876543210" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if !strings.Contains(rec.calls[0], "--owners amazon --filters Name=name,Values=al2023-ami-2023.*-x86_64 Name=state,Values=available") || strings.Contains(rec.calls[0], "--region") {
		t.Errorf("call = %s", rec.calls[0])
	}
	rec.out = "None"
	if _, err := r.Resolve(context.Background(), "self/my-build-*", nil); err == nil {
		t.Error("no match resolved")
	}
	if _, err := r.Resolve(context.Background(), "no-pattern", nil); err == nil {
		t.Error("a name without owner resolved")
	}
}

func TestGKE(t *testing.T) {
	rec := &recorder{out: `{"validMasterVersions":["1.34.1-gke.100","1.33.5-gke.200"],
"channels":[{"channel":"REGULAR","validVersions":["1.33.5-gke.1080000","1.33.4-gke.900","1.32.9-gke.1"]},
{"channel":"RAPID","validVersions":["1.34.1-gke.100"]}]}`}
	r := &GKEResolver{Run: rec.run}
	cases := map[string]string{
		"us-central1/REGULAR": "1.33.5-gke.1080000",
		"us-central1/rapid":   "1.34.1-gke.100",
		"us-central1":         "1.34.1-gke.100",
	}
	for name, want := range cases {
		if got, err := r.Resolve(context.Background(), name, parser.ParsePattern("#.#.#")); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %s", name, got, err, want)
		}
	}
	if got, _ := r.Resolve(context.Background(), "us-central1/REGULAR", parser.ParsePattern("1.32.#")); got != "1.32.9-gke.1" {
		t.Errorf("1.32.# = %q", got)
	}
	if rec.calls[0] != "gcloud container get-server-config --location us-central1 --format json" {
		t.Errorf("call = %s", rec.calls[0])
	}
	if _, err := r.Resolve(context.Background(), "europe-west1/STABLE@my-proj", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("an empty channel resolved")
	}
	if last := rec.calls[len(rec.calls)-1]; !strings.HasSuffix(last, "--location europe-west1 --format json --project my-proj") {
		t.Errorf("call = %s", last)
	}
}

func TestCloudRunMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := cloud.Run(context.Background(), "aws", "sts", "get-caller-identity")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
