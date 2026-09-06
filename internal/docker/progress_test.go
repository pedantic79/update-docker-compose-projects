package docker

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/docker/cli/cli/streams"
	"github.com/docker/compose/v5/pkg/api"
)

func TestProjectEventProcessorUsesPlainOutputWhenANSIIsUnavailable(t *testing.T) {
	tests := []struct {
		name     string
		terminal bool
		noColor  string
		term     string
	}{
		{name: "redirected output", term: "xterm-256color"},
		{name: "NO_COLOR", terminal: true, noColor: "1", term: "xterm-256color"},
		{name: "dumb terminal", terminal: true, term: "dumb"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", test.noColor)
			t.Setenv("TERM", test.term)

			var output bytes.Buffer
			out := streams.NewOut(&output)
			out.SetIsTerminal(test.terminal)
			projectEventProcessor(out, io.Discard).On(api.Resource{
				ID:     "Image app",
				Text:   "Pulled",
				Status: api.Done,
			})

			if bytes.Contains(output.Bytes(), []byte("\x1b")) {
				t.Fatalf("plain progress contains ANSI escapes: %q", output.String())
			}
		})
	}
}

func TestProjectEventProcessorClearsCompletedTerminalLines(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	var output bytes.Buffer
	out := streams.NewOut(&output)
	out.SetIsTerminal(true)
	projectEventProcessor(out, io.Discard).On(api.Resource{
		ID:     "Image app",
		Text:   "Pulled",
		Status: api.Done,
	})

	if !bytes.Contains(output.Bytes(), []byte(eraseToEndOfLine)) {
		t.Fatalf("terminal progress does not clear completed line: %q", output.String())
	}
}

func TestLineClearingWriterClearsEveryCompletedLine(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writer := lineClearingWriter{out: &output}
	input := []byte("[+] pull 1/1\n[+] up 2/2\ntail")

	n, err := writer.Write(input)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(input) {
		t.Fatalf("Write() = %d, want %d", n, len(input))
	}
	want := "[+] pull 1/1\x1b[K\n[+] up 2/2\x1b[K\ntail"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestLineClearingWriterPropagatesOutputFailures(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("terminal disconnected")
	tests := []struct {
		name        string
		input       string
		failCall    int
		wantWritten int
	}{
		{name: "line content", input: "header\n", failCall: 1},
		{name: "erase sequence", input: "header\n", failCall: 2, wantWritten: len("header")},
		{name: "newline", input: "header\n", failCall: 3, wantWritten: len("header")},
		{name: "incomplete line", input: "tail", failCall: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			writer := lineClearingWriter{out: &failOnCallWriter{
				failCall: test.failCall,
				err:      writeErr,
			}}
			n, err := writer.Write([]byte(test.input))
			if !errors.Is(err, writeErr) {
				t.Fatalf("Write() error = %v, want %v", err, writeErr)
			}
			if n != test.wantWritten {
				t.Fatalf("Write() = %d, want %d", n, test.wantWritten)
			}
		})
	}
}

func TestLineClearingWriterReportsShortWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		shortCall   int
		wantWritten int
	}{
		{name: "line content", input: "header\n", shortCall: 1, wantWritten: len("header") - 1},
		{name: "erase sequence", input: "header\n", shortCall: 2, wantWritten: len("header")},
		{name: "newline", input: "header\n", shortCall: 3, wantWritten: len("header")},
		{name: "incomplete line", input: "tail", shortCall: 1, wantWritten: len("tail") - 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			writer := lineClearingWriter{out: &shortOnCallWriter{shortCall: test.shortCall}}
			n, err := writer.Write([]byte(test.input))
			if !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("Write() error = %v, want %v", err, io.ErrShortWrite)
			}
			if n != test.wantWritten {
				t.Fatalf("Write() = %d, want %d", n, test.wantWritten)
			}
		})
	}
}

type failOnCallWriter struct {
	calls    int
	failCall int
	err      error
}

func (w *failOnCallWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failCall {
		return 0, w.err
	}
	return len(p), nil
}

type shortOnCallWriter struct {
	calls     int
	shortCall int
}

func (w *shortOnCallWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.shortCall {
		return len(p) - 1, nil
	}
	return len(p), nil
}
