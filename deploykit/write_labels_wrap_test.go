package deploykit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opencharly/spec/shellquote"
)

// joinedDockerfileLines applies the format's own line-continuation rule: a backslash
// immediately before a newline is removed, joining the line with the next one.
func joinedDockerfileLines(s string) (lines []string) {
	for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if n := len(lines); n > 0 && strings.HasSuffix(lines[n-1], "\\") {
			lines[n-1] = strings.TrimSuffix(lines[n-1], "\\") + l
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// A value that fits on one line is emitted exactly as before: the wrap must not perturb the
// common case, which is every label on a small box.
func TestWriteJSONLabel_ShortValueOnOneLine(t *testing.T) {
	var b strings.Builder
	writeJSONLabel(&b, "k", []string{"a", "b"})
	if got, want := b.String(), "LABEL k='[\"a\",\"b\"]'\n"; got != want {
		t.Fatalf("short value changed shape:\n got %q\nwant %q", got, want)
	}
	if strings.Count(b.String(), "\n") != 1 {
		t.Fatalf("short value must be ONE line, got %d newlines", strings.Count(b.String(), "\n"))
	}
}

// The regression this file exists for: a description larger than buildkit's 64 KiB line
// limit (opencharly/sdk#553, `box/immich-ml`). Every emitted line stays inside the cap, and
// the label the Dockerfile parser reconstructs is byte-identical to the unwrapped value.
func TestWriteJSONLabel_LongValueWrapsAndStaysByteIdentical(t *testing.T) {
	// One JSON string past the 65535-byte cap, carrying the characters that stress the shell
	// quoting (a single quote and a backslash) on both sides of every split point.
	item := strings.Repeat(`x'y\z"`, 9000)
	value := []string{item}

	var b strings.Builder
	writeJSONLabel(&b, "ai.opencharly.description", value)
	emitted := b.String()

	if len(emitted) <= 65535 {
		t.Fatalf("fixture is not larger than the format's cap: %d bytes", len(emitted))
	}
	for i, line := range strings.Split(strings.TrimSuffix(emitted, "\n"), "\n") {
		if len(line) > maxLabelLineBytes {
			t.Fatalf("emitted line %d is %d bytes, over the %d cap", i, len(line), maxLabelLineBytes)
		}
		if strings.HasSuffix(line, "\\\\") {
			t.Fatalf("emitted line %d ends in an even run of backslashes, which is not a continuation", i)
		}
	}

	lines := joinedDockerfileLines(emitted)
	if len(lines) != 1 {
		t.Fatalf("the parser must reconstruct ONE logical line, got %d", len(lines))
	}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := "LABEL ai.opencharly.description=" + shellquote.ShellQuote(string(data))
	if lines[0] != want {
		t.Fatalf("the wrapped label differs from the unwrapped one:\n got %q\nwant %q",
			head(lines[0]), head(want))
	}
}

func head(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:80] + "…"
}
