package parser

import (
	"testing"
)

func TestParse_ValidDockerfile(t *testing.T) {
	content := `FROM python:3.11
WORKDIR /app
COPY . .
RUN pip install -r requirements.txt
CMD ["python", "app.py"]`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 5 {
		t.Errorf("expected 5 instructions, got %d", len(df.Instructions))
	}

	tests := []struct {
		idx     int
		command string
		args    string
		line    int
	}{
		{0, "FROM", "python:3.11", 1},
		{1, "WORKDIR", "/app", 2},
		{2, "COPY", ". .", 3},
		{3, "RUN", "pip install -r requirements.txt", 4},
		{4, "CMD", `["python", "app.py"]`, 5},
	}

	for _, tt := range tests {
		instr := df.Instructions[tt.idx]
		if instr.Command != tt.command {
			t.Errorf("instruction[%d].Command = %s, want %s", tt.idx, instr.Command, tt.command)
		}
		if instr.Args != tt.args {
			t.Errorf("instruction[%d].Args = %s, want %s", tt.idx, instr.Args, tt.args)
		}
		if instr.Line != tt.line {
			t.Errorf("instruction[%d].Line = %d, want %d", tt.idx, instr.Line, tt.line)
		}
	}
}

func TestParse_WithDirectives(t *testing.T) {
	content := `# doner: python:3.11.#
FROM python:3.11.5
# doner: requests:2.#.#
RUN pip install requests==2.31.0`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Directives) != 2 {
		t.Errorf("expected 2 directives, got %d", len(df.Directives))
	}

	if len(df.Instructions) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(df.Instructions))
	}

	// Check first directive
	if df.Directives[0].Line != 1 {
		t.Errorf("directive[0].Line = %d, want 1", df.Directives[0].Line)
	}
	if len(df.Directives[0].Packages) != 1 {
		t.Errorf("directive[0] packages = %d, want 1", len(df.Directives[0].Packages))
	}
	if df.Directives[0].Packages[0].Name != "python" {
		t.Errorf("directive[0] package name = %s, want python", df.Directives[0].Packages[0].Name)
	}
}

func TestParse_MultiStageDockerfile(t *testing.T) {
	content := `FROM golang:1.21 AS builder
WORKDIR /build
COPY . .
RUN go build -o app

FROM alpine:3.19
COPY --from=builder /build/app /app
CMD ["/app"]`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 7 {
		t.Errorf("expected 7 instructions, got %d", len(df.Instructions))
	}

	// Check FROM instructions with AS
	if df.Instructions[0].Command != "FROM" {
		t.Errorf("instruction[0].Command = %s, want FROM", df.Instructions[0].Command)
	}
	if df.Instructions[0].Args != "golang:1.21 AS builder" {
		t.Errorf("instruction[0].Args = %s, want 'golang:1.21 AS builder'", df.Instructions[0].Args)
	}

	// Check COPY --from
	if df.Instructions[5].Command != "COPY" {
		t.Errorf("instruction[5].Command = %s, want COPY", df.Instructions[5].Command)
	}
	if df.Instructions[5].Args != "--from=builder /build/app /app" {
		t.Errorf("instruction[5].Args = %s", df.Instructions[5].Args)
	}
}

func TestParse_WithComments(t *testing.T) {
	content := `# This is a comment
FROM python:3.11
# Another comment
# doner: requests:2.#.#
RUN pip install requests
# Final comment`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(df.Instructions))
	}

	if len(df.Directives) != 1 {
		t.Errorf("expected 1 directive, got %d", len(df.Directives))
	}
}

func TestParse_EmptyLines(t *testing.T) {
	content := `FROM python:3.11

WORKDIR /app

COPY . .`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 3 {
		t.Errorf("expected 3 instructions, got %d", len(df.Instructions))
	}
}

func TestParse_EmptyDockerfile(t *testing.T) {
	content := ``

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 0 {
		t.Errorf("expected 0 instructions, got %d", len(df.Instructions))
	}

	if len(df.Directives) != 0 {
		t.Errorf("expected 0 directives, got %d", len(df.Directives))
	}
}

func TestParse_OnlyComments(t *testing.T) {
	content := `# Comment 1
# Comment 2
# Comment 3`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 0 {
		t.Errorf("expected 0 instructions, got %d", len(df.Instructions))
	}
}

func TestParseInstruction_CommandOnly(t *testing.T) {
	instr := parseInstruction("WORKDIR", 1)
	if instr == nil {
		t.Fatal("expected instruction, got nil")
	}

	if instr.Command != "WORKDIR" {
		t.Errorf("Command = %s, want WORKDIR", instr.Command)
	}
	if instr.Args != "" {
		t.Errorf("Args = %s, want empty", instr.Args)
	}
}

func TestParseInstruction_WithArgs(t *testing.T) {
	instr := parseInstruction("FROM python:3.11", 1)
	if instr == nil {
		t.Fatal("expected instruction, got nil")
	}

	if instr.Command != "FROM" {
		t.Errorf("Command = %s, want FROM", instr.Command)
	}
	if instr.Args != "python:3.11" {
		t.Errorf("Args = %s, want python:3.11", instr.Args)
	}
}

func TestParseInstruction_LowercaseCommand(t *testing.T) {
	instr := parseInstruction("from python:3.11", 1)
	if instr == nil {
		t.Fatal("expected instruction, got nil")
	}

	if instr.Command != "FROM" {
		t.Errorf("Command = %s, want FROM (uppercase)", instr.Command)
	}
}

func TestParseInstruction_WithTabs(t *testing.T) {
	instr := parseInstruction("FROM\tpython:3.11", 1)
	if instr == nil {
		t.Fatal("expected instruction, got nil")
	}

	if instr.Command != "FROM" {
		t.Errorf("Command = %s, want FROM", instr.Command)
	}
	if instr.Args != "python:3.11" {
		t.Errorf("Args = %s, want python:3.11", instr.Args)
	}
}

func TestParseInstruction_Empty(t *testing.T) {
	instr := parseInstruction("", 1)
	if instr != nil {
		t.Errorf("expected nil for empty line, got %v", instr)
	}
}

func TestParseInstruction_Whitespace(t *testing.T) {
	instr := parseInstruction("   ", 1)
	if instr != nil {
		t.Errorf("expected nil for whitespace line, got %v", instr)
	}
}

func TestParse_ComplexMultiStage(t *testing.T) {
	content := `# doner: golang:1.21.#
FROM golang:1.21.5 AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o app

# doner: alpine:3.19.#
FROM alpine:3.19.0
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/app /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]`

	df, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(df.Instructions) != 10 {
		t.Errorf("expected 10 instructions, got %d", len(df.Instructions))
	}

	if len(df.Directives) != 2 {
		t.Errorf("expected 2 directives, got %d", len(df.Directives))
	}

	// Verify directive positions
	if df.Directives[0].Line != 1 {
		t.Errorf("directive[0].Line = %d, want 1", df.Directives[0].Line)
	}
	if df.Directives[1].Line != 9 {
		t.Errorf("directive[1].Line = %d, want 9", df.Directives[1].Line)
	}
}
