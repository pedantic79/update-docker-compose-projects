# WI-005: Simplify terminal diagnostic write handling

- **File**: [main.go](../../main.go#L33)
- **Severity**: Suggestion
- **Impact**: Two conditionals inspect final `stderr` write errors even though both outcomes return the same exit status. The branches add statements without changing command behavior.

## Description

When backend initialization fails, and when the command has a final joined
error, `runCommand` prints one last diagnostic and returns status 1. If that
diagnostic write also fails, there is no remaining output channel and the
command still returns status 1. The write should therefore be explicitly
best-effort instead of branching on an outcome that cannot affect the result.

This change applies only to terminal diagnostics immediately before returning.
Reporter writes must continue to be captured in `consoleReporter.err`, because
those failures influence the final command result before the terminal
diagnostic is attempted.

## Proposed Fix

```diff
--- a/main.go
+++ b/main.go
@@
 	backend, err := factory()
 	if err != nil {
-		if _, writeErr := fmt.Fprintf(stderr, "initialize: %v\n", err); writeErr != nil {
-			return 1
-		}
+		_, _ = fmt.Fprintf(stderr, "initialize: %v\n", err)
 		return 1
 	}
@@
 	}
 	if err := errors.Join(runErr, closeErr, reporter.err); err != nil {
-		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
-			return 1
-		}
+		_, _ = fmt.Fprintln(stderr, err)
 		return 1
 	}
```

Do not change reporter write handling:

```go
func (r *consoleReporter) writef(writer io.Writer, destination, format string, args ...any) {
	if _, err := fmt.Fprintf(writer, format, args...); err != nil && r.err == nil {
		r.err = fmt.Errorf("write %s: %w", destination, err)
	}
}
```

## Acceptance Plan

1. `TestRunCommandReportsInitializationFailure` must still receive exit status 1
   and the initialization diagnostic when `stderr` works.
2. `TestRunCommandReportsRunAndCloseFailures` must still receive exit status 1
   and both joined errors.
3. `TestRunCommandReportsOutputFailure` must continue proving that reporter
   output failures affect the final command result.
4. Run `just fmt`, `just check`, and `just coverage`. `runCommand` should have
   100% statement coverage without adding tests for outcome-equivalent returns.

## Action Items

- [x] Make terminal diagnostic writes explicitly best-effort.
- [x] Preserve reporter error capture and aggregation.
- [x] Complete the acceptance plan.
