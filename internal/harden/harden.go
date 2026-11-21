// Package harden finds references that name a moving target, a GitHub
// Action used by tag or branch and a mise tool given as "latest" or a bare
// major, and rewrites them as exact pins. An exact pin is what the rest of
// doneram can reason about: discovery only sees an action pinned by SHA with
// its version in a comment, an advisory range is only checked against a
// full version, and a tag can be moved to a different commit after review.
//
// Scanning and rewriting are separate from resolving: Scan reads the repo,
// a Resolver supplies each replacement, and Apply writes them back,
// touching nothing but the reference itself.
package harden

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind names what a Finding pins.
type Kind string

const (
	Action Kind = "action"
	Tool   Kind = "tool"
)

// Finding is one reference that does not name an exact version.
type Finding struct {
	Kind Kind
	File string
	Line int
	// Name is the action repo ("actions/checkout") or the mise tool.
	Name string
	// Ref is the reference as written ("v4", "main", "latest", "22").
	Ref string
	// Replacement is the pinned text for Ref, set by Resolve. An action's is
	// "<sha> # <tag>", a tool's is the exact version.
	Replacement string
	Err         error

	// start and end bound the text on Line that Replacement takes over.
	start, end int
}

// Resolver supplies the exact pin for a finding.
type Resolver interface {
	// PinAction returns "<sha> # <tag>" for an action used at ref.
	PinAction(ctx context.Context, repo, ref string) (string, error)
	// PinTool returns the exact version mise would install for ref.
	PinTool(ctx context.Context, tool, ref string) (string, error)
}

var actionFiles = []string{
	".github/workflows/*.yml",
	".github/workflows/*.yaml",
	".github/actions/*/action.yml",
	".github/actions/*/action.yaml",
	"action.yml",
	"action.yaml",
}

// actionRef matches a remote `uses:` with its ref, quoted or not, and any
// trailing comment. Local (./) and docker:// uses have no "owner/repo@".
var actionRef = regexp.MustCompile(`uses:\s*(['"]?)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)((?:/[^@\s'"]*)?)@([^\s'"#]+)(['"]?)(\s*#.*)?$`)

// versionRef is a ref naming a release rather than a branch. A branch has
// no version to put in the comment, so discovery could never update it.
var versionRef = regexp.MustCompile(`^v?\d`)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// exactVersion is a full three-part version, the one form mise and an
// advisory range both read as a single release.
var exactVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+([.+-][\w.+-]*)?$`)

// floatingTool is a tool value mise resolves at install time: "latest",
// "lts", or a version prefix with fewer than three parts.
var floatingTool = regexp.MustCompile(`^(latest|lts|\d+(\.\d+)?)$`)

var miseLine = regexp.MustCompile(`^(\s*("[^"]+"|[A-Za-z0-9_.:/@-]+)\s*=\s*")([^"]*)"`)

// Scan returns every floating reference under baseDir, in file and line
// order.
func Scan(baseDir string) ([]Finding, error) {
	var files []string
	for _, g := range actionFiles {
		found, err := filepath.Glob(filepath.Join(baseDir, g))
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	sort.Strings(files)

	var out []Finding
	for _, file := range files {
		lines, err := readLines(file)
		if err != nil {
			return nil, err
		}
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			m := actionRef.FindStringSubmatchIndex(line)
			if m == nil {
				continue
			}
			ref := line[m[8]:m[9]]
			if fullSHA.MatchString(ref) {
				continue
			}
			// The replacement starts at the opening quote (or the repo) and
			// runs to the end of the line, so quotes and an old comment
			// naming the floating ref are rewritten together.
			out = append(out, Finding{
				Kind: Action, File: file, Line: i + 1,
				Name: line[m[4]:m[5]], Ref: ref,
				start: m[2], end: len(line),
			})
		}
	}

	tools, err := scanTools(baseDir)
	if err != nil {
		return nil, err
	}
	return append(out, tools...), nil
}

func scanTools(baseDir string) ([]Finding, error) {
	var out []Finding
	for _, name := range []string{"mise.toml", ".mise.toml"} {
		path := filepath.Join(baseDir, name)
		lines, err := readLines(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		section := ""
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				section = strings.Trim(trimmed, "[] ")
				continue
			}
			if section != "tools" {
				continue
			}
			m := miseLine.FindStringSubmatchIndex(line)
			if m == nil {
				continue
			}
			ref := line[m[6]:m[7]]
			if !floatingTool.MatchString(ref) {
				continue
			}
			out = append(out, Finding{
				Kind: Tool, File: path, Line: i + 1,
				Name: strings.Trim(line[m[4]:m[5]], `"`), Ref: ref,
				start: m[6], end: m[7],
			})
		}
	}

	path := filepath.Join(baseDir, ".tool-versions")
	lines, err := readLines(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") || !floatingTool.MatchString(fields[1]) {
			continue
		}
		start := strings.Index(line, fields[0]) + len(fields[0])
		start += strings.Index(line[start:], fields[1])
		out = append(out, Finding{
			Kind: Tool, File: path, Line: i + 1,
			Name: fields[0], Ref: fields[1],
			start: start, end: start + len(fields[1]),
		})
	}
	return out, nil
}

