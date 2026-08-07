# Issue 002: Preserve the original Compose project identity and launch metadata

- Priority: P1
- State: Resolved
- Verification verdict: No longer exists in the current implementation.
- Review source: Preserve the original Compose project identity
- Affected code: [`dockerclient/dockerclient.go`](../../dockerclient/dockerclient.go#L170-L186), [`main.go`](../../main.go#L47-L57)

## Summary

`ServiceLoadProject` attempts to reconstruct a running Compose project from only `api.Stack.ConfigFiles`. That value is a lossy, comma-joined display field, not a complete project identity.

The loader currently drops the explicit project name, multiple configuration paths, actual working directory, explicit environment files, and the set of running services that may have enabled profiles. As a result, the updater can fail to load a valid project, load a different model, miss active services, or run `up` under the wrong project name.

## Why this is bad

Compose project loading is sensitive to more than the path of one YAML file:

- `-p` and `COMPOSE_PROJECT_NAME` can override the directory-derived project name.
- Multiple `-f` files are merged in order.
- `--project-directory` controls relative paths and `.env` lookup.
- `--env-file` controls interpolation and may materially change the model.
- Explicitly selected services can activate otherwise disabled profiles.

Discarding those inputs makes the reconstructed `types.Project` an approximation. Passing an approximation to `up --remove-orphans` is particularly dangerous because reconciliation decisions are made from that model.

## Proof

### Multiple configuration files are encoded into one string

In `github.com/docker/compose/v5@v5.1.3`:

- `pkg/compose/loader.go` stores `project.ComposeFiles` in the container label with `strings.Join(project.ComposeFiles, ",")`.
- `pkg/compose/ls.go` combines the label values and returns `Stack.ConfigFiles` as another comma-joined string.

The current implementation then does both of the following:

```go
workingDir := filepath.Dir(stack.ConfigFiles)
ConfigPaths: []string{stack.ConfigFiles}
```

For `/srv/base.yml,/srv/prod.yml`, that computes a directory from the combined string and asks Compose to open one nonexistent path containing a comma.

### Explicit project names are discarded

`api.ProjectLoadOptions` has a `ProjectName` field. The current call leaves it empty. A project launched as `docker compose -p billing ...` can therefore be reloaded under a directory-derived name instead of `billing`.

### Required metadata already exists on containers

Compose defines and writes these labels:

- `com.docker.compose.project`
- `com.docker.compose.project.config_files`
- `com.docker.compose.project.working_dir`
- `com.docker.compose.project.environment_file`
- `com.docker.compose.service`

`api.Compose.Ps` returns typed container summaries including their labels, service, and state. The current code uses none of this information.

### Profiles are omitted

The pinned Compose loader's profile tests show that loading without a profile excludes services declared only under that profile. Explicit service selection can enable the required profile. The current loader passes neither profiles nor the running service names, while `ServiceImages` inspects all containers in the project.

## Proposed change

Replace `api.Stack` as the project's working model with a validated domain type discovered from running containers:

```go
type ProjectRef struct {
    Name        string
    ConfigPaths []string
    WorkingDir  string
    EnvFiles    []string
    Services    []string
}
```

Discovery should:

1. Use Compose `Ps` or the Docker Engine container list through the same Docker CLI client.
2. Group non-one-off containers by the Compose project label.
3. Read and split the config-file and environment-file labels.
4. Preserve the labeled working directory and project name.
5. Record distinct service names for containers that are eligible for update.
6. Verify that containers in a project agree on project-level labels; return a contextual error when they do not.

Load the project with explicit values:

```go
api.ProjectLoadOptions{
    ProjectName: projectRef.Name,
    ConfigPaths: projectRef.ConfigPaths,
    WorkingDir:  projectRef.WorkingDir,
    EnvFiles:    projectRef.EnvFiles,
    Services:    projectRef.Services,
}
```

This project reference should be the single input to pull and up operations. Remove `filepath.Dir(stack.ConfigFiles)` and stop passing the unsplit display value to the loader.

## Acceptance criteria

- Projects launched with multiple `-f` files load the same merged model in the original order.
- Projects launched with `-p` or `COMPOSE_PROJECT_NAME` retain the running project name.
- `--project-directory` and explicit `--env-file` values are preserved.
- Running services enabled through profiles are included without starting unrelated inactive profile services.
- Missing or inconsistent required labels produce a clear project-specific error and do not run `up` against an approximate model.
- Project discovery has no dependency on `filepath.Dir` over the `Stack.ConfigFiles` display string.

## Test plan

- Table-test parsing and validation of project labels.
- Test one-file and multi-file projects.
- Test explicit project name, project directory, and environment files.
- Test a project with one default service, one active profiled service, and one inactive profiled service.
- Test inconsistent label values across containers and missing-label errors.
