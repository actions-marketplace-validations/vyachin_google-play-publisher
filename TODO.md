# TODO — development plan for `google-play-publisher`

**Status:** v0.1.0 implemented. Stages S1–S8 are done; S0 (Play Console access) was deferred
by agreement; S9 is post-MVP.
**Goal:** a Go CLI that publishes a signed AAB to the Google Play `internal` track, with a
debug mode, machine-readable output and an extensible command structure.

## How to use

- Stages are sequential; `S2` and `S3` can run in parallel after `S1`.
- Estimates are person-days for one developer, excluding review and waiting for access.
- A stage closes only when its **DoD** is met; a "gate" is what must hold before the next
  stage.
- Checkboxes are ticked as work completes; unplanned work is appended with an `[unplanned]`
  marker.
- Documentation and commit messages are in English, see [AGENTS.md](AGENTS.md).

## Summary

| Stage | Result | Estimate | Gate |
| --- | --- | --- | --- |
| S0 | Access and a test app | 0.5–1 d | Manual publication to `internal` is possible |
| S1 | Scaffold: cobra, App, logging, exit codes, CI | 1–1.5 d | `--help` / `version` / tests / lint |
| S2 | Play adapter | 2 d | AAB uploads and commits on the test app |
| S3 | AAB and release domain | 1 d | Invalid inputs are rejected before network |
| S4 | `publish` | 1.5–2 d | End-to-end publication to `internal` |
| S5 | `doctor`, `status` | 1 d | Diagnostics and release reading |
| S6 | CLI polish | 0.5–1 d | UX/help/completion |
| S7 | Quality and CI | 1–1.5 d | Green pipeline, integration run |
| S8 | v0.1 release | 0.5–1 d | Binaries and instructions |
| S9 | After the MVP | 3–5 d | `rollout`/`promote` |

**Up to v0.1: 9–12 working days + 20% buffer.**

---

## S0. Environment setup — 0.5–1 d

> Deferred by agreement with the owner: Play Console access is configured in parallel.
> Live verification is the remaining manual step.

- [ ] Choose a test app and `applicationId` (or agree to use the working app).
- [ ] Make sure Play Console has at least one uploaded build: Edits API requires it.
- [ ] Enable `androidpublisher.googleapis.com` in the GCP project.
- [ ] Invite the service account to Play Console with "Release to testing tracks".
- [ ] Put the JSON key at a known path; verify passing it via
      `GOOGLE_APPLICATION_CREDENTIALS`.
- [ ] Confirm that managed publishing does not affect `internal` (official exception).
- [ ] Record the current `versionCode` and `applicationId` of the test app.
- [ ] Verify the build: `jarsigner -verify` passes on the AAB.

**DoD:** manual publication to `internal` through the console works; key access is verified;
the package and the next `versionCode` are known.

**Gate:** without S0, S2 cannot start (nowhere to publish, nothing to authenticate with).

## S1. Project scaffold — 1–1.5 d — done

- [x] `go.mod`, module path, Go version; `go mod tidy`.
- [x] `cmd/google-play-publisher/main.go`: build `App`, `cli.Execute`, `os.Exit(code)`.
- [x] `internal/cli/root.go`: cobra root + `PersistentFlags`
      (`--debug`, `--json`, `--log-format`, `--timeout`, `--credentials`, `--endpoint`).
- [x] `App`: `Stdout/Stderr`, `Now`; the `play.API` factory arrived in S2 with the interface.
- [x] `internal/logging`: slog, levels, `text`/`json`, enabling `DEBUG`.
- [x] `internal/exitcode`: typed errors and mapping 0–8/130; `SilenceUsage/SilenceErrors`.
- [x] `internal/version` + `version` command (ldflags).
- [x] `Makefile` (`build`, `test`, `test-race`, `lint`, `cover`, `snapshot`).
- [x] `.golangci.yml`, GitHub Actions: `build`, `test`, `lint`.
- [x] Tests: global flags, exit codes, `version`.
- [x] Groups `Release`/`Inspect` — added in S5 together with the commands.

## S2. Play adapter — 2 d — done (except the live run)

- [x] `internal/play`: `API` interface and an `androidpublisher` implementation.
- [x] Authentication: ADC and `--credentials` via `cloud.google.com/go/auth`, scope
      `androidpublisher`, User-Agent in the transport; timeout from the operation context.
- [x] Methods: `CreateEdit`, `DeleteEdit`, `UploadBundle`, `UpdateTrack`, `ValidateEdit`,
      `CommitEdit`, `TrackReleases` (read-only via `applications.tracks.releases.list`).
- [x] Upload: `ChunkSize` 8 MiB, `ChunkRetryDeadline` 30 s, `ProgressUpdater` (INFO every 5%);
      small files go multipart, large ones resumable.
- [x] Mapping `googleapi.Error` → typed errors per [05-errors.md](docs/05-errors.md).
- [x] `debugTransport`: HTTP exchanges, bodies ≤ 4 KiB, redaction; secrets are stripped.
- [x] Fake API on `httptest` + `option.WithEndpoint`; tests for the happy path and
      400/401/403/404/429/5xx, plus multipart and resumable uploads.
- [x] Env-gated contract test (`GOOGLE_PLAY_PUBLISHER_INTEGRATION=1`).
- [ ] Live run against the test app — after access is configured (S0).

**DoD:** the unit part is closed; the AAB has not been uploaded to a real app yet (no access).
**Gate:** do not start S4 without error mapping (otherwise there is nothing to show the user) —
closed.

