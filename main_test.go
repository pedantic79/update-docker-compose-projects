package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
)

func TestRunCommandRendersSuccessfulRunWithoutDocker(t *testing.T) {
	t.Parallel()

	backend := &commandBackend{projects: []updater.ProjectRef{
		{Name: "stopped", Status: "exited(1)"},
		{Name: "running", Status: "running(1)", Services: []string{"web"}},
	}}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runCommand(context.Background(), &stdout, &stderr, func() (backendCloser, error) {
		return backend, nil
	})

	if code != 0 {
		t.Fatalf("runCommand() = %d, stderr = %q", code, stderr.String())
	}
	wantStdout := "Name:stopped, Status:exited(1)\n\n" +
		"Name:running, Status:running(1)\n\n" +
		"Pruning images...\n" +
		"Pruned unused images.\n"
	if stdout.String() != wantStdout {
		t.Fatalf("stdout = %q, want %q", stdout.String(), wantStdout)
	}
	if stderr.String() != "skipping stopped: no running services\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if backend.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", backend.closeCalls)
	}
}

func TestRunCommandReportsPruneFailureWithoutSuccessOutput(t *testing.T) {
	t.Parallel()

	pruneErr := errors.New("prune failed")
	backend := &commandBackend{
		projects: []updater.ProjectRef{{Name: "app", Status: "running(1)", Services: []string{"web"}}},
		pruneErr: pruneErr,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runCommand(context.Background(), &stdout, &stderr, func() (backendCloser, error) {
		return backend, nil
	})

	if code != 1 {
		t.Fatalf("runCommand() = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "Pruning images...") || strings.Contains(stdout.String(), "Pruned unused images.") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "prune images: prune failed") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunCommandReportsInitializationFailure(t *testing.T) {
	t.Parallel()

	initErr := errors.New("invalid context")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCommand(context.Background(), &stdout, &stderr, func() (backendCloser, error) {
		return nil, initErr
	})

	if code != 1 {
		t.Fatalf("runCommand() = %d, want 1", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "initialize: invalid context") {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestRunCommandReportsOutputFailure(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("broken pipe")
	backend := &commandBackend{
		projects: []updater.ProjectRef{{Name: "app", Status: "running(1)", Services: []string{"web"}}},
	}
	var stderr bytes.Buffer

	code := runCommand(context.Background(), failingWriter{err: writeErr}, &stderr, func() (backendCloser, error) {
		return backend, nil
	})

	if code != 1 {
		t.Fatalf("runCommand() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "write stdout: broken pipe") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if backend.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", backend.closeCalls)
	}
	if backend.openCalls != 0 {
		t.Fatalf("open calls = %d, want none after project-heading failure", backend.openCalls)
	}
}

func TestConsoleReporterUsesColorForVisualHierarchy(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	reporter := &consoleReporter{
		stdout:      &stdout,
		stderr:      &stderr,
		stdoutColor: true,
		stderrColor: true,
	}

	if err := reporter.ProjectStarted(updater.ProjectRef{Name: "app", Status: "running(1)"}); err != nil {
		t.Fatalf("ProjectStarted() error = %v", err)
	}
	if err := reporter.ProjectFinished(updater.ProjectResult{
		Name:   "app",
		Status: updater.ProjectSkipped,
		Reason: "no running services",
	}); err != nil {
		t.Fatalf("ProjectFinished() error = %v", err)
	}
	if err := reporter.PruneStarted(); err != nil {
		t.Fatalf("PruneStarted() error = %v", err)
	}
	if err := reporter.PruneFinished(nil); err != nil {
		t.Fatalf("PruneFinished() error = %v", err)
	}

	for _, sequence := range []string{
		"\x1b[31mapp\x1b[0m",
		"\x1b[34mrunning(1)\x1b[0m",
		"\x1b[31mPruning images...\x1b[0m",
	} {
		if !strings.Contains(stdout.String(), sequence) {
			t.Errorf("stdout %q does not contain %q", stdout.String(), sequence)
		}
	}
	if !strings.Contains(stderr.String(), "\x1b[31mapp\x1b[0m") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if supportsColor(&bytes.Buffer{}) {
		t.Fatal("non-terminal writer should not enable color")
	}
}

func TestSupportsColorHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	if supportsColor(os.Stdout) {
		t.Fatal("NO_COLOR should disable color for terminal output")
	}
}

func TestSupportsColorChecksFileDescriptor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatalf("create output file: %v", err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Errorf("close output file: %v", err)
		}
	})

	if supportsColor(file) {
		t.Fatal("regular file should not enable color")
	}
}

func TestConsoleReporterStderrColorMismatch(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	reporter := &consoleReporter{
		stdout:      &stdout,
		stderr:      &stderr,
		stdoutColor: true,
		stderrColor: false,
	}

	if err := reporter.ProjectStarted(updater.ProjectRef{Name: "app", Status: "running(1)"}); err != nil {
		t.Fatalf("ProjectStarted() error = %v", err)
	}
	if err := reporter.ProjectFinished(updater.ProjectResult{
		Name:   "app",
		Status: updater.ProjectSkipped,
		Reason: "no running services",
	}); err != nil {
		t.Fatalf("ProjectFinished() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "\x1b[31mapp\x1b[0m") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("redirected stderr contains ANSI escape codes: %q", stderr.String())
	}
	if stderr.String() != "skipping app: no running services\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestConsoleReporterReportsStderrWriteFailure(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("broken pipe")
	reporter := &consoleReporter{
		stdout: &bytes.Buffer{},
		stderr: failingWriter{err: writeErr},
	}

	err := reporter.ProjectFinished(updater.ProjectResult{
		Name:   "stopped",
		Status: updater.ProjectSkipped,
		Reason: "no running services",
	})

	if !errors.Is(err, writeErr) {
		t.Fatalf("ProjectFinished() error = %v, want %v", err, writeErr)
	}
	if !strings.Contains(err.Error(), "write stderr") {
		t.Fatalf("ProjectFinished() error = %q, want stderr destination", err)
	}
}

func TestConsoleReporterReturnsSeparatorWriteFailures(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("broken pipe")
	tests := []struct {
		name   string
		report func(*consoleReporter) error
	}{
		{
			name: "project separator",
			report: func(reporter *consoleReporter) error {
				return reporter.ProjectStarted(updater.ProjectRef{Name: "next"})
			},
		},
		{
			name: "prune separator",
			report: func(reporter *consoleReporter) error {
				return reporter.PruneStarted()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reporter := &consoleReporter{
				stdout:  failingWriter{err: writeErr},
				started: true,
			}
			if err := test.report(reporter); !errors.Is(err, writeErr) {
				t.Fatalf("report error = %v, want %v", err, writeErr)
			}
		})
	}
}

func TestRunCommandReportsRunAndCloseFailures(t *testing.T) {
	t.Parallel()

	pullErr := errors.New("registry failed")
	closeErr := errors.New("close failed")
	backend := &commandBackend{
		projects: []updater.ProjectRef{{Name: "app", Services: []string{"web"}}},
		pullErr:  pullErr,
		closeErr: closeErr,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runCommand(context.Background(), &stdout, &stderr, func() (backendCloser, error) {
		return backend, nil
	})

	if code != 1 {
		t.Fatalf("runCommand() = %d, want 1", code)
	}
	for _, substring := range []string{"project \"app\": pull: registry failed", "close Docker client: close failed"} {
		if !strings.Contains(stderr.String(), substring) {
			t.Errorf("stderr %q does not contain %q", stderr.String(), substring)
		}
	}
	if backend.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", backend.closeCalls)
	}
}

type commandBackend struct {
	projects    []updater.ProjectRef
	discoverErr error
	pullErr     error
	pruneErr    error
	closeErr    error
	closeCalls  int
	openCalls   int
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (b *commandBackend) DiscoverProjects(context.Context) ([]updater.ProjectRef, error) {
	return b.projects, b.discoverErr
}

func (b *commandBackend) OpenProject(context.Context, updater.ProjectRef) (updater.ProjectSession, error) {
	b.openCalls++
	return commandProjectSession{backend: b}, nil
}

type commandProjectSession struct {
	backend *commandBackend
}

func (s commandProjectSession) Pull(context.Context) error {
	return s.backend.pullErr
}

func (commandProjectSession) Up(context.Context) error {
	return nil
}

func (b *commandBackend) PruneImages(context.Context) error {
	return b.pruneErr
}

func (b *commandBackend) Close() error {
	b.closeCalls++
	return b.closeErr
}
