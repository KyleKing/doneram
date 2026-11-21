package fleet

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Git runs git for a fleet run. The token travels as an http.extraheader
// set through GIT_CONFIG_* variables: never in a URL, which git prints in
// errors and writes to .git/config, and never in argv, which other
// processes on the machine can read.
type Git struct {
	Token string
	// Server is the URL prefix the header is scoped to, so the token goes
	// to the host the clones come from and nowhere else.
	Server string
}

// Run runs git in dir and returns its combined output, with the token
// scrubbed from both the output and the error.
func (g *Git) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if g.Token != "" && g.Server != "" {
		cmd.Env = append(cmd.Env, g.headerEnv()...)
	}
	out, err := cmd.CombinedOutput()
	text := g.redact(strings.TrimSpace(string(out)))
	if err != nil {
		return text, fmt.Errorf("git %s: %w: %s", subcommand(args), err, lastLines(text, 15))
	}
	return text, nil
}

// headerEnv adds the auth header after any GIT_CONFIG_* entries the
// environment already sets, rather than replacing them.
func (g *Git) headerEnv() []string {
	n, _ := strconv.Atoi(os.Getenv("GIT_CONFIG_COUNT"))
	server := strings.TrimRight(g.Server, "/") + "/"
	return []string{
		fmt.Sprintf("GIT_CONFIG_COUNT=%d", n+1),
		fmt.Sprintf("GIT_CONFIG_KEY_%d=http.%s.extraheader", n, server),
		// Basic with x-access-token is what actions/checkout sends; it is
		// accepted for app installation tokens and personal tokens alike.
		fmt.Sprintf("GIT_CONFIG_VALUE_%d=AUTHORIZATION: basic %s", n, g.basic()),
	}
}

func (g *Git) basic() string {
	return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.Token))
}

func (g *Git) redact(s string) string {
	if g.Token == "" {
		return s
	}
	return strings.NewReplacer(g.Token, "***", g.basic(), "***").Replace(s)
}

// subcommand names the git command in an error, past any -c settings.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
