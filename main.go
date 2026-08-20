package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	dockerbackend "github.com/pedantic79/update-docker-compose-projects/internal/docker"
	"github.com/pedantic79/update-docker-compose-projects/internal/updater"
	"golang.org/x/term"
)

type backendCloser interface {
	updater.Backend
	Close() error
}

type backendFactory func() (backendCloser, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runCommand(ctx, os.Stdout, os.Stderr, func() (backendCloser, error) {
		return dockerbackend.New()
	})
	stop()
	os.Exit(code)
}

func runCommand(ctx context.Context, stdout, stderr io.Writer, factory backendFactory) int {
	backend, err := factory()
	if err != nil {
		fmt.Fprintf(stderr, "initialize: %v\n", err)
		return 1
	}

	reporter := newConsoleReporter(stdout, stderr)
	_, runErr := updater.New(backend, reporter).Run(ctx)

	closeErr := backend.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close Docker client: %w", closeErr)
	}
	if err := errors.Join(runErr, closeErr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

type consoleReporter struct {
	stdout      io.Writer
	stderr      io.Writer
	started     bool
	stdoutColor bool
	stderrColor bool
}

func newConsoleReporter(stdout, stderr io.Writer) *consoleReporter {
	return &consoleReporter{
		stdout:      stdout,
		stderr:      stderr,
		stdoutColor: supportsColor(stdout),
		stderrColor: supportsColor(stderr),
	}
}

func supportsColor(writer io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := writer.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (r *consoleReporter) ProjectStarted(project updater.ProjectRef) {
	if r.started {
		fmt.Fprintln(r.stdout)
	}
	r.started = true
	status := project.Status
	if status == "" {
		status = "unknown"
	}
	fmt.Fprintf(
		r.stdout,
		"Name:%s, Status:%s\n",
		r.colorize(r.stdoutColor, "31", project.Name),
		r.colorize(r.stdoutColor, "34", status),
	)
}

func (r *consoleReporter) ProjectFinished(project updater.ProjectResult) {
	if project.Status == updater.ProjectSkipped {
		fmt.Fprintf(
			r.stderr,
			"skipping %s: %s\n",
			r.colorize(r.stderrColor, "31", project.Name),
			project.Reason,
		)
	}
}

func (r *consoleReporter) PruneStarted() {
	if r.started {
		fmt.Fprintln(r.stdout)
	}
	fmt.Fprintln(r.stdout, r.colorize(r.stdoutColor, "31", "Pruning images..."))
}

func (r *consoleReporter) PruneFinished(err error) {
	if err == nil {
		fmt.Fprintln(r.stdout, "Pruned unused images.")
	}
}

func (r *consoleReporter) colorize(enabled bool, code, value string) string {
	if !enabled {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
