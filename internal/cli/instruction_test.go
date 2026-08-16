package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

type fakeResolver struct {
	name    string
	version string
	err     error
	calls   int
}

func (f *fakeResolver) Name() string { return f.name }

func (f *fakeResolver) Resolve(_ context.Context, _ string, _ *parser.VersionPattern) (string, error) {
	f.calls++
	return f.version, f.err
}

func (f *fakeResolver) GetChangelog(_ context.Context, _ string, _, _ string) (string, error) {
	return "", nil
}

func newResolvers() (*fakeResolver, *fakeResolver) {
	return &fakeResolver{name: "dockerhub", version: "3.20"}, &fakeResolver{name: "ghcr", version: "v2"}
}

func directiveFor(pkg string) *parser.Directive {
	return &parser.Directive{
		Packages: []parser.PackageDirective{{Name: pkg, Pattern: parser.ParsePattern("3.x")}},
	}
}

func ignoredDirective() *parser.Directive {
	return &parser.Directive{Packages: []parser.PackageDirective{{Name: "alpine", Ignore: true}}}
}

func TestCheckFromInstructionUsesDockerHub(t *testing.T) {
	hub, ghcr := newResolvers()
	instr := parser.Instruction{Command: "FROM", Args: "alpine:3.19"}

	got, err := checkFromInstruction(context.Background(), instr, directiveFor("alpine"), hub, ghcr)
	if err != nil {
		t.Fatalf("checkFromInstruction: %v", err)
	}
	if got != "3.20" {
		t.Errorf("got %q, want 3.20", got)
	}
	if hub.calls != 1 || ghcr.calls != 0 {
		t.Errorf("resolver calls: hub=%d ghcr=%d, want hub=1 ghcr=0", hub.calls, ghcr.calls)
	}
}

func TestCheckFromInstructionUsesGHCR(t *testing.T) {
	hub, ghcr := newResolvers()
	instr := parser.Instruction{Command: "FROM", Args: "ghcr.io/kyleking/doneram:v1"}

	got, err := checkFromInstruction(context.Background(), instr, directiveFor("doneram"), hub, ghcr)
	if err != nil {
		t.Fatalf("checkFromInstruction: %v", err)
	}
	if got != "v2" {
		t.Errorf("got %q, want v2", got)
	}
	if ghcr.calls != 1 || hub.calls != 0 {
		t.Errorf("resolver calls: hub=%d ghcr=%d, want hub=0 ghcr=1", hub.calls, ghcr.calls)
	}
}

func TestCheckFromInstructionErrors(t *testing.T) {
	hub, ghcr := newResolvers()

	if _, err := checkFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine"}, directiveFor("alpine"), hub, ghcr); err == nil {
		t.Error("an untagged FROM should be rejected")
	}

	if _, err := checkFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine:3.19"}, &parser.Directive{}, hub, ghcr); err == nil {
		t.Error("a directive without packages should be rejected")
	}

	failing := &fakeResolver{name: "dockerhub", err: errors.New("boom")}
	if _, err := checkFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine:3.19"}, directiveFor("alpine"), failing, ghcr); err == nil {
		t.Error("a resolver error should propagate")
	}
}

func TestCheckFromInstructionIgnoredPackage(t *testing.T) {
	hub, ghcr := newResolvers()

	got, err := checkFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine:3.19"}, ignoredDirective(), hub, ghcr)
	if err != nil {
		t.Fatalf("checkFromInstruction: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty result for an ignored package", got)
	}
	if hub.calls != 0 {
		t.Error("an ignored package should not reach the resolver")
	}
}

func TestCheckCopyFromInstruction(t *testing.T) {
	hub, ghcr := newResolvers()

	got, err := checkCopyFromInstruction(context.Background(),
		parser.Instruction{Command: "COPY", Args: "--from=alpine:3.19 /bin/sh /bin/sh"},
		directiveFor("alpine"), hub, ghcr)
	if err != nil {
		t.Fatalf("checkCopyFromInstruction: %v", err)
	}
	if got != "3.20" {
		t.Errorf("got %q, want 3.20", got)
	}
}

