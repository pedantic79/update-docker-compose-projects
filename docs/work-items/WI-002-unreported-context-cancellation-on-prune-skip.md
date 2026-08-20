# WI-002: Unreported context cancellation when image pruning is skipped

- **File**: [internal/updater/updater.go](../../internal/updater/updater.go#L159)
- **Severity**: Warning
- **Impact**: If context is canceled after project convergence but before image pruning, pruning is skipped but `Run()` returns a `nil` error. Callers are falsely informed that the updater run completed successfully without error.

## Description
In `internal/updater/updater.go`, when `needsPrune` is set to true during project processing, line 159 checks `if needsPrune && ctx.Err() == nil` before performing image pruning. If the context was canceled after all projects completed convergence, `ctx.Err()` is non-nil, causing pruning to be skipped. However, `ctx.Err()` is never added to `runErrors`. If all preceding projects converged without error, `runErrors` is empty, causing `Run()` to return a `nil` error despite pruning being skipped due to context cancellation.

## Proposed Fix
Replace the condition `if needsPrune && ctx.Err() == nil` with an explicit check for context cancellation: `if err := ctx.Err(); err != nil { runErrors = append(runErrors, err) } else if needsPrune { ... }`. This ensures context cancellation is recorded in `runErrors` while still skipping pruning.

```diff
--- a/internal/updater/updater.go
+++ b/internal/updater/updater.go
@@ -159,12 +159,14 @@ func (u *Updater) Run(ctx context.Context) (RunResult, error) {
-	if needsPrune && ctx.Err() == nil {
-		result.PruneAttempted = true
-		u.reporter.PruneStarted()
-		pruneErr := u.backend.PruneImages(ctx)
-		u.reporter.PruneFinished(pruneErr)
-		if pruneErr != nil {
-			runErrors = append(runErrors, fmt.Errorf("prune images: %w", pruneErr))
-		} else {
-			result.Pruned = true
-		}
-	}
+	if err := ctx.Err(); err != nil {
+		runErrors = append(runErrors, err)
+	} else if needsPrune {
+		result.PruneAttempted = true
+		u.reporter.PruneStarted()
+		pruneErr := u.backend.PruneImages(ctx)
+		u.reporter.PruneFinished(pruneErr)
+		if pruneErr != nil {
+			runErrors = append(runErrors, fmt.Errorf("prune images: %w", pruneErr))
+		} else {
+			result.Pruned = true
+		}
+	}
```

## Acceptance Plan
1. Add a unit test `TestRunReportsCancellationWhenPruneSkippedAfterConvergence` in `internal/updater/updater_test.go` where context is canceled right after projects converge. Verify that `result.PruneAttempted` is false and `err` is non-nil (`errors.Is(err, context.Canceled) == true`).
2. Run `go test ./internal/updater/...` to ensure all existing cancellation and pruning tests pass without regression.

## Action Items
- [x] Apply the proposed fix to the codebase.
- [x] Verify the fix using the acceptance plan.
