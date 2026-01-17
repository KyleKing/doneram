package builder

import (
	"testing"
	"time"

	"github.com/kyleking/doner/internal/parser"
)

func TestExtractHealthcheck(t *testing.T) {
	tests := []struct {
		name string
		df   *parser.Dockerfile
		want *Healthcheck
	}{
		{
			name: "simple healthcheck",
			df: &parser.Dockerfile{
				Instructions: []parser.Instruction{
					{Command: "FROM", Args: "python:3.13"},
					{Command: "HEALTHCHECK", Args: "CMD curl -f http://localhost/ || exit 1"},
				},
			},
			want: &Healthcheck{
				Command:  "curl -f http://localhost/ || exit 1",
				Interval: 30 * time.Second,
				Timeout:  30 * time.Second,
				Retries:  3,
			},
		},
		{
			name: "healthcheck with options",
			df: &parser.Dockerfile{
				Instructions: []parser.Instruction{
					{Command: "HEALTHCHECK", Args: "--interval=5s --timeout=3s --retries=2 CMD curl -f http://localhost/"},
				},
			},
			want: &Healthcheck{
				Command:  "curl -f http://localhost/",
				Interval: 5 * time.Second,
				Timeout:  3 * time.Second,
				Retries:  2,
			},
		},
		{
			name: "no healthcheck",
			df: &parser.Dockerfile{
				Instructions: []parser.Instruction{
					{Command: "FROM", Args: "python:3.13"},
					{Command: "RUN", Args: "apt-get update"},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHealthcheck(tt.df)

			if tt.want == nil {
				if got != nil {
					t.Errorf("ExtractHealthcheck() = %+v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatalf("ExtractHealthcheck() = nil, want %+v", tt.want)
			}

			if got.Command != tt.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tt.want.Command)
			}
			if got.Interval != tt.want.Interval {
				t.Errorf("Interval = %v, want %v", got.Interval, tt.want.Interval)
			}
			if got.Timeout != tt.want.Timeout {
				t.Errorf("Timeout = %v, want %v", got.Timeout, tt.want.Timeout)
			}
			if got.Retries != tt.want.Retries {
				t.Errorf("Retries = %d, want %d", got.Retries, tt.want.Retries)
			}
		})
	}
}

func TestParseHealthcheck(t *testing.T) {
	tests := []struct {
		name string
		args string
		want *Healthcheck
	}{
		{
			name: "simple CMD",
			args: "CMD curl -f http://localhost/",
			want: &Healthcheck{
				Command:  "curl -f http://localhost/",
				Interval: 30 * time.Second,
				Timeout:  30 * time.Second,
				Retries:  3,
			},
		},
		{
			name: "with all options",
			args: "--interval=10s --timeout=5s --retries=5 CMD /healthcheck.sh",
			want: &Healthcheck{
				Command:  "/healthcheck.sh",
				Interval: 10 * time.Second,
				Timeout:  5 * time.Second,
				Retries:  5,
			},
		},
		{
			name: "with partial options",
			args: "--interval=1m CMD test -f /ready",
			want: &Healthcheck{
				Command:  "test -f /ready",
				Interval: 1 * time.Minute,
				Timeout:  30 * time.Second,
				Retries:  3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseHealthcheck(tt.args)

			if got.Command != tt.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tt.want.Command)
			}
			if got.Interval != tt.want.Interval {
				t.Errorf("Interval = %v, want %v", got.Interval, tt.want.Interval)
			}
			if got.Timeout != tt.want.Timeout {
				t.Errorf("Timeout = %v, want %v", got.Timeout, tt.want.Timeout)
			}
			if got.Retries != tt.want.Retries {
				t.Errorf("Retries = %d, want %d", got.Retries, tt.want.Retries)
			}
		})
	}
}