func TestCheckCopyFromInstructionErrors(t *testing.T) {
	hub, ghcr := newResolvers()

	if _, err := checkCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "/a /b"}, directiveFor("alpine"), hub, ghcr); err == nil {
		t.Error("a COPY without --from should be rejected")
	}

	if _, err := checkCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=builder /a /b"}, directiveFor("alpine"), hub, ghcr); err == nil {
		t.Error("a stage alias without a tag should be rejected")
	}

	if _, err := checkCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=alpine:3.19 /a"}, &parser.Directive{}, hub, ghcr); err == nil {
		t.Error("a directive without packages should be rejected")
	}
}

func TestCheckCopyFromInstructionIgnoredPackage(t *testing.T) {
	hub, ghcr := newResolvers()

	got, err := checkCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=alpine:3.19 /a"}, ignoredDirective(), hub, ghcr)
	if err != nil {
		t.Fatalf("checkCopyFromInstruction: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty result for an ignored package", got)
	}
}

func TestResolveFromInstruction(t *testing.T) {
	hub, ghcr := newResolvers()

	got, err := resolveFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine:3.19 AS base"}, directiveFor("alpine"), hub, ghcr)
	if err != nil {
		t.Fatalf("resolveFromInstruction: %v", err)
	}
	if got != "3.20" {
		t.Errorf("got %q, want 3.20", got)
	}

	if _, err := resolveFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine"}, directiveFor("alpine"), hub, ghcr); err == nil {
		t.Error("an untagged FROM should be rejected")
	}

	if _, err := resolveFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine:3.19"}, &parser.Directive{}, hub, ghcr); err == nil {
		t.Error("a directive without packages should be rejected")
	}

	ignored, err := resolveFromInstruction(context.Background(),
		parser.Instruction{Args: "alpine:3.19"}, ignoredDirective(), hub, ghcr)
	if err != nil || ignored != "" {
		t.Errorf("ignored package: got (%q, %v), want (\"\", nil)", ignored, err)
	}
}

func TestResolveFromInstructionUsesGHCR(t *testing.T) {
	hub, ghcr := newResolvers()

	got, err := resolveFromInstruction(context.Background(),
		parser.Instruction{Args: "ghcr.io/kyleking/doneram:v1"}, directiveFor("doneram"), hub, ghcr)
	if err != nil {
		t.Fatalf("resolveFromInstruction: %v", err)
	}
	if got != "v2" {
		t.Errorf("got %q, want v2", got)
	}
}

func TestResolveCopyFromInstruction(t *testing.T) {
	hub, ghcr := newResolvers()

	got, err := resolveCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=ghcr.io/kyleking/doneram:v1 /a /b"}, directiveFor("doneram"), hub, ghcr)
	if err != nil {
		t.Fatalf("resolveCopyFromInstruction: %v", err)
	}
	if got != "v2" {
		t.Errorf("got %q, want v2", got)
	}

	stage, err := resolveCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=builder /a /b"}, directiveFor("alpine"), hub, ghcr)
	if err != nil || stage != "" {
		t.Errorf("stage alias: got (%q, %v), want (\"\", nil)", stage, err)
	}

	if _, err := resolveCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=alpine:3.19 /a"}, &parser.Directive{}, hub, ghcr); err == nil {
		t.Error("a directive without packages should be rejected")
	}

	ignored, err := resolveCopyFromInstruction(context.Background(),
		parser.Instruction{Args: "--from=alpine:3.19 /a"}, ignoredDirective(), hub, ghcr)
	if err != nil || ignored != "" {
		t.Errorf("ignored package: got (%q, %v), want (\"\", nil)", ignored, err)
	}
}

var _ resolver.Resolver = (*fakeResolver)(nil)
