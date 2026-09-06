# WI-004: Consolidate project convergence and cancellation handling

- **File**: [internal/updater/updater.go](../../internal/updater/updater.go#L89)
- **Severity**: Suggestion
- **Status**: Complete
- **Governing decision**:
  [ADR 0001](../adr/adr-0001-project-scoped-progress-output.md)
- **Impact**: `OpenProject`, `Pull`, and `Up` repeat the same project-result,
  error-aggregation, and cancellation branches. The behavior is covered, but
  the duplication makes later policy changes easy to apply inconsistently.

## Current State

`Updater.Run` owns two kinds of work that are currently interleaved:

1. Batch policy: discovery, eligibility, reporting, failure isolation,
   cancellation scheduling, error aggregation, and final pruning.
2. One project's ordered convergence operations: `OpenProject`, `Pull`, then
   `Up`.

For each operation failure, `Run` repeats the same sequence: wrap the operation
error with the project name, mark the result failed, finish and report the
project, append the error, then break on cancellation or continue with the next
project. There is no known user-visible defect in this logic; this work item is
a maintainability refactor.

The operation paths are not completely interchangeable. Once `OpenProject`
succeeds, `Pull` is attempted and final pruning must be scheduled even if that
pull fails. Compose pulls are not atomic, so a failed pull may already have
updated images or downloaded layers.

This work item is subordinate to ADR 0001. It may reduce duplication inside
`Updater.Run`, but it must not change the accepted project-scoped progress
model or any behavior that supports it.

## ADR 0001 Compliance Requirements

| ADR 0001 decision | WI-004 requirement |
|---|---|
| Progress is synchronous and project-scoped. | Keep the project loop sequential. Do not add goroutines, buffer project lifecycle events, or begin the next project before the current helper returns and its result is reported. |
| The heading appears before project work. | Keep `ProjectStarted(ref)` in `Run` before eligibility handling and before `convergeProject`. Do not move it into the helper or defer it until convergence finishes. |
| Pull finishes before `up` begins. | Call `session.Pull(ctx)` and wait for it to return before calling `session.Up(ctx)`. A pull failure must prevent `Up` for that project. |
| One fresh progress session belongs to each project and is shared by its pull and `up`. | Call `OpenProject` exactly once for each eligible project. Keep its returned `ProjectSession` local to `convergeProject`, use that same value for both operations, and never cache or reuse it for another project. Do not create separate pull and `up` sessions. |
| All sessions use one Docker context and daemon. | Use the existing `u.backend`; do not construct a Docker client, Compose service, backend, or alternate context in the updater or helper. Session creation remains behind `Backend.OpenProject`. |
| Blank separators and terminal-aware colors are part of the output contract. | Do not change `Reporter`, `consoleReporter`, stream selection, color detection, or lifecycle-event order. The existing reporter remains responsible for separators and colors. |
| Pruning is one separate final section. | Keep pruning outside the project loop and helper. A pull attempt may schedule cleanup, but at most one prune may run after all safe project work and only with a live context. |
| Project failures are isolated and aggregated. | Keep result construction, `ProjectFinished`, error aggregation, and cancellation scheduling in `Run`; ordinary failures continue to the next project. |
| Display status does not control eligibility. | Continue to call `ProjectRef.Eligible`, which uses typed running-service state. Do not inspect or parse `ProjectRef.Status`. |
| Compose owns stale-container convergence. | Continue to call `Up` for every eligible project whose pull succeeds. Do not add mutable-tag comparisons, image-change gates, or forced recreation. Compose option policy remains in `internal/docker`. |

## Scope

Extract only the ordered `OpenProject`/`Pull`/`Up` sequence. Keep the following
responsibilities in `Run`:

- deciding whether a discovered project is eligible;
- emitting `ProjectStarted` and exactly one `ProjectFinished` event;
- constructing and appending `ProjectResult` values;
- isolating ordinary project failures so later projects still run;
- stopping new project work after context cancellation;
- deduplicating a context error already wrapped by a project error; and
- performing at most one final image prune when the context is still live.

Do not change the `Backend` or `ProjectSession` interfaces, Compose service
selection, Docker-context ownership, reporter behavior, retry behavior, or
prune policy. Do not move session construction out of `Backend.OpenProject`.

## Required Helper Contract

Add a helper whose boolean means `pullAttempted`, not pull success:

| Result | `pullAttempted` | Error |
|---|---:|---|
| `OpenProject` fails | `false` | wraps the failure as `open: ...` |
| `Pull` fails | `true` | wraps the failure as `pull: ...` |
| `Up` fails | `true` | wraps the failure as `up: ...` |
| All operations succeed | `true` | `nil` |

```go
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
```

Every return after the `Pull` invocation reports `true`. Do not add an
assignment such as `pullAttempted = true` when all following returns already
provide `true`; the repository's `ineffassign` lint check rejects that form.

## Proposed `Run` Integration

Replace the three inline operation/error branches with one convergence result
path:

```go
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
		u.finishProject(&result, projectResult)

		if ctx.Err() != nil {
			break
		}
	}
```

The helper call is synchronous. It completes the current project's pull and
`up` displays before `finishProject` runs and before the loop can emit the next
project heading. The cancellation check remains after the project is recorded
so an operation that observes cancellation still produces exactly one failed
project result. No additional context checks should be inserted between
`OpenProject`, `Pull`, and `Up` as part of this behavior-preserving refactor.

Leave the post-loop cancellation/prune block unchanged. Its `errors.Is` check
prevents a standalone context error from being appended when the recorded
project error already wraps it. If cancellation occurs after successful
convergence, the block adds the context error and skips pruning.

## Acceptance Plan

1. Add an updater test with two eligible projects and one shared event log for
   the reporter and backend. Assert the complete synchronous order:
   `start:first`, `open:first`, `pull:first`, `up:first`, `finish:first`, then
   the same sequence for `second`, followed by the separate prune lifecycle.
   No event from the second project may appear before the first finishes.
2. Preserve `TestBackendUsesOneFreshProgressSessionPerProject`. It must continue
   proving that each project gets a distinct Compose session while each
   project's pull and `up` use the same session and loaded project.
3. Preserve heading order, blank separators, and the separate final prune
   section in `TestRunCommandRendersSuccessfulRunWithoutDocker`. Preserve color
   and non-color output behavior in `TestConsoleReporterUsesColorForVisualHierarchy`,
   `TestConsoleReporterStderrColorMismatch`, and the `supportsColor` tests.
4. Preserve typed eligibility, exact backend call order, reporter event order,
   and once-per-run pruning in
   `TestProjectRefEligible` and
   `TestRunConvergesEligibleProjectsAndSkipsStoppedProjects`.
5. Preserve exact running-service selection and Compose-owned diverged
   convergence in the Docker backend tests. No updater change may introduce an
   image-change gate, force recreation, a second Docker client, or a different
   context.
6. Preserve per-project failure isolation, operation-specific error text, and
   joined error identity in `TestRunIsolatesAndAggregatesProjectFailures`.
7. Preserve pruning after failed pull attempts in
   `TestRunPrunesOnceWhenEveryPullFails`, and preserve joined pull/prune errors
   in `TestRunJoinsPullAndPruneFailures`.
8. Preserve retry behavior and the unconditional pull/`up` convergence path
   after an earlier `Up` failure in
   `TestRunRetriesConvergenceAfterUpFailure`.
9. Preserve cancellation before discovery, between projects, after successful
   convergence, and when a backend operation observes cancellation. In every
   canceled case, no prune may begin.
10. Preserve the single joined cancellation entry asserted by
   `TestRunAggregatesObservedCancellationOnce`.
11. Run `just fmt`, `just check`, and `just coverage`; total statement coverage
   must remain above 95%.

## Action Items

- [x] Add `convergeProject` with the contract above.
- [x] Replace the three inline failure branches with one result-handling path.
- [x] Preserve every ADR 0001 constraint in the compliance table.
- [x] Add the two-project synchronous lifecycle-order test.
- [x] Keep batch policy and the post-loop cancellation/prune block in `Run`.
- [x] Verify project-session freshness and sharing at the Docker boundary.
- [x] Verify all existing output, failure, cancellation, reporter, retry, service-selection, and pruning tests.
- [x] Complete the repository formatting, lint, race, and coverage checks.
