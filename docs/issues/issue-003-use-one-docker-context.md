# Issue 003: Use one Docker client and one selected context for every operation

- Priority: P1
- State: Resolved
- Verification verdict: No longer exists in the current implementation.
- Review source: Do not create a second Docker endpoint
- Affected code: [`dockerclient/dockerclient.go`](../../dockerclient/dockerclient.go#L24-L47), [`dockerclient/dockerclient.go`](../../dockerclient/dockerclient.go#L60-L83)

## Summary

`DockerClient.New` creates two independent clients:

- A Moby Engine client configured from environment variables.
- A Docker CLI client configured using Docker CLI context resolution.

Compose list, load, pull, and up operations use the Docker CLI client. Image inspection and pruning use the independent Moby client. Those clients are not guaranteed to target the same daemon.

## Why this is bad

Docker contexts are a first-class endpoint-selection mechanism. A user can select a remote context in `~/.docker/config.json` or through `DOCKER_CONTEXT` without setting `DOCKER_HOST`.

In that normal configuration, this program can:

1. List and update projects on the selected remote daemon.
2. Inspect images on the default local daemon.
3. Decide whether a remote project needs a restart using unrelated local state.
4. Prune images on the local daemon after updating the remote daemon.

The last behavior is a destructive cross-boundary error. The `DockerClient` struct visually suggests one connection while hiding two independently resolved endpoints.

## Proof

The current constructor calls:

```go
client.New(client.FromEnv, client.WithAPIVersionFromEnv())
command.NewDockerCli()
dockerCli.Initialize(flags.NewClientOptions())
```

In `github.com/moby/moby/client@v0.4.1`, `FromEnv` reads `DOCKER_HOST`, `DOCKER_API_VERSION`, `DOCKER_CERT_PATH`, and `DOCKER_TLS_VERIFY`. It does not read Docker CLI's persisted current context.

In `github.com/docker/cli@v29.4.3+incompatible`, `DockerCli` resolves context in this order:

1. Command-line context option.
2. `DOCKER_CONTEXT`.
3. The current context stored in Docker CLI configuration.
4. The default context.

The two resolution algorithms therefore diverge whenever a non-default context is selected without an overriding `DOCKER_HOST`.

The raw `client` field is used by both `NeedsRestart` and `ImagePrune`; the `dockerCli` field backs every Compose operation.

## Proposed change

Create and initialize one `command.DockerCli`, then use its Engine client for any remaining low-level operation:

```go
dockerCli.Client()
```

The backend should construct one `api.Compose` service from the same CLI and retain it for the lifetime of the run.

After Issue 001 removes custom image inspection, the only likely low-level operation is image pruning. That operation should use an injected narrow interface backed by `dockerCli.Client()`, for example:

```go
type ImagePruner interface {
    ImagePrune(context.Context, client.ImagePruneOptions) (client.ImagePruneResult, error)
}
```

Do not create another Moby client from environment variables. The backend should expose its resolved context or daemon host in debug output so endpoint selection can be diagnosed.

## Acceptance criteria

- Exactly one Docker endpoint is resolved per process.
- Compose operations and image pruning use the same underlying Docker CLI client.
- Selecting a context through `docker context use` works without setting `DOCKER_HOST`.
- Selecting a context through `DOCKER_CONTEXT` works consistently for every operation.
- No call to `client.New(client.FromEnv, ...)` remains in application code.
- Initialization failures are returned as errors instead of causing hidden endpoint fallback.

## Test plan

- Unit-test the backend with one injected fake Engine client and verify that pruning uses that instance.
- Add an integration test with a named Docker context whose endpoint differs from the default endpoint.
- Verify that list, pull, up, and prune requests all reach the named context.
- Verify that invalid context configuration fails before any project mutation occurs.
