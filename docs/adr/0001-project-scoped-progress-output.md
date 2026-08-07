# ADR 0001: Keep progress scoped to one project and operation

- Status: Accepted
- Date: 2026-08-06

## Context

This command is normally watched while it updates several independent Docker
Compose projects. Its output is therefore part of its operational interface,
not merely diagnostic logging.

The original output presented one project at a time. It printed the project
name and status, displayed that project's pull activity, and displayed any
restart activity before moving to the next project. An operator could scan the
terminal from top to bottom and answer three questions immediately:

1. Which project is being processed?
2. Is it pulling images or reconciling containers?
3. Did that operation complete before the next project began?

A refactor changed this behavior in two ways:

- Project headings were collected and printed only after every project had
  finished.
- One Compose progress renderer was reused for multiple pull and `up`
  operations. Because the renderer retains progress entries, otherwise
  sequential operations appeared as one large combined `up` block containing
  images and containers from unrelated projects.

Although the underlying update workflow remained sequential, the presentation
made it look concurrent and obscured which events belonged to which project.
The maintainer prefers the original project-at-a-time presentation because the
command is easier to follow and failures are easier to associate with their
project.

## Decision

Progress output is a synchronous, project-scoped stream.

For every discovered project, the command will:

1. Print the project name and display status before performing project work.
2. Finish the project's pull operation and its progress display.
3. Finish the project's Compose convergence (`up`) and its progress display.
4. Print a blank separator before starting the next project.

Each project receives one fresh Compose progress session, shared by its pull
and `up` operations. This keeps related image and container activity together
while ensuring retained progress entries cannot leak into the next project's
section. Every project session uses the same initialized Docker CLI and Engine
client, so all operations still use one selected Docker context and daemon.
Only the stateful progress renderer and its thin Compose service wrapper are
recreated between projects.

Project names and statuses retain distinct colors because color is part of the
visual hierarchy that makes a project section easy to scan. Color is enabled
only for terminal output. Redirected output, dumb terminals, and environments
that set `NO_COLOR` receive the same content without ANSI escape sequences.

Image pruning remains a once-per-run operation and is displayed as a separate
final section. Project failures remain isolated: a failure is associated with
the current project, later independent projects are attempted, and the command
returns the aggregated errors at the end.

The status printed in a project heading is display-only. Update eligibility is
still determined from typed container state; application control flow must not
parse the formatted status string.

This decision does not restore mutable image-tag comparisons or forced
recreation. The updater still calls Compose convergence for every eligible
project so Compose can reliably detect stale containers after earlier partial
failures or out-of-band image pulls.

## Consequences

### Positive

- Terminal output follows the same order as execution.
- Image and container events have an obvious owning project.
- Pull and convergence activity stays together within its owning project.
- Project names and statuses remain visually distinct within each section.
- Failures can be understood without reconstructing an interleaved event log.
- The correctness improvements around Compose convergence, retries, typed
  state, error isolation, and Docker context selection are preserved.

### Negative

- A small Compose service wrapper and progress renderer are constructed for
  each eligible project.
- An unchanged project may still show a short `up` block because convergence
  must run to detect stale containers reliably.
- Output-order tests intentionally constrain this part of the command-line
  interface.

## Alternatives considered

### Reuse one progress renderer for the entire run

Rejected because retained progress entries merge unrelated projects into one
ambiguous block.

### Create separate progress renderers for pull and `up`

Rejected because both operations belong to the same project section. Sharing
one project-scoped renderer preserves that relationship without allowing
progress entries to carry into the next project.

### Buffer all output and print a summary at the end

Rejected because the command is watched interactively and the operator needs
to know what is happening while a pull or restart is in progress.

### Update projects concurrently

Rejected for the default workflow because concurrent progress would interleave
project events and make the output harder to audit. It would also make daemon
load and failure behavior less predictable.

### Run `up` only when a before/after tag comparison changes

Rejected because image tags are mutable. That approach can miss a container
that remains on an old immutable image after a partial failure or an image pull
performed outside this command.
