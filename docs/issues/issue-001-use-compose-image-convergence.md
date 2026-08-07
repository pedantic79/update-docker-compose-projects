# Issue 001: Use Compose image convergence instead of mutable-tag snapshots

- Priority: P1
- State: Resolved
- Verification verdict: No longer exists in the current implementation.
- Review source: Image detector loses running-container identity
- Affected code: [`main.go`](../../main.go#L47-L58), [`dockerclient/dockerclient.go`](../../dockerclient/dockerclient.go#L60-L78), [`dockerclient/dockerclient.go`](../../dockerclient/dockerclient.go#L104-L155)

## Summary

The updater decides whether to recreate a project by recording the image ID currently resolved by each image tag, pulling, and resolving the same tag again. That is not the same as comparing the desired image with the immutable image used by the running container.

This can leave a container permanently stale after a partial pull, a failed `up`, or an image pulled outside this program. The simpler and more reliable design is to let Docker Compose perform its own image-digest convergence.

## Why this is bad

Image tags are mutable. Once a pull moves a tag, the old value cannot be reconstructed by resolving that tag again.

The current flow therefore loses the only state that matters: the immutable image ID attached to the running container. It also duplicates convergence behavior already implemented and tested by Compose, adds a second image-inspection path, and forces every service to be recreated when any image changes.

The failure is persistent rather than limited to one run. After the mutable tag has moved, later runs can conclude that nothing changed even while an old container remains active.

## Proof

### Current execution sequence

[`main.go`](../../main.go#L47-L58) does the following:

1. Calls `ServiceImages` before pulling.
2. Pulls the project's images.
3. Passes the pre-pull summaries to `NeedsRestart`.
4. Resolves each repository and tag again and compares the two tag resolutions.

`NeedsRestart` never reads the image ID used by the running container.

### Pinned Compose behavior

In `github.com/docker/compose/v5@v5.1.3`:

- `pkg/compose/images.go`, `Images`, calls `ImageInspect(ctx, c.Image)`. `c.Image` is an image reference such as `nginx:latest`; the container summary's immutable `ImageID` is not used for the returned ID.
- `pkg/compose/build.go`, `ensureImagesExists`, records the desired local image digest in the `com.docker.compose.image` label.
- `pkg/compose/convergence.go`, `mustRecreate`, compares the existing container's recorded digest with the desired digest and recreates the service when they differ.

Compose already owns the exact convergence rule this program is trying to reproduce.

### Failure sequence

1. Container `web` is running immutable image `A`; tag `example/web:latest` resolves to `A`.
2. The updater records `A` through `ServiceImages`.
3. `ServicePull` updates the tag to image `B` but then fails on another service, or `ServiceUp` fails.
4. The process exits while `web` still runs `A` and the tag resolves to `B`.
5. On the next run, `ServiceImages` records `B` because it resolves the tag.
6. `NeedsRestart` also resolves the tag to `B`.
7. The updater compares `B` with `B` and never recreates the container still running `A`.

The same state occurs when another process pulls `B` before this updater starts.

## Proposed change

Use the Compose convergence engine as the canonical owner of image-change detection:

1. Remove the pre-pull `ServiceImages` call.
2. Delete `NeedsRestart` and its `map[string][2]string` result.
3. Pull the project.
4. Call `Up` with the normal diverged/default recreation policy instead of `api.RecreateForce`.
5. Allow Compose to compare its existing container digest label with the post-pull desired digest.
6. Return a normal `error` from `ServicePull` and `ServiceUp`; remove their dummy `any` results.

The default behavior should recreate only services whose image or configuration diverged. If restarting the entire project after any image change is a hard product requirement, detect that condition after the pull by comparing immutable running-container image IDs or digest labels with post-pull desired digests. Do not restore a before/after mutable-tag comparison.

Pruning should remain a once-per-run cleanup policy. It must not be coupled to the deleted snapshot map.

## Acceptance criteria

- A tag pulled before the updater starts causes an older running container to converge to the pulled image.
- If a pull changes one tag and then fails, a later successful run still recreates the stale container.
- If `up` fails after a successful pull, the next run retries convergence.
- An unchanged project does not force-recreate every service.
- Digest-pinned and untagged/local image references do not produce invalid references such as `repository:` or `:`.
- `NeedsRestart`, the pre-pull `ServiceImages` snapshot, and the opaque `[2]string` image-diff tuple are removed.

## Test plan

- Unit-test the update workflow with a fake Compose backend that records `Pull` and `Up` calls.
- Add an integration test where a container runs image `A`, the tag moves to `B` before the updater starts, and the updater converges to `B`.
- Add a retry test where pull mutates one tag and returns an error before the next run.
- Add coverage for unchanged, digest-pinned, shared-image, and buildable services.
