package builder

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/kyleking/doner/internal/parser"
)

// Builder builds and validates Docker images
type Builder interface {
	// Build builds a Docker image from a Dockerfile
	Build(ctx context.Context, dockerfilePath string) (string, error)

	// Validate runs healthcheck and validates the image
	Validate(ctx context.Context, imageID string, healthcheck *Healthcheck) error

	// Cleanup removes the built image
	Cleanup(ctx context.Context, imageID string) error
}

// Healthcheck represents a Docker HEALTHCHECK instruction
type Healthcheck struct {
	Command  string
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
}

// DockerBuilder implements Builder using Docker CLI
type DockerBuilder struct {
	verbose bool
}

// NewDockerBuilder creates a new Docker builder
func NewDockerBuilder(verbose bool) *DockerBuilder {
	return &DockerBuilder{
		verbose: verbose,
	}
}

// Build builds a Docker image and returns the image ID
func (b *DockerBuilder) Build(ctx context.Context, dockerfilePath string) (string, error) {
	args := []string{"build", "-f", dockerfilePath, "-q", "."}
	if b.verbose {
		args = []string{"build", "-f", dockerfilePath, "."}
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker build failed: %w\nOutput: %s", err, string(output))
	}

	// Extract image ID from output
	imageID := strings.TrimSpace(string(output))
	// In quiet mode, output is just the SHA256
	// In normal mode, extract from "writing image sha256:..."
	if strings.Contains(imageID, "sha256:") {
		parts := strings.Split(imageID, "sha256:")
		if len(parts) > 1 {
			imageID = "sha256:" + strings.Fields(parts[1])[0]
		}
	}

	return imageID, nil
}

// Validate runs the healthcheck against the image
func (b *DockerBuilder) Validate(ctx context.Context, imageID string, healthcheck *Healthcheck) error {
	if healthcheck == nil {
		// No healthcheck defined, skip validation
		return nil
	}

	// Create a container from the image
	containerName := fmt.Sprintf("doner-validate-%d", time.Now().Unix())
	createCmd := exec.CommandContext(ctx, "docker", "create", "--name", containerName, imageID)
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating container: %w\nOutput: %s", err, string(output))
	}

	// Ensure cleanup
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName).Run()
	}()

	// Start the container
	startCmd := exec.CommandContext(ctx, "docker", "start", containerName)
	if output, err := startCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting container: %w\nOutput: %s", err, string(output))
	}

	// Wait for container to be healthy or timeout
	maxRetries := healthcheck.Retries
	if maxRetries == 0 {
		maxRetries = 3
	}

	interval := healthcheck.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}

	for i := 0; i < maxRetries; i++ {
		time.Sleep(interval)

		inspectCmd := exec.CommandContext(ctx, "docker", "inspect", "--format={{.State.Health.Status}}", containerName)
		output, err := inspectCmd.Output()
		if err != nil {
			// Container might not have health status yet
			continue
		}

		status := strings.TrimSpace(string(output))
		if status == "healthy" {
			return nil
		}
		if status == "unhealthy" {
			return fmt.Errorf("healthcheck failed: container unhealthy")
		}
	}

	return fmt.Errorf("healthcheck timeout after %d retries", maxRetries)
}

// Cleanup removes the built image
func (b *DockerBuilder) Cleanup(ctx context.Context, imageID string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("removing image: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// ExtractHealthcheck extracts HEALTHCHECK instruction from Dockerfile
func ExtractHealthcheck(df *parser.Dockerfile) *Healthcheck {
	for _, instr := range df.Instructions {
		if instr.Command == "HEALTHCHECK" {
			return parseHealthcheck(instr.Args)
		}
	}
	return nil
}

// parseHealthcheck parses HEALTHCHECK instruction arguments
func parseHealthcheck(args string) *Healthcheck {
	hc := &Healthcheck{
		Interval: 30 * time.Second,
		Timeout:  30 * time.Second,
		Retries:  3,
	}

	// Simple parsing - extract CMD
	if idx := strings.Index(args, "CMD"); idx != -1 {
		hc.Command = strings.TrimSpace(args[idx+3:])
	}

	// Parse interval, timeout, retries if present
	if strings.Contains(args, "--interval=") {
		if d, err := parseHealthcheckDuration(args, "--interval="); err == nil {
			hc.Interval = d
		}
	}
	if strings.Contains(args, "--timeout=") {
		if d, err := parseHealthcheckDuration(args, "--timeout="); err == nil {
			hc.Timeout = d
		}
	}
	if strings.Contains(args, "--retries=") {
		if r, err := parseHealthcheckRetries(args); err == nil {
			hc.Retries = r
		}
	}

	return hc
}

func parseHealthcheckDuration(args, flag string) (time.Duration, error) {
	idx := strings.Index(args, flag)
	if idx == -1 {
		return 0, fmt.Errorf("flag not found")
	}

	rest := args[idx+len(flag):]
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return 0, fmt.Errorf("no value")
	}

	return time.ParseDuration(parts[0])
}

func parseHealthcheckRetries(args string) (int, error) {
	idx := strings.Index(args, "--retries=")
	if idx == -1 {
		return 0, fmt.Errorf("flag not found")
	}

	rest := args[idx+len("--retries="):]
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return 0, fmt.Errorf("no value")
	}

	var retries int
	_, err := fmt.Sscanf(parts[0], "%d", &retries)
	return retries, err
}
