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
		_, _ = fmt.Fprintf(stderr, "initialize: %v\n", err)
		return 1
	}

	reporter := newConsoleReporter(stdout, stderr)
	_, runErr := updater.New(backend, reporter).Run(ctx)

	closeErr := backend.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close Docker client: %w", closeErr)
	}
	if err := errors.Join(runErr, closeErr); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

const (
	ansiRed  = "31"
	ansiBlue = "34"
)

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

func (r *consoleReporter) ProjectStarted(project updater.ProjectRef) error {
	if r.started {
		if err := r.outf("\n"); err != nil {
			return err
		}
	}
	r.started = true
	status := project.Status
	if status == "" {
		status = "unknown"
	}
	return r.outf(
		"Name:%s, Status:%s\n",
		colorize(r.stdoutColor, ansiRed, project.Name),
		colorize(r.stdoutColor, ansiBlue, status),
	)
}

func (r *consoleReporter) ProjectFinished(project updater.ProjectResult) error {
	if project.Status == updater.ProjectSkipped {
		return r.errf(
			"skipping %s: %s\n",
			colorize(r.stderrColor, ansiRed, project.Name),
			project.Reason,
		)
	}
	return nil
}

func (r *consoleReporter) PruneStarted() error {
	if r.started {
		if err := r.outf("\n"); err != nil {
			return err
		}
	}
	return r.outf("%s\n", colorize(r.stdoutColor, ansiRed, "Pruning images..."))
}

func (r *consoleReporter) PruneFinished(err error) error {
	if err == nil {
		return r.outf("Pruned unused images.\n")
	}
	return nil
}

func (r *consoleReporter) outf(format string, args ...any) error {
	return writef(r.stdout, "stdout", format, args...)
}

func (r *consoleReporter) errf(format string, args ...any) error {
	return writef(r.stderr, "stderr", format, args...)
}

func writef(writer io.Writer, destination, format string, args ...any) error {
	if _, err := fmt.Fprintf(writer, format, args...); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}
	return nil
}

func colorize(enabled bool, code, value string) string {
	if !enabled {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
