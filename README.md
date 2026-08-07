# Update Docker Compose Projects

Updates the currently running services in Docker Compose projects:

1. Discovers projects and their running services from typed container state.
2. Reloads each project with its original name, Compose files, working
   directory, and environment files.
3. Pulls images for the running services.
4. Runs Compose convergence so only services whose image or configuration
   diverged are recreated.
5. Prunes unused images once after at least one successful pull.

Failures are isolated by project. The updater attempts the remaining projects,
performs final cleanup when appropriate, and exits nonzero with all errors.
Each project is printed before its work begins, and its pull and convergence
progress share one project-scoped Compose renderer before the next project
starts with a fresh renderer.
Project names and statuses are colored when output is connected to a terminal;
redirected output and environments using `NO_COLOR` remain plain text.
This output contract and its rationale are recorded in
[ADR 0001](docs/adr/0001-project-scoped-progress-output.md).

## Tests

The default test suite uses in-memory backends and does not require the Docker
binary or a running Docker daemon:

```sh
just test
```

Generate a coverage profile and print per-function coverage with:

```sh
just coverage
```

The unit tests cover the update workflow, failure aggregation, retries,
cancellation, cleanup policy, Compose project metadata validation, typed
running-service selection, and Docker adapter option construction.
