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

func TestConsoleReporterUsesColorForVisualHierarchy(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	reporter := &consoleReporter{
		stdout:      &stdout,
		stderr:      &stderr,
		colorOutput: true,
	}

	reporter.ProjectStarted(updater.ProjectRef{Name: "app", Status: "running(1)"})
	reporter.ProjectFinished(updater.ProjectResult{
		Name:   "app",
		Status: updater.ProjectSkipped,
		Reason: "no running services",
	})
	reporter.PruneStarted()
	reporter.PruneFinished(nil)

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
}

func (b *commandBackend) DiscoverProjects(context.Context) ([]updater.ProjectRef, error) {
	return b.projects, b.discoverErr
}

func (b *commandBackend) OpenProject(context.Context, updater.ProjectRef) (updater.ProjectSession, error) {
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
