# WI-006: Couple console streams with their error labels and simplify colorization

- **File**: [main.go](../../main.go#L58)
- **Severity**: Suggestion
- **Impact**: Reporter call sites repeatedly pair an output stream with a separate destination label, and `colorize` is a method whose receiver is unused. Small helpers can make stream/label mismatches impossible and make colorization's dependencies explicit.

## Description

Every `writef` caller must currently pass either `(r.stdout, "stdout")` or
`(r.stderr, "stderr")`. Those values are one logical choice, but the type
system does not keep them synchronized. Add stream-specific wrappers while
retaining `writef` as the single place that stores the first output failure.

Colorization depends only on an enabled flag, an ANSI code, and a value. Make it
a package function rather than an instance method, and name the two ANSI codes
used by the reporter.

## Proposed Fix

```diff
--- a/main.go
+++ b/main.go
@@
+const (
+	ansiRed  = "31"
+	ansiBlue = "34"
+)
+
 type consoleReporter struct {
@@
 func (r *consoleReporter) ProjectStarted(project updater.ProjectRef) {
 	if r.started {
-		r.writef(r.stdout, "stdout", "\n")
+		r.outf("\n")
 	}
@@
-	r.writef(
-		r.stdout,
-		"stdout",
+	r.outf(
 		"Name:%s, Status:%s\n",
-		r.colorize(r.stdoutColor, "31", project.Name),
-		r.colorize(r.stdoutColor, "34", status),
+		colorize(r.stdoutColor, ansiRed, project.Name),
+		colorize(r.stdoutColor, ansiBlue, status),
 	)
 }
@@
 	if project.Status == updater.ProjectSkipped {
-		r.writef(
-			r.stderr,
-			"stderr",
+		r.errf(
 			"skipping %s: %s\n",
-			r.colorize(r.stderrColor, "31", project.Name),
+			colorize(r.stderrColor, ansiRed, project.Name),
 			project.Reason,
 		)
 	}
 }
@@
 func (r *consoleReporter) PruneStarted() {
 	if r.started {
-		r.writef(r.stdout, "stdout", "\n")
+		r.outf("\n")
 	}
-	r.writef(r.stdout, "stdout", "%s\n", r.colorize(r.stdoutColor, "31", "Pruning images..."))
+	r.outf("%s\n", colorize(r.stdoutColor, ansiRed, "Pruning images..."))
 }
 
 func (r *consoleReporter) PruneFinished(err error) {
 	if err == nil {
-		r.writef(r.stdout, "stdout", "Pruned unused images.\n")
+		r.outf("Pruned unused images.\n")
 	}
 }
@@
+func (r *consoleReporter) outf(format string, args ...any) {
+	r.writef(r.stdout, "stdout", format, args...)
+}
+
+func (r *consoleReporter) errf(format string, args ...any) {
+	r.writef(r.stderr, "stderr", format, args...)
+}
+
 func (r *consoleReporter) writef(writer io.Writer, destination, format string, args ...any) {
 	if _, err := fmt.Fprintf(writer, format, args...); err != nil && r.err == nil {
 		r.err = fmt.Errorf("write %s: %w", destination, err)
 	}
 }
 
-func (r *consoleReporter) colorize(enabled bool, code, value string) string {
+func colorize(enabled bool, code, value string) string {
 	if !enabled {
 		return value
 	}
```

Add a focused test to preserve the destination label for failed `stderr`
writes:

```go
func TestConsoleReporterReportsStderrWriteFailure(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("broken pipe")
	reporter := &consoleReporter{
		stdout: &bytes.Buffer{},
		stderr: failingWriter{err: writeErr},
	}

	reporter.ProjectFinished(updater.ProjectResult{
		Name:   "stopped",
		Status: updater.ProjectSkipped,
		Reason: "no running services",
	})

	if !errors.Is(reporter.err, writeErr) {
		t.Fatalf("reporter error = %v, want %v", reporter.err, writeErr)
	}
	if !strings.Contains(reporter.err.Error(), "write stderr") {
		t.Fatalf("reporter error = %q, want stderr destination", reporter.err)
	}
}
```

## Acceptance Plan

1. Add `TestConsoleReporterReportsStderrWriteFailure` as shown above.
2. Preserve independent stdout and stderr color behavior in
   `TestConsoleReporterStderrColorMismatch`.
3. Preserve the exact successful command output asserted by
   `TestRunCommandRendersSuccessfulRunWithoutDocker`.
4. Preserve first-write-error behavior in `writef`.
5. Run `just fmt`, `just check`, and `just coverage`; total statement coverage
   must remain above 95%.

## Action Items

- [ ] Add `outf` and `errf` wrappers.
- [ ] Convert `colorize` to a pure package function and name its ANSI codes.
- [ ] Add the focused failed-`stderr` test.
- [ ] Complete the acceptance plan.
