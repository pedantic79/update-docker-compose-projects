# Issue 008: Open project sessions entirely inside the Docker boundary

- Priority: P2
- State: Resolved
- Verification verdict: Fixed and covered by updater and Docker backend tests; project loading, exact running-service selection, and fresh progress-session creation now occur behind one `OpenProject` boundary without exposing Compose SDK types to the updater.
- Review source: Collapse the Compose object round trip
- Affected code: [`internal/updater/updater.go`](../../internal/updater/updater.go#L30-L45), [`internal/updater/updater.go`](../../internal/updater/updater.go#L120-L136), [`internal/docker/backend.go`](../../internal/docker/backend.go#L89-L109)

## Summary

The updater boundary exposes `*types.Project` even though the updater never
reads, modifies, or owns that value.

`Backend.LoadProject` creates a Compose project in `internal/docker`; the
updater stores it briefly and immediately passes it back to
`Backend.NewProjectSession`. This round trip leaks a third-party infrastructure
type into the policy layer without giving the policy layer any useful
capability.

## Why this is bad

The current boundary creates concepts instead of removing them:

- `internal/updater` must import `compose-go` despite owning no Compose logic.
- Every fake backend must construct a `types.Project` merely to satisfy the
  handoff.
- Project loading and progress-session construction appear as separate policy
  operations even though both belong to the Docker adapter.
- The updater needs two nearly identical project-failure branches.
- The adapter cannot make project loading, exact service selection, and session
  construction one coherent operation.

This is a thin pass-through abstraction. The project value has one producer and
one consumer in the same package, with an unrelated layer in between.

## Proof

The updater interface exposes the concrete Compose model:

```go
type Backend interface {
    DiscoverProjects(context.Context) ([]ProjectRef, error)
    LoadProject(context.Context, ProjectRef) (*types.Project, error)
    NewProjectSession(*types.Project) (ProjectSession, error)
    PruneImages(context.Context) error
}
```

The only production use is:

```go
project, err := u.backend.LoadProject(ctx, ref)
// error handling
session, err := u.backend.NewProjectSession(project)
```

No updater code inspects `project`. The Docker backend is both the producer and
the eventual consumer.

## Proposed change

Replace the two methods with one application-level operation:

```go
type Backend interface {
    DiscoverProjects(context.Context) ([]ProjectRef, error)
    OpenProject(context.Context, ProjectRef) (ProjectSession, error)
    PruneImages(context.Context) error
}
```

`internal/docker.Backend.OpenProject` should:

1. Load the Compose project with its original identity and launch metadata.
2. Narrow it to the exact running-service target set.
3. Create the fresh project-scoped Compose progress renderer.
4. Return a session that owns both the loaded project and Compose service.

The updater can then report one contextual preparation failure and remain free
of Compose SDK types.

If callers need load and session-creation errors distinguished for diagnostics,
the adapter can wrap them with operation context while retaining a single
boundary method.

## Behavior change

No user-visible runtime behavior is intended to change. Project discovery,
eligibility, pull and `up` ordering, per-project progress output, failure
isolation, cancellation, and final pruning should remain the same.

The change is architectural: instead of the updater receiving an opaque
Compose project and returning it to the Docker backend, the updater asks the
backend to open a ready-to-use project session. Load failures and progress
session creation failures must remain distinguishable in error messages even
though they cross one application-level method.

## Acceptance criteria

- `internal/updater` no longer imports `compose-go`.
- No concrete Docker or Compose SDK type appears in the updater `Backend`
  interface.
- Project loading and project-session construction are represented by one
  backend method.
- Load failures and progress-session failures retain clear operation-specific
  error messages.
- The Docker adapter owns exact service selection before exposing a session.
- Updater fakes do not construct placeholder `types.Project` values.
- The one-fresh-progress-session-per-project behavior remains unchanged.

## Test plan

- Update updater tests so fake backends return `ProjectSession` directly from
  `OpenProject`.
- Preserve separate tests for load failure and progress-renderer creation
  failure through wrapped adapter errors.
- Verify that each eligible project opens exactly one session and that skipped
  projects open none.
- Run the complete unit and race suites to confirm that cancellation, failure
  isolation, reporter ordering, and pruning policy remain unchanged.
