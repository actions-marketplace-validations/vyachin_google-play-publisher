# ADR-0003 — Cobra and a layered command structure

- **Status:** proposed (design)
- **Date:** 2026-11-09
- **Context:** the tool will grow with commands (`rollout`, `promote`, metadata); we need a
  predictable way to add commands without bloating `main` or mixing CLI with HTTP.

## Context

One command `publish` is enough today, but the roadmap includes `status`, `rollout`, `promote`
and possibly metadata management. Requirements:

- a consistent UX: global flags, exit codes, JSON, debug mode for every command;
- business logic must not depend on the framework or on `androidpublisher`;
- commands must be testable without network;
- completion, help and command grouping "out of the box";
- minimal dependencies.

## Considered options

1. **`spf13/cobra` + `pflag`.** The de-facto standard for Go CLIs (kubectl, gh, hugo):
   subcommands, groups, completion, documentation generation, testability through
   `SetArgs/ExecuteContext`. Cost: a dependency and some boilerplate.
2. **`stdlib flag` + manual dispatch.** Zero dependencies, full control. Cost: hand-written
   subcommand parsing, help, completion, error handling; with more commands it quickly becomes a
   mini-framework.
3. **`urfave/cli`.** Simpler than cobra, but a weaker nested-command and completion model.
4. **`alecthomas/kong`.** Declarative structs, little code, good help; smaller ecosystem and
   less familiar to most Go developers.

## Decision

Use **cobra** and a layered structure:

```
internal/cli   — flag parsing, command construction, output (thin layer)
internal/app   — use cases: publish, doctor, status (no cobra, no HTTP)
internal/play  — Play API adapter (the only place with androidpublisher)
```

Rules:

- global flags (`--debug`, `--json`, `--log-format`, `--timeout`, `--credentials`,
  `--endpoint`) are root `PersistentFlags`;
- `RunE` returns an error; `main` maps it once to a message and an exit code via
  `internal/exitcode`;
- `SilenceUsage/SilenceErrors` are enabled so runtime errors do not print usage;
- the command tree is built by constructors, without `init()` or global state;
- a use case receives dependencies through the `App` struct (writers, clock, API factory).

The full stack description is in [06-stack.md](../06-stack.md#6-cobra-conventions).

## Consequences

**Pros**

- adding a command is a local change: a file in `cli`, a use case in `app`, tests;
- consistent global flags and exit codes for all commands;
- completion and help without manual work;
- use cases are tested with a fake API and without cobra.

**Cons**

- the `cobra`/`pflag` dependency (moderate, stable);
- command description boilerplate (short/long/example);
- the temptation to put logic in `RunE` — prevented by review and the rule that `cli` does not
  construct the Play client.

**What this changes**

- `internal/cli` contains one file per command; `root.go` holds only global flags and groups;
- `internal/app` appears as a separate layer (three use cases in the MVP);
- CLI tests assert exit codes and JSON, not help text.

## When to revisit

- if there are so many commands that a plugin architecture (external binaries) is needed;
- if cobra gets in the way (for example, a fully declarative schema is required) — `kong` can
  replace it while keeping the `app`/`play` layers.
