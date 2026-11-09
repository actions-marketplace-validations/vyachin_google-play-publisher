# Project rules

These rules apply to every change in this repository.

## Language

- **All documentation is written in English.** This includes `README.md`, `CHANGELOG.md`,
  `TODO.md`, everything under `docs/`, ADRs and inline documentation.
- Code, identifiers, comments, CLI help, error messages and log messages are in English.
- Commit messages are in English and follow Conventional Commits.

## Naming

- The project, the Go module and the executable are all named `google-play-publisher`.
- The module path is `github.com/vyachin/google-play-publisher`.
- The binary is built from `cmd/google-play-publisher` and is always called
  `google-play-publisher` in commands, scripts, archives and documentation.

## Documentation

- `docs/` is the source of truth for design, behavior, setup and error handling.
- A change in behavior must update the matching document in the same commit.
- Internal links between documents must keep working after edits.
- The development plan lives in `TODO.md`; completed stages are marked there.

## Code

- Keep the layering: `cli -> app -> play/artifact/release`. The `cli` layer never constructs
  the Play client directly; it receives a factory through `App`.
- Prefer the standard library; add dependencies only when they clearly pay off.
- Every behavior change comes with tests; run `make fmt`, `make lint`, `make test`.
