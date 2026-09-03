# WI-008: Replace the test-only substring helper with `strings.Contains`

- **File**: [internal/updater/updater_test.go](../../internal/updater/updater_test.go#L1)
- **Severity**: Suggestion
- **Impact**: One updater test uses a local byte-substring loop for behavior already provided by the Go standard library and used by the repository's other test packages.

## Description

`TestRunIsolatesAndAggregatesProjectFailures` calls a local `contains` helper.
The helper has no project-specific semantics and duplicates `strings.Contains`.
Use the standard library directly and remove the helper.

## Proposed Fix

```diff
--- a/internal/updater/updater_test.go
+++ b/internal/updater/updater_test.go
@@
 import (
 	"context"
 	"errors"
 	"fmt"
 	"reflect"
+	"strings"
 	"testing"
 )
@@
 	for _, name := range []string{"load-fails", "session-fails", "pull-fails", "up-fails"} {
-		if !contains(err.Error(), "project \""+name+"\"") {
+		if !strings.Contains(err.Error(), "project \""+name+"\"") {
 			t.Errorf("Run() error %q does not identify project %q", err, name)
 		}
 	}
@@
-func contains(value, substring string) bool {
-	for i := 0; i+len(substring) <= len(value); i++ {
-		if value[i:i+len(substring)] == substring {
-			return true
-		}
-	}
-	return false
-}
```

## Acceptance Plan

1. Run `TestRunIsolatesAndAggregatesProjectFailures` and confirm it still checks
   that every failed project is named in the joined error.
2. Run `just fmt`, `just check`, and `just coverage`; total statement coverage
   must remain above 95%.

## Action Items

- [x] Import `strings` in `internal/updater/updater_test.go`.
- [x] Replace the sole helper call with `strings.Contains`.
- [x] Delete the local `contains` helper.
- [x] Complete the acceptance plan.
