# WI-004: Consolidate project convergence and cancellation handling

- **File**: [internal/updater/updater.go](../../internal/updater/updater.go#L102)
- **Severity**: Warning
- **Impact**: Open, pull, and up failures repeat project result recording and cancellation branching. Keeping those paths separate makes future changes to reporting, aggregation, or cancellation easy to apply inconsistently.

## Description

`Updater.Run` currently performs `OpenProject`, `Pull`, and `Up` inline. Each
failure path constructs a step-specific error, marks and reports the project as
failed, aggregates the error, checks cancellation, and either breaks or
continues. The steps have one important difference that must survive the
refactor: a successful open means a pull will be attempted, so final pruning is
required even when that pull fails partway through.

Extract only the ordered open/pull/up operation sequence. Keep batch policy in
`Run`: eligibility, project result recording, per-project failure isolation,
cancellation scheduling, and final pruning remain responsibilities of the
batch runner.

## Proposed Fix

Add a helper whose boolean explicitly means `pullAttempted`, not pull success:

```go
func (u *Updater) convergeProject(
	ctx context.Context,
	ref ProjectRef,
) (pullAttempted bool, err error) {
	session, err := u.backend.OpenProject(ctx, ref)
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}

	pullAttempted = true
	if err := session.Pull(ctx); err != nil {
		return true, fmt.Errorf("pull: %w", err)
	}

	if err := session.Up(ctx); err != nil {
		return true, fmt.Errorf("up: %w", err)
	}

	return true, nil
}
```

Replace the three inline operation/error blocks in `Run` with one convergence
result path:

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

Leave the post-loop cancellation/prune block unchanged. Its `errors.Is` check
is still required to avoid appending a standalone context error when the
project error already wraps it:

```go
	if err := ctx.Err(); err != nil {
		if !errors.Is(errors.Join(runErrors...), err) {
			runErrors = append(runErrors, err)
		}
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
```

## Acceptance Plan

1. Preserve the exact call and reporter event order asserted by
   `TestRunConvergesEligibleProjectsAndSkipsStoppedProjects`.
2. Preserve failure isolation and step-specific error text for open, pull, and
   up failures in `TestRunIsolatesAndAggregatesProjectFailures`.
3. Preserve pruning after every pull attempt, including failed pulls, in
   `TestRunPrunesOnceWhenEveryPullFails`.
4. Preserve immediate cancellation stop behavior and skipped pruning in
   `TestRunStopsSchedulingAndSkipsPruneAfterCancellation` and
   `TestRunStopsWhenBackendOperationObservesCancellation`.
5. Preserve a single joined cancellation error in
   `TestRunAggregatesObservedCancellationOnce`.
6. Run `just fmt`, `just check`, and `just coverage`; total statement coverage
   must remain above 95%.

## Action Items

- [ ] Add `convergeProject` with the concrete contract above.
- [ ] Replace the three inline operation failure paths with unified result handling.
- [ ] Verify all failure, cancellation, reporter, and pruning invariants.
- [ ] Complete the acceptance plan.