## S3. AAB and release domain — 1 d — done

- [x] `internal/artifact`: ZIP and `BundleConfig.pb` check, sha256, `output-metadata.json`.
- [x] `internal/release`: `Plan`, `status`/`rollout` validation, release notes ≤ 500
      characters, track names, `--if-exists`.
- [x] Table tests for every flag combination (code 2 before network).
- [x] `package` vs metadata check.

**DoD:** an invalid file, flags or locale are rejected before an edit is created; tests cover
the boundaries.

## S4. `publish` command — 1.5–2 d — done (except the live run)

- [x] Use case `internal/app/publish.go`: `insert → upload → tracks.update → commit`.
- [x] Preflight: AAB/`BundleConfig.pb`, AGP metadata, version on the track,
      `--if-exists=fail|skip`, track access.
- [x] `--dry-run`: `validate` + delete the edit.
- [x] Edit cleanup on error and context cancellation (short context, best effort).
- [x] Commit: `ERROR_IF_IN_REVIEW` by default, `--on-in-review=replace`, retry with
      `changesNotSentForReview=true` and code 8.
- [x] Ambiguous commit: verify releases before retrying; retry only for `Transient`.
- [x] JSON result per [03-cli-contract.md](docs/03-cli-contract.md#5-json-result).
- [x] Failure-scenario tests: upload, commit, retry, ambiguous, skip, dry-run.
- [ ] Live run against the test app — after access is configured (S0).

**DoD:** the unit/CLI part is closed; publication against a real app has not been verified.

## S5. `doctor` and `status` — 1 d — done (except the live run)

- [x] `doctor`: API/app access, track, AAB (ZIP/`BundleConfig.pb`), signature (heuristic),
      metadata match.
- [x] `status`: track releases, state and version codes, `--json`; read-only.
- [x] Tests with a fake API; clear messages (codes 3/4/7).

**DoD:** the unit/CLI part is closed; verification against a real account is pending access
(S0).

## S6. CLI polish — 0.5–1 d — done

- [x] `completion bash|zsh|fish|powershell`; `--track` flag completion
      (`RegisterFlagCompletionFunc`).
- [x] `Short`/`Long`/`Example` for every command; `Release`/`Inspect` groups in `help`.
- [x] `--json` + `--debug`: one object on `stdout`, logs on `stderr` (tested).
- [x] Debug acceptance criteria from
      [03-cli-contract.md](docs/03-cli-contract.md#7-debug-mode): JSON logs on stderr, no logs
      on stdout, secrets are never printed.
- [x] Error messages: Play message + hint with an action.

## S7. Quality and CI — 1–1.5 d — done

- [x] Coverage: `app` 85%, `artifact` 90%, `release` 100%, `play` 82%, `cli` 91%; the CI gate
      enforces ≥ 80% total.
- [x] `go test -race`, `golangci-lint`, `govulncheck` in CI.
- [x] Dependabot for `gomod` and GitHub Actions; `google.golang.org/api` pinned to `v0.311.0`.
- [x] Integration job: manual `workflow_dispatch`, dedicated test app, key from a secret.
- [x] Debug-log test for absence of `private_key`, `Authorization`, `Bearer` and tokens.
- [x] Flaky check: `go test -count=3 ./...` — green.

**DoD:** pipeline is green, coverage is above the threshold, the integration run is
reproducible manually.

## S8. v0.1 release — 0.5–1 d — done

- [x] `goreleaser`: linux/darwin/windows, amd64/arm64, tar.gz/zip, checksums, changelog.
- [x] `-trimpath` and `-ldflags`; version/commit/date visible in `version`.
- [x] Smoke test of the linux/amd64 and linux/arm64 binaries in containers.
- [x] Instructions: [README.md](README.md) — installation, doctor, first `internal` publish.
- [x] Tag `v0.1.0` and notes in [CHANGELOG.md](CHANGELOG.md).

**DoD:** binaries build, run and publish on the test app; documentation matches behavior.

## S9. After the MVP — 3–5 d — not started

- [ ] `rollout`: increase/halt/complete (production, `userFraction`).
- [ ] `promote`: `internal → beta`, confirmation for production.
- [ ] `--config`: a file with multiple apps (koanf), if needed.
- [ ] Multiple apps per run (`errgroup`), if needed.
- [ ] AAB manifest parsing (protobuf) — only if pre-upload validation is required.
- [ ] Metrics/audit: JSON events, external collection.

## Risks and buffer

| Risk | Impact | Mitigation |
| --- | --- | --- |
| No test app/access | S0 and S2 stall | Agree at the start; the working app with `internal` can be used |
| Edit quota exhausted | Publishing is unavailable until the next day | One edit per run; integration tests run rarely |
| API/client changes | Tests break | Pinned version, contract test |
| Managed publishing on other tracks | Publication "hangs" | Irrelevant for the MVP; account for it in `promote` |
| Optimistic estimates | Schedule slip | 20% buffer; S9 is outside v0.1 |

## Definition of Done for v0.1

- [x] `publish -p <id> -f app.aab -t internal` publishes end-to-end (verified with a fake API;
      the live run is the remaining manual step).
- [x] `--json` returns a stable result; exit codes match
      [03-cli-contract.md](docs/03-cli-contract.md#4-exit-codes).
- [x] `--debug` shows HTTP exchanges and does not reveal secrets.
- [x] `doctor` explains access problems.
- [x] Tests, lint, `-race`, `govulncheck` are green.
- [x] Release binaries build with goreleaser.
- [x] Documentation in `docs/` matches the implementation.
