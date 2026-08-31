package parser

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/locator"
)

// CompiledSite is a `# doneram:` directive and the FROM or COPY --from line
// it applies to, compiled into a locator anchored on that exact line so the
// same find/resolve engine that drives a pkl config drives a Dockerfile.
type CompiledSite struct {
	Tool         string
	ResolverName string
	Constraint   *VersionPattern
	Locator      locator.Locator
}

// CompileLocators turns every non-ignored directive/instruction pair in df
// into a CompiledSite anchored to path.
func CompileLocators(path string, df *Dockerfile) []CompiledSite {
	directiveMap := make(map[int]*Directive, len(df.Directives))
	for _, d := range df.Directives {
		directiveMap[d.Line] = d
	}

	var sites []CompiledSite
	for _, instr := range df.Instructions {
		directive := directiveMap[instr.Line-1]
		if directive == nil || directive.Ignore || len(directive.Packages) == 0 {
			continue
		}

		var imageName, version string
		switch instr.Command {
		case "FROM":
			imageName, version = parseImageRef(instr.Args)
		case "COPY":
			if strings.Contains(instr.Args, "--from=") {
				imageName, version = parseCopyImageRef(instr.Args)
			}
		}
		if imageName == "" || version == "" {
			continue
		}

		pkg := directive.Packages[0]
		if pkg.Ignore {
			continue
		}

		pattern, err := linePattern(instr.Raw, version)
		if err != nil {
			continue
		}

		sites = append(sites, CompiledSite{
			Tool:         pkg.Name,
			ResolverName: imageName,
			Constraint:   pkg.Pattern,
			Locator: locator.Locator{
				Glob:     path,
				Pattern:  pattern,
				Resolver: resolverKind(imageName),
				Expect:   1,
			},
		})
	}

	return sites
}

func resolverKind(imageName string) string {
	if strings.Contains(imageName, "ghcr.io") {
		return "ghcr"
	}
	return "docker"
}

var imageRefRegex = regexp.MustCompile(`^([^:]+):(.+)$`)

func parseImageRef(args string) (string, string) {
	matches := imageRefRegex.FindStringSubmatch(stripAlias(args))
	if matches == nil {
		return "", ""
	}
	return matches[1], matches[2]
}

var copyFromRefRegex = regexp.MustCompile(`--from=([^\s]+)`)

func parseCopyImageRef(args string) (string, string) {
	matches := copyFromRefRegex.FindStringSubmatch(args)
	if matches == nil {
		return "", ""
	}
	return parseImageRef(matches[1])
}

func stripAlias(image string) string {
	image = strings.TrimSpace(image)
	if idx := strings.Index(image, " AS "); idx != -1 {
		image = image[:idx]
	}
	if idx := strings.Index(image, " as "); idx != -1 {
		image = image[:idx]
	}
	return strings.TrimSpace(image)
}

// linePattern anchors a regex to raw's exact text with version captured, so
// the locator matches only this one occurrence of the directive's pin.
func linePattern(raw, version string) (string, error) {
	idx := strings.Index(raw, version)
	if idx == -1 {
		return "", fmt.Errorf("version %q not found in line %q", version, raw)
	}
	prefix := regexp.QuoteMeta(raw[:idx])
	suffix := regexp.QuoteMeta(raw[idx+len(version):])
	return "^" + prefix + "(" + regexp.QuoteMeta(version) + ")" + suffix + "$", nil
}
