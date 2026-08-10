# Repository Guidelines

## Project

This repository contains a Go CLI that discovers Docker Compose projects,
updates exactly the services that were already running, and prunes unused
images after successful work. Preserve Compose project identity, Docker
context consistency, per-project failure isolation, and exact running-service
selection when changing behavior.

## Development workflow

- Run `just fmt` after editing Go code.
- Run `just check` before handing off a change. It checks formatting, runs
  `golangci-lint`, and executes the race-enabled test suite.
- Use `just coverage` when changing behavior or tests; keep total statement
  coverage above 95%.
- Keep the default test suite independent of a Docker binary or running Docker
  daemon. Use in-memory fakes or construct clients without connecting.

## Go conventions

- Keep all Go source formatted with `gofmt`.
- Wrap operational errors with context and `%w` so callers can use
  `errors.Is` and `errors.As`.
- Prefer the narrow interfaces in `internal/updater` over leaking Docker or
  Compose implementation types into the update workflow.
- Add focused tests for success, failure, cancellation, and cleanup behavior.
  Parallelize tests unless they mutate process-wide state such as environment
  variables.
- Preserve unrelated working-tree changes. Do not edit generated artifacts
  unless the task requires it.
