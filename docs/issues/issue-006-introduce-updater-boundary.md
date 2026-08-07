# Issue 006: Replace the grab-bag Docker facade with a testable updater boundary

- Priority: P2
- State: Resolved
- Verification verdict: Resolved in substance; ADR 0001 supersedes the one-Compose-service-per-run criterion.
- Review source: Replace the grab-bag facade with an updater boundary
- Affected code: [`dockerclient/dockerclient.go`](../../dockerclient/dockerclient.go), [`main.go`](../../main.go)

## Summary

`dockerclient.DockerClient` combines endpoint construction, two SDK clients, Compose-service construction, project loading, image policy, pruning, terminal formatting, and restart logging.

Most methods merely construct a new Compose service and forward one call. `ServicePull` and `ServiceUp` return a dummy `any` value only so they can be passed through the generic `unwrap` helper. The abstraction adds indirection without creating a coherent ownership boundary or a useful test seam.

## Why this is bad

The current shape makes business behavior inseparable from infrastructure:

- Tests cannot inject a fake backend because `DockerClient` constructs concrete dependencies internally.
- Console formatting occurs inside Docker operations.
- Every wrapper mirrors third-party API option types, so application policy leaks through the facade.
- A new Compose service and event processor are created for each operation despite one CLI lifetime.
- Opaque `any` and `[2]string` results hide the real contracts.
- There are no repository test files protecting the update workflow.

This is not solved by adding more pass-through methods. The code needs one narrow boundary around the workflow and one adapter around Docker.

## Proof

The current `DockerClient` owns both `*client.Client` and `*command.DockerCli`.

`ServiceList`, `ServiceImages`, `ServiceUp`, `ServicePull`, and `ServiceLoadProject` each call `newDockerComposeService`. The first four wrappers do little beyond forwarding to `api.Compose`.

`ServiceUp` performs terminal rendering before invoking Compose, while `ImagePrune` also prints directly. This prevents callers from choosing output behavior and makes unit tests depend on process-global stdout and color state.

Both mutation methods return `(any, error)` while always returning `nil` for the value. That signature exists only to work around the command's generic unwrapping mechanism.

`git ls-files '*_test.go'` returns no files.

## Proposed change

Use two direct layers rather than a growing facade hierarchy:

### `internal/updater`

Own the application workflow and policy:

```go
type Backend interface {
    DiscoverProjects(context.Context) ([]ProjectRef, error)
    LoadProject(context.Context, ProjectRef) (*types.Project, error)
    Pull(context.Context, *types.Project) error
    Up(context.Context, *types.Project) error
    PruneImages(context.Context) error
}

type Updater struct {
    backend Backend
}

func (u *Updater) Run(context.Context) (RunResult, error)
```

Keep this interface limited to operations the workflow actually needs. Do not mirror all of `api.Compose`.

### `internal/docker`

Own one initialized Docker CLI, one Compose service, context-consistent Engine access, label parsing, and translation between Docker SDK types and domain types.

Docker-specific option construction belongs here. Methods should return normal typed values or `error`, not dummy `any` results.

### `main`

Own signal context, terminal rendering, color choices, error printing, and exit status. Render typed `RunResult` and `ProjectResult` values produced by the updater.

The intended result is fewer concepts in the hot path:

```text
main -> updater.Run -> docker backend
```

Delete `newDockerComposeService` as a per-call factory. Construct the Compose service once and reuse it.

## Acceptance criteria

- The workflow can be unit-tested without a Docker daemon.
- Docker SDK construction is isolated to one adapter package.
- One Compose service is constructed per run.
- Infrastructure methods do not print or apply color.
- `ServicePull`/`ServiceUp`-style dummy `any` return values are removed.
- Domain types such as `ProjectRef`, `ProjectResult`, and `RunResult` replace opaque tuples and maps at package boundaries.
- The backend interface contains only application-required capabilities.
- Tests cover happy path, skip policy, partial failure, retry after pull, pruning policy, and cancellation.

## Test plan

- Add updater unit tests using a small hand-written fake backend.
- Add adapter tests for container-label parsing and Compose option construction.
- Add command tests for rendering and exit status with injected writers.
- Keep a small optional integration suite for real Docker/Compose behavior; unit tests must remain daemon-independent.
