# Issue 004: Isolate project failures and aggregate errors at the command boundary

- Priority: P1
- State: Resolved
- Verification verdict: No longer exists in the current implementation.
- Review source: Isolate failures per project
- Affected code: [`main.go`](../../main.go#L14-L64)

## Summary

The generic `unwrap` helper panics on every error. Because every project is processed inside one loop, one project failure immediately terminates the process, prevents later projects from updating, and can skip final cleanup.

This is the wrong failure model for independent batch work. Project-level failures should be contextual, isolated, and reported together after all safe work is complete.

## Why this is bad

The current failure boundary is the entire process rather than one project. A stale configuration file, unavailable registry, or malformed project can block updates for every unrelated project on the host.

Panic output also lacks deliberate operational context. An error such as `no such file or directory` does not identify whether list, load, image inspection, pull, up, or prune failed, and it may not identify the project.

The failure can occur after an operation has already changed local state. Pulling is not atomic across all services. Exiting immediately can leave moved tags, old containers, and skipped cleanup. This compounds the stale-container behavior described in Issue 001.

## Proof

`unwrap` is used for:

- Client construction.
- Project listing.
- Project loading.
- Image listing.
- Pulling.
- Restart detection.
- Running `up`.
- Pruning.

Every error reaches `panic(err)`. There is no per-project recovery or error collection.

`needsPrune` is evaluated only after the project loop. Any panic during the loop bypasses that block. A failure in project 2 also guarantees that projects 3 through N are never attempted.

## Proposed change

Introduce explicit command and project boundaries:

```go
func main() {
    if err := run(signalContext()); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run(ctx context.Context) error
func updateProject(ctx context.Context, ref ProjectRef) (ProjectResult, error)
```

The policy should be:

- Initialization and project discovery failures are fatal because no safe batch can be formed.
- A project load, pull, or up failure is recorded with the project name and operation, then processing continues with the next independent project.
- Context cancellation stops new work promptly and is returned.
- Pruning runs once after project processing when the configured cleanup policy requires it, even if some projects failed.
- Prune failures are joined with project failures.
- `run` returns `errors.Join(collected...)`; `main` prints the result and exits nonzero.

All errors should be wrapped at the layer that knows the operation:

```go
fmt.Errorf("project %q: pull: %w", ref.Name, err)
```

Do not replace `panic` with scattered logging. Keep error ownership explicit and testable.

## Acceptance criteria

- One failing project does not prevent later independent projects from being attempted.
- The process exits nonzero when any project or final cleanup fails.
- Every reported project error includes the project name and failed operation.
- Fatal discovery errors stop before mutation.
- Cancellation stops scheduling new project work.
- Cleanup runs according to policy after successful mutations, even when another project failed.
- The generic `unwrap` helper is removed.

## Test plan

- Simulate three projects where the middle project fails; verify that projects one and three are processed.
- Verify that multiple failures are returned together with `errors.Is` support intact.
- Verify that initialization and discovery failures perform no pull or up calls.
- Verify cleanup behavior after all-success, partial-success, all-failure, and cancellation cases.
- Verify command exit status and concise stderr output without panic stack traces.
