# Issue 009: Clean up after pulls that fail after partial mutation

- Priority: P2
- State: Resolved
- Verification verdict: Fixed and covered by updater tests; any pull attempt conservatively schedules at most one final prune, canceled runs still skip pruning, and pull and prune failures remain aggregated.
- Review source: Failed pulls can still require cleanup
- Affected code: [`internal/updater/updater.go`](../../internal/updater/updater.go#L138-L181), [`internal/updater/updater_test.go`](../../internal/updater/updater_test.go#L137-L157)

## Summary

The updater marks image cleanup as necessary only after `ProjectSession.Pull`
returns `nil`.

A Compose pull is not atomic. It pulls multiple service images concurrently,
and one image can be updated successfully before another image fails. In that
case the operation returns an error after mutating local image state, but the
updater records no cleanup requirement. If every project pull returns an error,
the run skips pruning entirely.

## Why this is bad

The `needsPrune` boolean currently models operation success rather than the
condition it names: whether a pull may have created cleanup work.

This makes cleanup dependent on an invariant the backend cannot provide. A
single `error` return cannot prove that no image or layer was downloaded, no
tag moved, and no dangling data was created before failure.

The test `TestRunDoesNotPruneWhenEveryPullFails` locks in that incorrect
assumption by treating every failed pull as mutation-free.

## Proof

The updater sets the cleanup flag only after the error branch:

```go
if err := session.Pull(ctx); err != nil {
    // record project failure
    continue
}

needsPrune = true
```

In the pinned `github.com/docker/compose/v5@v5.4.0`, Compose `Pull` launches
per-service pulls through an `errgroup`. Independent goroutines can finish
successfully before one returns an error. `Pull` then returns that error even
though successful siblings may already have updated image tags or downloaded
layers.

For a one-project run with such a partial failure, `needsPrune` remains false
and `PruneImages` is never called.

## Proposed change

Use the conservative invariant that beginning a pull can create cleanup work:

```go
needsPrune = true
if err := session.Pull(ctx); err != nil {
    // record project failure
    // continue unless canceled
}
```

The existing `ctx.Err() == nil` guard should continue to prevent a new prune
mutation after cancellation.

If pruning after every non-canceled pull attempt is considered too broad,
replace `Pull(context.Context) error` with an explicit typed result that can
report whether mutation occurred. Do not infer mutation absence from a non-nil
error.

## Behavior change

Today, cleanup is scheduled only when at least one project pull returns
successfully. If every pull returns an error, the updater does not prune even
when one of those aggregate operations successfully pulled another image
before failing.

After the conservative change, beginning any non-canceled pull attempt is
enough to schedule one final prune. This means:

- A failed pull that partially changed image state is cleaned up at the end of
  the run.
- Several successful or failed pull attempts still produce at most one prune.
- A failed pull that made no changes may also cause a prune; the normal Docker
  image-prune policy determines what, if anything, is removed.
- Cancellation continues to suppress pruning so the updater does not start a
  new mutation with a canceled context.
- Pull failures remain reported, and a prune failure is reported alongside
  them rather than replacing them.

If the typed-result alternative is implemented instead, pruning behavior
changes only for failed pulls that explicitly report a possible mutation.

## Acceptance criteria

- A non-canceled pull failure can still schedule the once-per-run cleanup.
- Multiple successful or failed pull attempts still result in at most one
  prune operation.
- Cancellation during pull does not begin pruning with the canceled context.
- Pull and project errors remain isolated and aggregated as before.
- A prune failure is joined with the original project failures.
- Tests no longer assume that `Pull` returning an error proves no mutation
  occurred.

## Test plan

- Replace the all-pulls-fail/no-prune assertion with a partial-mutation case
  that expects one final prune.
- Add multiple failed projects and verify that pruning occurs once.
- Add a failed pull followed by a successful pull and verify that pruning still
  occurs once.
- Preserve the cancellation test and verify that cancellation during pull
  suppresses pruning.
- Verify that a prune error remains discoverable with `errors.Is` alongside the
  pull error.
