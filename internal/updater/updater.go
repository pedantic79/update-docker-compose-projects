package updater

import (
	"context"
	"errors"
	"fmt"
)

// ProjectRef contains display state and, for eligible projects, the launch
// metadata needed to reconstruct their running services. Status is
// display-only; eligibility uses typed service state captured during discovery.
type ProjectRef struct {
	Name        string
	Status      string
	ConfigPaths []string
	WorkingDir  string
	EnvFiles    []string
	Services    []string
}

// Eligible reports whether the project has at least one running service to
// converge. Intentionally stopped services are not selected for update.
func (p ProjectRef) Eligible() bool {
	return len(p.Services) > 0
}

// Backend is the narrow boundary between update policy and Docker/Compose.
// Unit tests implement this interface without constructing a Docker client.
type Backend interface {
	DiscoverProjects(context.Context) ([]ProjectRef, error)
	OpenProject(context.Context, ProjectRef) (ProjectSession, error)
	PruneImages(context.Context) error
}

// ProjectSession scopes the mutable Compose progress state to one project.
// Pull and Up deliberately share a session, while the next project receives a
// fresh one.
type ProjectSession interface {
	Pull(context.Context) error
	Up(context.Context) error
}

// Reporter receives lifecycle events synchronously. A ProjectStarted failure
// prevents new project work. Prune notifications cannot suppress cleanup that
// is already required by an attempted pull.
type Reporter interface {
	ProjectStarted(ProjectRef) error
	ProjectFinished(ProjectResult) error
	PruneStarted() error
	PruneFinished(error) error
}

type ProjectStatus string

const (
	ProjectConverged ProjectStatus = "converged"
	ProjectSkipped   ProjectStatus = "skipped"
	ProjectFailed    ProjectStatus = "failed"
)

type ProjectResult struct {
	Name   string
	Status ProjectStatus
	Reason string
	Err    error
}

type RunResult struct {
	Projects       []ProjectResult
	PruneAttempted bool
	Pruned         bool
}

type Updater struct {
	backend  Backend
	reporter Reporter
}

func New(backend Backend, reporter Reporter) *Updater {
	if reporter == nil {
		reporter = discardReporter{}
	}
	return &Updater{backend: backend, reporter: reporter}
}

// Run discovers the complete batch before making changes, isolates failures
// to individual projects, and returns all failures as an errors.Join tree.
func (u *Updater) Run(ctx context.Context) (RunResult, error) {
	var result RunResult
	if err := ctx.Err(); err != nil {
		return result, err
	}

	projects, err := u.backend.DiscoverProjects(ctx)
	if err != nil {
		return result, fmt.Errorf("discover projects: %w", err)
	}

	var runErrors []error
	needsPrune := false
	for _, ref := range projects {
		if err := ctx.Err(); err != nil {
			runErrors = append(runErrors, err)
			break
		}

		if err := u.reporter.ProjectStarted(ref); err != nil {
			runErrors = append(runErrors, fmt.Errorf("report project %q started: %w", ref.Name, err))
			break
		}

		projectResult := ProjectResult{Name: ref.Name}
		if !ref.Eligible() {
			projectResult.Status = ProjectSkipped
			projectResult.Reason = "no running services"
			if err := u.finishProject(&result, projectResult); err != nil {
				runErrors = append(runErrors, err)
				break
			}
			continue
		}

		pullAttempted, projectErr := u.convergeProject(ctx, ref)
		if pullAttempted {
			// A pull may update some service images before another service fails.
			// Conservatively schedule one final cleanup for every pull attempt.
			needsPrune = true
		}

		if projectErr != nil {
			projectResult.Status = ProjectFailed
			projectResult.Err = fmt.Errorf("project %q: %w", ref.Name, projectErr)
			runErrors = append(runErrors, projectResult.Err)
		} else {
			projectResult.Status = ProjectConverged
		}
		if err := u.finishProject(&result, projectResult); err != nil {
			runErrors = append(runErrors, err)
			break
		}

		if ctx.Err() != nil {
			break
		}
	}

	// Do not begin another mutation after cancellation. A later invocation can
	// perform cleanup using a live context.
	if err := ctx.Err(); err != nil {
		if !errors.Is(errors.Join(runErrors...), err) {
			runErrors = append(runErrors, err)
		}
	} else if needsPrune {
		if err := u.reporter.PruneStarted(); err != nil {
			runErrors = append(runErrors, fmt.Errorf("report prune started: %w", err))
		}

		result.PruneAttempted = true
		pruneErr := u.backend.PruneImages(ctx)
		if err := u.reporter.PruneFinished(pruneErr); err != nil {
			runErrors = append(runErrors, fmt.Errorf("report prune finished: %w", err))
		}
		if pruneErr != nil {
			runErrors = append(runErrors, fmt.Errorf("prune images: %w", pruneErr))
		} else {
			result.Pruned = true
		}
	}

	return result, errors.Join(runErrors...)
}

func (u *Updater) convergeProject(
	ctx context.Context,
	ref ProjectRef,
) (pullAttempted bool, err error) {
	session, err := u.backend.OpenProject(ctx, ref)
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}

	if err := session.Pull(ctx); err != nil {
		return true, fmt.Errorf("pull: %w", err)
	}

	if err := session.Up(ctx); err != nil {
		return true, fmt.Errorf("up: %w", err)
	}

	return true, nil
}

func (u *Updater) finishProject(result *RunResult, project ProjectResult) error {
	result.Projects = append(result.Projects, project)
	if err := u.reporter.ProjectFinished(project); err != nil {
		return fmt.Errorf("report project %q finished: %w", project.Name, err)
	}
	return nil
}

type discardReporter struct{}

func (discardReporter) ProjectStarted(ProjectRef) error     { return nil }
func (discardReporter) ProjectFinished(ProjectResult) error { return nil }
func (discardReporter) PruneStarted() error                 { return nil }
func (discardReporter) PruneFinished(error) error           { return nil }
