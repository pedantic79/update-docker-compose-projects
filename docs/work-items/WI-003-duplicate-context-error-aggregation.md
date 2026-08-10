# WI-003: Duplicate Context Error Aggregation

- **File**: [internal/updater/updater.go](../../internal/updater/updater.go#L129)
- **Severity**: Warning
- **Impact**: When context cancellation occurs during project Pull or Up operations, the cancellation error is appended to `runErrors` twice (once wrapped in `projectResult.Err` and once directly as `ctx.Err()`), producing duplicate context cancellation error entries in `errors.Join(runErrors...)`.

## Description
In `session.Pull` and `session.Up` error handling in `internal/updater/updater.go` (lines 129-151), when a backend operation observes context cancellation, `projectResult.Err` is constructed wrapping the operation error (which wraps `ctx.Err()` via `%w`) and appended to `runErrors`. Immediately after, `if ctx.Err() != nil` appends `ctx.Err()` directly to `runErrors` a second time before breaking out of the loop. Because `projectResult.Err` already wraps `ctx.Err()`, appending `ctx.Err()` directly results in duplicate context cancellation error nodes in the aggregated multierror returned by `errors.Join(runErrors...)`.

## Proposed Fix
In `internal/updater/updater.go`:
1. Remove `runErrors = append(runErrors, ctx.Err())` from `session.Pull` error handling.
2. Remove `runErrors = append(runErrors, ctx.Err())` from `session.Up` error handling.
3. Add `if ctx.Err() != nil { break }` in `OpenProject` error handling to consistently break on context cancellation.

```diff
--- a/internal/updater/updater.go
+++ b/internal/updater/updater.go
@@ -120,6 +120,9 @@ func (u *Updater) Run(ctx context.Context) (RunResult, error) {
 			projectResult.Status = ProjectFailed
 			u.finishProject(&result, projectResult)
 			runErrors = append(runErrors, projectResult.Err)
+			if ctx.Err() != nil {
+				break
+			}
 			continue
 		}
 
@@ -131,7 +134,6 @@ func (u *Updater) Run(ctx context.Context) (RunResult, error) {
 			projectResult.Status = ProjectFailed
 			u.finishProject(&result, projectResult)
 			runErrors = append(runErrors, projectResult.Err)
 			if ctx.Err() != nil {
-				runErrors = append(runErrors, ctx.Err())
 				break
 			}
 			continue
@@ -143,7 +145,6 @@ func (u *Updater) Run(ctx context.Context) (RunResult, error) {
 			projectResult.Status = ProjectFailed
 			u.finishProject(&result, projectResult)
 			runErrors = append(runErrors, projectResult.Err)
 			if ctx.Err() != nil {
-				runErrors = append(runErrors, ctx.Err())
 				break
 			}
 			continue
```

## Acceptance Plan
1. Unit Test Verification:
   - In `internal/updater/updater_test.go`, update `TestRunStopsWhenBackendOperationObservesCancellation` or add a specific test verifying the joined error structure upon context cancellation during Pull, Up, or OpenProject.
   - Assert that `errors.Is(err, context.Canceled)` returns true.
   - Assert that unwrapping the returned joined error (e.g. via interface `{ Unwrap() []error }`) contains exactly one error item (`projectResult.Err`) rather than duplicate `context.Canceled` errors.
2. Regression Testing:
   - Execute `go test ./...` to verify all updater tests pass.
   - Ensure cancellation behavior (halting further projects, skipping image pruning) remains completely intact.

## Action Items
- [ ] Apply the proposed fix to the codebase.
- [ ] Verify the fix using the acceptance plan.
