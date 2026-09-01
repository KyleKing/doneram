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
// groups, picking the line whose name matches the site's tool. An entry
// that matches with an empty latest group is up to date, which is how tools
// like `uv tree --outdated` report a current package. Located matches, when
// the site has a file, override the command's own current value.
func runCommandSite(ctx context.Context, s Site, matches []locator.Match) SiteResult {
	result := SiteResult{Site: s, Matches: matches}

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
		entry := re.FindStringSubmatch(scanner.Text())
		if entry == nil || entry[nameIdx] != want {
			continue
		}
		if len(matches) == 0 {
			result.Matches = []locator.Match{{Value: entry[currentIdx]}}
		}
		result.Latest = entry[latestIdx]
		if result.Latest == "" {
			result.Latest = result.Matches[0].Value
		}
		return result
	}

	result.Err = fmt.Errorf("command %q reported no entry for %q", s.Command, want)
	return result
}
