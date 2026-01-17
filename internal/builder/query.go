package builder

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ContainerQuerier interface {
	QueryPackages(ctx context.Context, imageID string, queryCmd string) (string, error)
}

type DockerQuerier struct {
	verbose bool
}

func NewDockerQuerier(verbose bool) *DockerQuerier {
	return &DockerQuerier{
		verbose: verbose,
	}
}

func (q *DockerQuerier) QueryPackages(ctx context.Context, imageID string, queryCmd string) (string, error) {
	containerName := fmt.Sprintf("doner-query-%d", time.Now().Unix())

	createCmd := exec.CommandContext(ctx, "docker", "create", "--name", containerName, imageID, "sh", "-c", queryCmd)
	if output, err := createCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("creating query container: %w\nOutput: %s", err, string(output))
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "docker", "rm", "-f", containerName).Run()
	}()

	startCmd := exec.CommandContext(ctx, "docker", "start", containerName)
	if output, err := startCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("starting query container: %w\nOutput: %s", err, string(output))
	}

	waitCmd := exec.CommandContext(ctx, "docker", "wait", containerName)
	if output, err := waitCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("waiting for query container: %w\nOutput: %s", err, string(output))
	}

	logsCmd := exec.CommandContext(ctx, "docker", "logs", containerName)
	output, err := logsCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("getting query container logs: %w\nOutput: %s", err, string(output))
	}

	return strings.TrimSpace(string(output)), nil
}
