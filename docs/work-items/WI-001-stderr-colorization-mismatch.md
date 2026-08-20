# WI-001: Stderr colorization mismatch when stdout and stderr have different color support

- **File**: [main.go](../../main.go#L61)
- **Severity**: Warning
- **Impact**: When stdout is attached to a TTY but stderr is redirected to a file or pipe, ANSI color escape sequences are written to stderr, corrupting log files with ANSI characters.

## Description
In `main.go`, `newConsoleReporter` checks color support only on `stdout` (`supportsColor(stdout)`) and stores a single `colorOutput` boolean flag on `consoleReporter`. When `ProjectFinished` prints skip notifications to `r.stderr`, it uses `r.colorize(...)`, which checks `r.colorOutput`. Because color output capability is determined exclusively from `stdout`, `stderr` output inherits `stdout`'s color preference regardless of whether `stderr` is actually connected to a terminal.

## Proposed Fix
Update `consoleReporter` struct to maintain separate color settings (`stdoutColor` and `stderrColor`) for `stdout` and `stderr`. Initialize both in `newConsoleReporter` via `supportsColor(stdout)` and `supportsColor(stderr)`. Modify `r.colorize` to take a boolean parameter indicating whether color formatting is enabled for that target stream.

```diff
--- a/main.go
+++ b/main.go
@@ -55,16 +55,18 @@ type consoleReporter struct {
 	stdout      io.Writer
 	stderr      io.Writer
 	started     bool
-	colorOutput bool
+	stdoutColor bool
+	stderrColor bool
 }
 
 func newConsoleReporter(stdout, stderr io.Writer) *consoleReporter {
 	return &consoleReporter{
 		stdout:      stdout,
 		stderr:      stderr,
-		colorOutput: supportsColor(stdout),
+		stdoutColor: supportsColor(stdout),
+		stderrColor: supportsColor(stderr),
 	}
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
-		r.colorize("31", project.Name),
-		r.colorize("34", status),
+		r.colorize(r.stdoutColor, "31", project.Name),
+		r.colorize(r.stdoutColor, "34", status),
 	)
 }
 
 func (r *consoleReporter) ProjectFinished(project updater.ProjectResult) {
 	if project.Status == updater.ProjectSkipped {
 		fmt.Fprintf(
 			r.stderr,
 			"skipping %s: %s\n",
-			r.colorize("31", project.Name),
+			r.colorize(r.stderrColor, "31", project.Name),
 			project.Reason,
 		)
 	}
 }
 
 func (r *consoleReporter) PruneStarted() {
 	if r.started {
 		fmt.Fprintln(r.stdout)
 	}
-	fmt.Fprintln(r.stdout, r.colorize("31", "Pruning images..."))
+	fmt.Fprintln(r.stdout, r.colorize(r.stdoutColor, "31", "Pruning images..."))
 }
 
 func (r *consoleReporter) PruneFinished(err error) {
 	if err == nil {
 		fmt.Fprintln(r.stdout, "Pruned unused images.")
 	}
 }
 
-func (r *consoleReporter) colorize(code, value string) string {
-	if !r.colorOutput {
+func (r *consoleReporter) colorize(enabled bool, code, value string) string {
+	if !enabled {
 		return value
 	}
 	return "\x1b[" + code + "m" + value + "\x1b[0m"
 }
```

## Acceptance Plan
1. Add unit test `TestConsoleReporterStderrColorMismatch` in `main_test.go` verifying that when stdout is color-enabled and stderr is color-disabled, `ProjectFinished` output to stderr contains no ANSI escape codes (`\x1b[31m`).
2. Execute `go test ./...` to verify all unit tests pass clean.

## Action Items
- [x] Apply the proposed fix to the codebase.
- [x] Verify the fix using the acceptance plan.
