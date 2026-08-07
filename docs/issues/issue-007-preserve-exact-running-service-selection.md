# Issue 007: Preserve the exact running-service selection through Compose loading

- Priority: P1
- State: Resolved
- Verification verdict: Fixed and covered by backend tests; the final project passed to pull and up is narrowed to the exact running-service selection while profiled services still load successfully.
- Review source: Stopped dependencies are included again
- Affected code: [`internal/docker/backend.go`](../../internal/docker/backend.go#L89-L100), [`internal/docker/backend.go`](../../internal/docker/backend.go#L143-L162), [`internal/docker/backend_test.go`](../../internal/docker/backend_test.go#L12-L101)

## Summary

Discovery correctly records only services with running containers in
`ProjectRef.Services`, but that exact selection is lost while the Compose
project is loaded.

Docker Compose expands `ProjectLoadOptions.Services` to include service
dependencies. The backend later derives the pull and `up` targets from
`project.ServiceNames()`, so a dependency that discovery classified as stopped
can be pulled and started as a side effect of updating a running service.

## Why this is bad

The updater documents an explicit policy: converge currently running services
without starting intentionally stopped services. Dependency expansion violates
that policy at the infrastructure boundary after the typed discovery layer has
already made the correct decision.

This is particularly surprising for projects in which an operator has stopped
a database, worker, or optional supporting service while leaving a dependent
frontend running. The updater can silently reverse that operational decision.

The current tests use independent services, so they prove that service slices
are forwarded but not that the same set reaches `Pull` and `Up`.

## Proof

`projectLoadOptions` passes the running service names through
`api.ProjectLoadOptions.Services`:

```go
Services: append([]string(nil), ref.Services...),
```

In the pinned `github.com/docker/compose/v5@v5.4.0`, `LoadProject` eventually
calls:

```go
project.WithSelectedServices(options.Services)
```

`compose-go` documents and implements `WithSelectedServices` as selecting the
named services **and their dependencies** unless `types.IgnoreDependencies` is
explicitly supplied.

The backend then uses the expanded model as the target set:

```go
services := project.ServiceNames()
```

Compose `Pull` also iterates every service in `project.Services`. Therefore, if
running service `web` depends on stopped service `db`, both `web` and `db` can
be pulled and passed to `Up`.

## Proposed change

Preserve profile activation during the initial load, then narrow the loaded
project back to the exact running-service set:

```go
project, err := b.compose.LoadProject(ctx, projectLoadOptions(ref))
if err != nil {
    return nil, err
}

project, err = project.WithSelectedServices(ref.Services, types.IgnoreDependencies)
if err != nil {
    return nil, err
}
```

An equivalent design may retain `ref.Services` as the explicit session target
and implement the semantics of `docker compose pull SERVICE` plus
`docker compose up --no-deps SERVICE`. The important invariant is that the
target set reaching pull and convergence exactly matches the typed running
services selected during discovery.

Do not remove dependency expansion before profile activation is handled. A
running service may belong to a profile that must be enabled while the project
is reconstructed.

## Behavior change

Today, selecting a running service also selects its Compose dependencies. If
`web` is running and depends on an intentionally stopped `db`, an updater run
can pull `db` and start or recreate it while converging `web`.

After this change, the service set captured during discovery becomes exact:

- `web` is pulled and converged because it was running at discovery time.
- `db` remains stopped and is neither pulled nor passed to `up` unless it also
  had a running container at discovery time.
- Dependency expansion may still be used internally while loading the Compose
  model and activating profiles, but it cannot enlarge the final mutation
  target set.

This intentionally changes update behavior to the equivalent of targeting the
running services with `--no-deps`. A running service will no longer cause the
updater to repair or start a stopped dependency on its behalf.

## Acceptance criteria

- A running service whose dependency is stopped does not pull, create, start,
  or recreate that stopped dependency.
- A dependency that is itself running remains in the selected update set.
- Running services enabled through profiles still load successfully.
- Pull and `up` receive exactly the distinct running services selected during
  discovery.
- The backend tests cover a project with a running service and an intentionally
  stopped dependency.
- An integration test, if available, verifies that the stopped dependency
  remains stopped after a complete updater run.

## Test plan

- Add an adapter test with `web -> db`, where only `web` appears in
  `ProjectRef.Services`; assert that the final project and all operation options
  contain only `web`.
- Add a case where both `web` and `db` are running; assert that both remain
  selected.
- Add a profiled running service to ensure the initial load still activates its
  profile before exact selection is applied.
- Add an optional Docker integration test that starts both services, stops
  `db`, runs the updater, and verifies that `db` remains stopped.
