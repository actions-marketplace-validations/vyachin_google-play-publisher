# 06 — Stack and project structure

This document fixes technology choices. The tool will grow with new commands, so the stack is
chosen for growth rather than for a single publish flow: a thin CLI layer, reusable use cases
and one adapter to the Play API.

## 1. Principles

1. **Standard library first.** An external dependency appears only when stdlib is clearly
   worse.
2. **Thin CLI.** A command parses flags and calls a use case; business logic and HTTP live
   outside `cobra`.
3. **One Play adapter.** All `androidpublisher` types live only in `internal/play`.
4. **Testability without network.** Use cases depend on interfaces, not on
   `*androidpublisher.Service`.
5. **One binary, static build.** Cross-platform releases via goreleaser.

## 2. Stack

| Layer | Choice | Why | When to revisit |
| --- | --- | --- | --- |
| CLI framework | [`spf13/cobra`](https://github.com/spf13/cobra) + `pflag` | Subcommands, groups, completion, man/help generation, maturity | A fully declarative schema could use `kong` |
| Configuration | Flags only (plus ADC env) | Minimal magic, predictable precedence | A config file would use `koanf` |
| Logging | `log/slog` (stdlib) | Structured logs, levels, JSON, zero dependencies | Practically never |
| Play API | `google.golang.org/api/androidpublisher/v3` | Full Edits API coverage, resumable upload | If the client lags — spot REST calls ([ADR-0001](adr/0001-androidpublisher-go-client.md)) |
| Authentication | ADC via `cloud.google.com/go/auth` (JSON key path) | Google standard, no hand-written OAuth | Not planned |
| Retries | A small package in `internal/app` (per-chunk retries come from the client) | Simple rules: 5xx/429, jitter, cap, "do not retry commit" | If the policy grows — `cenkalti/backoff/v5` |
| AAB | `archive/zip` + `crypto/sha256` | Inspection without bundletool | Manifest parsing would need protobuf + Android resources |
| JSON | `encoding/json` | Standard, stable tags | Not planned |
| Human output | `fmt` + `text/tabwriter` | Tables without TUI dependencies | Interactive UI would use `survey`/`bubbletea` |
| Errors/exit codes | `internal/exitcode` + `%w` | One place for error-to-code mapping | Not planned |
| Version | `runtime/debug` + `-ldflags` | Version and commit in the binary | Not planned |
| Tests | `testing`, `httptest`, golden files + `testify/require`, `go-cmp` | Minimal but convenient diffs | Not planned |
| Lint | `golangci-lint` (staticcheck, govet, errcheck, revive, gofumpt) | One tool in CI | Not planned |
| Build/release | `Makefile` + `goreleaser` | Cross-platform archives, checksums, changelog | A package manager would add a brew tap |
| CI | GitHub Actions | Already in use | Moving to GitLab CI |
| Dependency security | `govulncheck` + Dependabot | Vulnerabilities and updates | Not planned |

There are three direct runtime dependencies: `cobra`, `google.golang.org/api` and
`cloud.google.com/go/auth` (with `oauth2`). Everything else is stdlib. `testify`/`go-cmp` are
test-only.

## 3. Deliberately excluded

| Candidate | Why not |
| --- | --- |
| `spf13/viper` | Pulls config files, watch, formats; overkill for flags + env |
| `zap` / `logrus` | `slog` covers the needs; no second logger |
| `urfave/cli` | Cobra has a better subcommand and completion model |
| `wire` / `fx` | Manual DI: constructors and one `App` struct are simpler to debug |
| `mockgen` | Interfaces are small; hand-written fakes read better |
| TUI libraries | The MVP is a non-interactive CI CLI |

## 4. Command tree

```
google-play-publisher
├── publish     (group: Release)  — MVP
├── doctor      (group: Inspect)  — MVP
├── status      (group: Inspect)  — MVP
├── version                       — MVP
├── completion                    — from cobra, free
├── rollout     (group: Release)  — after the MVP
└── promote     (group: Release)  — after the MVP
```

Growth rule: a new command is one file in `internal/cli` plus a use case in `internal/app`
(when there is logic) plus tests. `internal/play` does not grow until a new API call is needed.

## 5. Package structure

```
cmd/google-play-publisher/     build App, Execute, os.Exit(code)
internal/cli/                  cobra: root + commands (thin)
  root.go                      PersistentFlags, groups, ExecuteContext
  publish.go                   flags → app.Publish
  doctor.go                    flags → app.Doctor
  status.go                    flags → app.Status
  version.go
internal/app/                  use cases, without cobra and androidpublisher
  publish.go                   preflight → insert → upload → track → commit
  doctor.go
  status.go
internal/play/                 Play API adapter: API interface + implementation
internal/artifact/             AAB inspection: ZIP, BundleConfig.pb, sha256, metadata
internal/release/              domain: Plan, status/rollout/notes validation
internal/retry/                (reserved) error classification and backoff
internal/logging/              slog, levels, debug transport, redaction
internal/exitcode/             typed errors → exit codes
internal/version/              version, commit, build date
```

Dependencies point inward: `cli → app → play/artifact/release`; `play` knows nothing about
`cli`; `app` knows nothing about `cobra` or `*androidpublisher.Service`.

## 6. Cobra conventions

- **No `init()`.** The tree is built by constructors: `NewRootCmd(app *App) *cobra.Command`.
- **Global flags are `PersistentFlags`:** `--debug`, `--json`, `--log-format`, `--timeout`,
  `--credentials`, `--endpoint`. Available before and after the command name.
- **`RunE`, not `Run`.** The error goes up; `main` prints the message once and calls
  `os.Exit`.
- **`SilenceUsage: true`, `SilenceErrors: true`.** Runtime errors do not print usage; usage is
  only printed for flag errors (code 2).
- **Argument validation** — `Args: cobra.NoArgs` (all parameters are flags).
- **Groups** — `cobra.Group`: `Release` and `Inspect`, so `help` reads like a product.
- **Completion** — `RegisterFlagCompletionFunc` for `--track` (currently a static list, later
  from the API).
- **Command tests** — `cmd.SetArgs`, `cmd.SetOut/SetErr`, `cmd.ExecuteContext`; assert exit
  codes and JSON, not help text.
- **Env binding** — not implemented in v0.1; only ADC and `NO_COLOR` are read from the
  environment.

## 7. App struct and dependency injection

```go
type App struct {
    Stdout, Stderr io.Writer
    Version        version.Info
    Now            func() time.Time
    Logger         *slog.Logger
    NewAPI         func(ctx context.Context, opts play.Options) (play.API, error) // lazy
}
```

- `main` creates `App` and passes it to `cli.NewRootCmd`.
- Commands receive `*App` instead of package globals.
- `play.API` is created lazily: `doctor` and `version` do not need a valid key when they do not
  use it.
- Clock and writers are replaced in tests.

## 8. Build and release

- `go build -trimpath -ldflags "-s -w -X .../internal/version.Version=... -X ...Commit=...
  -X ...Date=..."`.
- `goreleaser`: `linux/amd64`, `linux/arm64`, `darwin/arm64`, `darwin/amd64`,
  `windows/amd64`, `windows/arm64`; tar.gz/zip, `checksums.txt`, changelog from commits.
- `Makefile`: `build`, `test`, `test-race`, `lint`, `cover`, `snapshot`.
- Version is also available from `debug.ReadBuildInfo` (commit, dirty).

## 9. Dependency policy

- Pin versions in `go.mod`; update in a separate PR with tests and `govulncheck`.
- A new dependency is accepted if: it covers something stdlib does not; it is maintained; it
  does not pull a transitive forest; it is under ~1 MB of code or clearly worth it.
- Dependencies for "convenient sugar" in one place are forbidden.
- `go mod tidy` and `go mod verify` run in CI.

## 10. What comes next

| Need | Solution | Trigger |
| --- | --- | --- |
| Config file (multiple apps/tracks) | `koanf` + schema + validation | Introduction of `google-play-publisher.yaml` |
| Multiple apps per run | `errgroup` + concurrency limit | A `release --all` command |
| Interactive confirmation | `survey` or a simple prompt + `--yes` | `promote` to production |
| AAB manifest parsing | protobuf + Android `Resources.proto` | Validation before upload |
| Metrics/audit | JSON events on stderr + external collection | An audit requirement |
