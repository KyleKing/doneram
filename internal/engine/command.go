package engine

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/locator"
)

// runCommandSite runs a Site's Command and parses its output with
// CommandPattern, a regex with "name", "current", and "latest" named
// groups, picking the line whose name matches the site's tool.
func runCommandSite(ctx context.Context, s Site) SiteResult {
	result := SiteResult{Site: s}

	re, err := regexp.Compile(s.CommandPattern)
	if err != nil {
		result.Err = fmt.Errorf("compiling command pattern %q: %w", s.CommandPattern, err)
		return result
	}
	nameIdx := re.SubexpIndex("name")
	currentIdx := re.SubexpIndex("current")
	latestIdx := re.SubexpIndex("latest")
	if nameIdx == -1 || currentIdx == -1 || latestIdx == -1 {
		result.Err = fmt.Errorf("command pattern %q must have name, current, and latest named groups", s.CommandPattern)
		return result
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", s.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		result.Err = fmt.Errorf("running command %q: %w\noutput: %s", s.Command, err, output)
		return result
	}

	want := s.resolverName()
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if matches == nil || matches[nameIdx] != want {
			continue
		}
		result.Matches = []locator.Match{{Value: matches[currentIdx]}}
		result.Latest = matches[latestIdx]
		return result
	}

	result.Err = fmt.Errorf("command %q reported no entry for %q", s.Command, want)
	return result
}
