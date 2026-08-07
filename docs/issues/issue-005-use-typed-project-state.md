# Issue 005: Replace formatted status parsing with typed project state

- Priority: P2
- State: Resolved
- Verification verdict: No longer exists in the current implementation.
- Review source: Stop parsing aggregate status text
- Affected code: [`main.go`](../../main.go#L26-L45)

## Summary

Project eligibility is determined with:

```go
strings.HasPrefix(projectView.Status, "running(")
```

`api.Stack.Status` is formatted aggregate output intended for display. It is not a stable state contract and cannot express the update policy clearly.

## Why this is bad

The check couples control flow to punctuation, capitalization, ordering, and formatting owned by an external dependency. It also handles mixed-state projects accidentally rather than deliberately.

A project can have running and exited services at the same time. The updater should explicitly decide whether it updates:

- Projects with at least one running service.
- Only projects whose services are all running.
- Only the individual services that are currently running.

The current prefix check does not document or model any of these policies. It silently skips mixed-state projects and contradicts the README's broad statement that every project is processed.

## Proof

In `github.com/docker/compose/v5@v5.1.3/pkg/compose/ls.go`, `combinedStatus`:

1. Counts container states.
2. Sorts state names alphabetically.
3. Formats a string such as `exited(1), running(2)`.

Therefore a project with two running containers and one exited container does not start with `running(` and is skipped.

The same pinned Compose API exposes `Ps`, whose `ContainerSummary` includes typed `container.ContainerState`, service name, labels, and one-off metadata. Typed data is already available at the canonical boundary.

## Proposed change

Move eligibility into project discovery and base it on typed container summaries.

The proposed default policy is:

1. Exclude one-off containers.
2. A project is eligible when it has at least one running service container.
3. `ProjectRef.Services` contains the distinct names of currently running services.
4. Pull and up target those services so intentionally stopped services are not started as a side effect.
5. A project with no running services is skipped with a structured reason.

Represent the outcome explicitly:

```go
type ProjectState struct {
    RunningServices []string
    StoppedServices []string
}

func (s ProjectState) Eligible() bool {
    return len(s.RunningServices) > 0
}
```

If the desired policy is instead “all services must be running,” implement that as a named predicate with tests. Do not parse `Stack.Status` in either case.

## Acceptance criteria

- No application control flow parses `api.Stack.Status`.
- Eligibility is expressed through a named predicate over typed container states.
- Mixed running/exited projects behave according to a documented policy.
- Intentionally stopped services are not started accidentally.
- One-off containers do not make a project eligible.
- Skip output states the structured reason rather than echoing a string that the code also parses.

## Test plan

- Table-test projects containing only running, only exited, mixed running/exited, paused, created, dead, and one-off containers.
- Verify deterministic service ordering in `ProjectRef.Services`.
- Verify that mixed projects target only the service set selected by policy.
- Verify that formatting changes to `Stack.Status` cannot affect eligibility.
