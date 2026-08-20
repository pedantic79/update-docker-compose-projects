package updater

import (
	"context"
	"errors"
	"fmt"
)

// ProjectRef contains the launch metadata needed to reconstruct a running
// Compose project. Status is display-only; eligibility uses typed service
// state captured during discovery.
type ProjectRef struct {
	Name            string
	Status          string
	ConfigPaths     []string
	WorkingDir      string
	EnvFiles        []string
	Services        []string
	StoppedServices []string
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

// Reporter receives lifecycle events synchronously. A console reporter can
// therefore print a project header before Docker emits progress for it.
type Reporter interface {
	ProjectStarted(ProjectRef)
	ProjectFinished(ProjectResult)
	PruneStarted()
	PruneFinished(error)
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

func New(backend Backend, reporters ...Reporter) *Updater {
	reporter := Reporter(discardReporter{})
	if len(reporters) > 0 && reporters[0] != nil {
		reporter = reporters[0]
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

		projectResult := ProjectResult{Name: ref.Name}
		u.reporter.ProjectStarted(ref)
		if !ref.Eligible() {
			projectResult.Status = ProjectSkipped
			projectResult.Reason = "no running services"
			u.finishProject(&result, projectResult)
			continue
		}

		session, err := u.backend.OpenProject(ctx, ref)
		if err != nil {
			projectResult.Err = fmt.Errorf("project %q: open: %w", ref.Name, err)
			projectResult.Status = ProjectFailed
			u.finishProject(&result, projectResult)
			runErrors = append(runErrors, projectResult.Err)
			continue
		}

		// A pull may update some service images before another service fails.
		// Conservatively schedule one final cleanup for every pull attempt.
		needsPrune = true
		if err := session.Pull(ctx); err != nil {
			projectResult.Err = fmt.Errorf("project %q: pull: %w", ref.Name, err)
			projectResult.Status = ProjectFailed
			u.finishProject(&result, projectResult)
			runErrors = append(runErrors, projectResult.Err)
			if ctx.Err() != nil {
				runErrors = append(runErrors, ctx.Err())
				break
			}
			continue
		}

		if err := session.Up(ctx); err != nil {
			projectResult.Err = fmt.Errorf("project %q: up: %w", ref.Name, err)
			projectResult.Status = ProjectFailed
			u.finishProject(&result, projectResult)
			runErrors = append(runErrors, projectResult.Err)
			if ctx.Err() != nil {
				runErrors = append(runErrors, ctx.Err())
				break
			}
			continue
		}

		projectResult.Status = ProjectConverged
		u.finishProject(&result, projectResult)
	}

	// Do not begin another mutation after cancellation. A later invocation can
	// perform cleanup using a live context.
	if err := ctx.Err(); err != nil {
		runErrors = append(runErrors, err)
	} else if needsPrune {
		result.PruneAttempted = true
		u.reporter.PruneStarted()
		pruneErr := u.backend.PruneImages(ctx)
		u.reporter.PruneFinished(pruneErr)
		if pruneErr != nil {
			runErrors = append(runErrors, fmt.Errorf("prune images: %w", pruneErr))
		} else {
			result.Pruned = true
		}
	}

	return result, errors.Join(runErrors...)
}

func (u *Updater) finishProject(result *RunResult, project ProjectResult) {
	result.Projects = append(result.Projects, project)
	u.reporter.ProjectFinished(project)
}

type discardReporter struct{}

func (discardReporter) ProjectStarted(ProjectRef)     {}
func (discardReporter) ProjectFinished(ProjectResult) {}
func (discardReporter) PruneStarted()                 {}
func (discardReporter) PruneFinished(error)           {}
