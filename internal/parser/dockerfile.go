package parser

import (
	"strings"
)

type Dockerfile struct {
	Path         string
	Instructions []Instruction
	Directives   []*Directive
}

type Instruction struct {
	Command string
	Args    string
	Line    int
	Raw     string
}

func Parse(content string) (*Dockerfile, error) {
	df := &Dockerfile{}
	lines := strings.Split(content, "\n")

	var pendingDirective *Directive

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if directive := ParseDirective(line, lineNum); directive != nil {
			pendingDirective = directive
			df.Directives = append(df.Directives, directive)
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		instr := parseInstruction(line, lineNum)
		if instr != nil {
			if pendingDirective != nil {
				pendingDirective = nil
			}
			df.Instructions = append(df.Instructions, *instr)
		}
	}

	return df, nil
}

func parseInstruction(line string, lineNum int) *Instruction {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	spaceIdx := strings.IndexAny(trimmed, " \t")
	if spaceIdx == -1 {
		return &Instruction{
			Command: strings.ToUpper(trimmed),
			Line:    lineNum,
			Raw:     line,
		}
	}

	command := strings.ToUpper(trimmed[:spaceIdx])
	args := strings.TrimSpace(trimmed[spaceIdx+1:])

	return &Instruction{
		Command: command,
		Args:    args,
		Line:    lineNum,
		Raw:     line,
	}
}