// Resolve fills each finding's Replacement, or its Err. Findings that name
// the same thing at the same ref are resolved once.
func Resolve(ctx context.Context, findings []Finding, r Resolver) {
	type key struct {
		kind      Kind
		name, ref string
	}
	type answer struct {
		value string
		err   error
	}
	memo := map[key]answer{}
	for i := range findings {
		f := &findings[i]
		k := key{f.Kind, f.Name, f.Ref}
		a, ok := memo[k]
		if !ok {
			switch f.Kind {
			case Action:
				if !versionRef.MatchString(f.Ref) {
					a.err = fmt.Errorf("%s@%s tracks a branch, not a release; pin it by hand to the commit you reviewed", f.Name, f.Ref)
					break
				}
				a.value, a.err = r.PinAction(ctx, f.Name, f.Ref)
			case Tool:
				a.value, a.err = r.PinTool(ctx, f.Name, f.Ref)
				if a.err == nil && !exactVersion.MatchString(a.value) {
					a.err = fmt.Errorf("mise resolved %s@%s to %q, not an exact version", f.Name, f.Ref, a.value)
				}
			}
			memo[k] = a
		}
		f.Replacement, f.Err = a.value, a.err
	}
}

// Apply rewrites every resolved finding in place and returns how many
// changed. A file whose line no longer reads as scanned is left alone.
func Apply(findings []Finding) (int, error) {
	byFile := map[string][]*Finding{}
	for i := range findings {
		f := &findings[i]
		if f.Err == nil && f.Replacement != "" {
			byFile[f.File] = append(byFile[f.File], f)
		}
	}
	changed := 0
	for file, fs := range byFile {
		data, err := os.ReadFile(file)
		if err != nil {
			return changed, err
		}
		lines := strings.Split(string(data), "\n")
		for _, f := range fs {
			line := lines[f.Line-1]
			if f.end > len(line) || !strings.Contains(line[f.start:f.end], f.Ref) {
				return changed, fmt.Errorf("%s:%d changed since it was scanned", file, f.Line)
			}
			lines[f.Line-1] = line[:f.start] + f.rewrite(line[f.start:f.end]) + line[f.end:]
			changed++
		}
		info, err := os.Stat(file)
		if err != nil {
			return changed, err
		}
		if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), info.Mode()); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// rewrite renders the replacement for the span Scan recorded. An action
// becomes `owner/repo[/path]@<sha> # <tag>`, unquoted, keeping any comment
// that said more than the old ref.
func (f Finding) rewrite(span string) string {
	if f.Kind == Tool {
		return f.Replacement
	}
	m := actionRef.FindStringSubmatch("uses: " + span)
	if m == nil {
		return span
	}
	sha, tag, _ := strings.Cut(f.Replacement, " # ")
	out := m[2] + m[3] + "@" + sha + " # " + tag
	if comment := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[6]), "#")); comment != "" && comment != f.Ref && comment != tag {
		out += " (" + comment + ")"
	}
	return out
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}
