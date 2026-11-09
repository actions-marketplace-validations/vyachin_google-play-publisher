# TODO — development plan for `google-play-publisher`

**Status:** v0.0.4 released. Stages S1–S8 and S8.1 are done; S0 (Play Console access) was deferred
by agreement; S9 is post-MVP. S10 (GitHub Actions usability) is analyzed and planned as
S10.0–S10.6; S10.0 (design) is done.
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
| S8.1 | v0.0.2: Gunclub migration (notes from changelogs, release name) | 1–1.5 d | v0.0.2 published with `--changelog-dir` |
| S8.2 | v0.0.3: follow-up review fixes (R11–R15, I5–I6) | — | v0.0.3 published with verified checksums |
| S9 | After the MVP | 3–5 d | `rollout`/`promote` |
| S10 | GitHub Actions setup/publish actions and AAB identity | 7.5–12 d | Actions tested without Play access; release published; Gunclub migrated |

**Up to v0.1: 9–12 working days + 20% buffer.** S10 is post-v0.1 and is not part of this
total.

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

## S8. v0.0.1 release — 0.5–1 d — done

- [x] `goreleaser`: linux/darwin/windows, amd64/arm64, tar.gz/zip, checksums, changelog.
- [x] `-trimpath` and `-ldflags`; version/commit/date visible in `version`.
- [x] Smoke test of the linux/amd64 and linux/arm64 binaries in containers.
- [x] Instructions: [README.md](README.md) — installation, doctor, first `internal` publish.
- [x] Tag `v0.0.1` and notes in [CHANGELOG.md](CHANGELOG.md).

**DoD:** binaries build, run and publish on the test app; documentation matches behavior.

## S9. After the MVP — 3–5 d — not started

- [ ] `rollout`: increase/halt/complete (production, `userFraction`).
- [ ] `promote`: `internal → beta`, confirmation for production.
- [ ] `--config`: a file with multiple apps (koanf), if needed.
- [ ] Multiple apps per run (`errgroup`), if needed.
- [ ] AAB manifest parsing (protobuf) — moved to S10.3 for CI metadata resolution.
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

## Code review — 2026-11-09 [unplanned]

Scope: Go implementation, tests, CLI/error contracts, and CI/release configuration.
`make fmt`, `make lint`, `make test` and `make test-race` passed. Temporary characterization tests
reproduced the ten findings below and were removed after review. No production code
was changed. Real Play publication was not exercised; S0/live verification remains open.
P1 means high priority because CI can make the wrong decision; P2 means normal priority.
Each fix must include regression tests and update the matching document in `docs/`.
Status 2026-11-09: R1–R10, I1, I2 and I4 are fixed with regression tests; I3 (live
verification) remains open until S0 access exists. The fixes are listed in the v0.0.2 section of
[CHANGELOG.md](CHANGELOG.md).

### Confirmed findings

- [x] **R1 — P1: Classify transport failures as transient errors.**
  Location: [internal/play/errors.go](internal/play/errors.go), `mapError`.
  Every error other than `*googleapi.Error` becomes `Internal` (1), including
  `context.DeadlineExceeded`, `io.EOF`, and `io.ErrUnexpectedEOF` (reproduced).
  This violates [the error catalog](docs/05-errors.md#2-error-catalog), prevents
  the transient commit retry, and gives CI the wrong recovery signal.
  Classify wrapped network/timeout errors with `errors.Is`/`errors.As`, preserve
  cancellation semantics, and distinguish token retrieval failures from API failures.
  Acceptance: timeout/EOF/reset errors return 6; authentication failures return 3;
  SIGINT/SIGTERM still return 130; ambiguous network commit failures use verification.

- [x] **R2 — P1: Return the documented manual-action exit code after saving changes.**
  Locations: [internal/app/publish.go](internal/app/publish.go), manual-review
  fallback; [internal/cli/publish.go](internal/cli/publish.go), `runPublish`.
  When the first commit requires manual review and the retry with
  `changesNotSentForReview=true` succeeds, the command returns 0 with
  `review=notSent` (reproduced). The contract requires 8 so CI can detect that
  a person must submit the saved changes.
  Preserve the committed result while returning a typed manual-action outcome;
  do not delete a successfully committed edit.
  Acceptance: this fallback returns 8, includes the saved version/edit in output,
  and explains the Play Console action. Cover explicit save-without-review behavior
  against [the exit-code contract](docs/03-cli-contract.md#4-exit-codes) as well.

- [x] **R3 — P1: Implement the JSON error contract.**
  Locations: [internal/cli/root.go](internal/cli/root.go), `Execute`;
  [internal/play/errors.go](internal/play/errors.go), `mapError`.
  `--json publish` with missing arguments returns an empty stdout and text stderr
  (reproduced); API failures in publish/status also bypass JSON rendering.
  Doctor failures can contain checks but omit the contracted `error` object.
  Moreover, mapping Google errors drops structured API code/reason information.
  Add one error-rendering path and retain structured error metadata, following
  [the JSON schema](docs/03-cli-contract.md#5-json-result).
  Acceptance: usage, authentication, API and interrupted failures produce exactly
  one JSON object with stable `error.code`, `error.exitCode`, message and hint;
  API code/reason are retained when available; doctor does not emit two objects.

- [x] **R4 — P2: Resolve the package from metadata as documented.**
  Location: [internal/cli/publish.go](internal/cli/publish.go), the initial required
  package check; [internal/app/publish.go](internal/app/publish.go), preflight order.
  The [publish flag contract](docs/03-cli-contract.md#2-commands) makes `--package`
  optional when metadata supplies `applicationId`. With a valid AAB and valid
  metadata but no `--package`, the CLI still returns usage code 2 and
  `--package is required` (reproduced).
  Read metadata and resolve the effective package before required-field/plan
  validation; retain the mismatch check when the package is explicitly supplied.
  Acceptance: metadata-only package selection succeeds, explicit mismatches fail
  locally, and omitting both metadata and package remains a usage error.

- [x] **R5 — P2: Report review state from the effective commit options.**
  Location: [internal/app/publish.go](internal/app/publish.go), lines assigning
  `result.Review` after verification or a transient retry.
  `review` is computed before the manual-action fallback changes
  `commitOpts.ChangesNotSentForReview`. The sequence manual-action error → transient
  error → successful retry reports `review=sent` although the successful request
  used `changesNotSentForReview=true` (reproduced).
  Derive the outcome from the options actually used, including verified ambiguous
  outcomes, and preserve the manual-action exit code.
  Acceptance: test both successful verification and a third successful commit
  after switching to save-without-review; neither can report `sent` or `replaced`.

- [x] **R6 — P2: Verify an ambiguous final commit attempt.**
  Location: [internal/app/publish.go](internal/app/publish.go), final transient
  commit retry and error return.
  If the first commit fails without applying changes and the retry applies them
  but loses its response, no second state check occurs. The CLI returns failure
  and attempts edit cleanup even though the release was saved (reproduced).
  Reconcile every ambiguous attempt before deciding the outcome. If verification
  is unavailable, report an unknown state with package, track, version and edit ID.
  Acceptance: a retry applied by the server but returning an error is recognized
  by a final read; successful reconciliation never attempts edit deletion.

- [x] **R7 — P2: Keep debug body capture transparent on read errors.**
  Location: [internal/play/transport.go](internal/play/transport.go), `captureBody`.
  If `io.ReadAll` consumes bytes and returns an error, the function returns the
  already-consumed original reader. A body returning JSON bytes together with
  `io.ErrUnexpectedEOF` becomes an empty reader with a clean EOF (reproduced).
  Debug mode can therefore change payloads and hide the original transport error.
  Use a bounded observing reader or replay captured bytes while preserving the
  error and the original body's close semantics.
  Acceptance: debug on/off deliver the same bytes and read error for partial reads;
  the body is closed correctly and capture never relies solely on Content-Length
  to enforce its memory bound.

- [x] **R8 — P2: Reject NaN rollout fractions locally.**
  Location: [internal/release/plan.go](internal/release/plan.go), `resolveStatus`.
  Both comparisons with NaN are false, so a NaN fraction passes validation and
  produces an `inProgress` plan (reproduced). The subsequent request cannot encode
  this JSON number, after publication setup has already begun.
  Explicitly reject non-finite fractions and keep the existing exclusive bounds.
  Acceptance: NaN, positive/negative infinity, zero and one return usage errors
  before API construction or any mutation; valid fractions still work.

- [x] **R9 — P2: Run local publish preflight before constructing the API client.**
  Location: [internal/cli/publish.go](internal/cli/publish.go), `runPublish`.
  Plan, AAB and metadata validation happen inside `Publisher.Publish`, after
  `newAPIClient`. With invalid local input and unavailable credentials, the command
  returns authentication code 3 instead of usage code 2 (reproduced with an injected
  failing factory). ADC discovery can also perform unnecessary external work.
  Expose an app-layer preflight and construct the client only after it succeeds,
  preserving the injected factory and the repository's layering.
  Acceptance: malformed package/track/status/notes, invalid AAB and mismatched
  metadata never invoke the API factory, including when credentials are unavailable.

- [x] **R10 — P2: Propagate doctor JSON output failures.**
  Location: [internal/cli/doctor.go](internal/cli/doctor.go), `runDoctor`.
  The return value of `writeDoctorJSON` is discarded. A stdout writer returning
  `io.ErrClosedPipe` still produces exit code 0 when all checks pass (reproduced).
  Return the output error when checks succeed; define precedence when both checks
  and output fail. Audit the equivalent ignored writes in human-readable output.
  Acceptance: a failed JSON write returns nonzero and a diagnostic, consistently
  with the publish/status JSON writers.

### Additional improvements

- [x] **I1 — P2: Implement the documented bounded retry policy.**
  The ordinary API methods in [internal/play/client.go](internal/play/client.go)
  issue one `.Do()` call; inspection of the pinned generated client shows a single
  `SendRequest` for ordinary requests. There is no application backoff or
  `Retry-After` handling. Chunk retries and the special commit retry cover only
  part of [the retry policy](docs/05-errors.md#3-retry-policy).
  Add context-aware retries for the documented safely repeatable operations,
  honoring limits and `Retry-After`; keep commit reconciliation separate.
  Validate with httptest sequences 503 → success, 429 with Retry-After, exhaustion,
  cancellation and non-retryable validation errors. Avoid real sleeps in tests.

- [x] **I2 — P2: Bind inspection and upload to the same artifact bytes.**
  [internal/artifact/artifact.go](internal/artifact/artifact.go) closes the file
  after inspection, and publish later reopens its path. A rebuild/replacement
  between those steps can upload different bytes while reporting the old size/hash.
  Use a stable artifact snapshot or verify the uploaded hash against the inspected
  bytes and reject a mismatch before committing. Add a file-replacement scenario.

- [ ] **I3 — P2: Keep release readiness and live verification evidence accurate.**
  Several completed DoD items above refer to behavior contradicted by this review,
  and the S8 DoD requires publication on a test app while S0/live runs remain open.
  [internal/play/live_test.go](internal/play/live_test.go) currently covers only
  create/read/delete, not upload/update/validate/commit. Separate local/fake gates
  from live gates and record evidence for each. Extend the opt-in live test against
  a dedicated test app when access and a signed test AAB are available.

- [x] **I4 — P2: Clarify the metadata prerequisite for idempotent skip.**
  [README.md](README.md) and the CLI contract advertise `--if-exists=skip` as an
  idempotent rerun, while [the architecture document](docs/02-architecture.md#6-idempotency-and-recovery)
  explicitly allows duplicate-upload code 5 without metadata. The implementation
  only checks existing versions when metadata is supplied; a fake-API probe
  confirmed that skip without metadata still enters the upload/update/commit path.
  Reconcile these contracts: document/enforce the metadata prerequisite or resolve
  identity from the AAB so skip also works without the sidecar. Test both variants.

## S8.1. v0.0.2 — Gunclub release pipeline migration [unplanned]

Context: the Gunclub repository (a separate checkout) replaces Fastlane with this tool. The
contract is the OpenSpec change `android/openspec/changes/replace-fastlane-with-go-publisher/`
there: read `design.md` (decisions D2 and D5) and `tasks.md` (group 1) before starting.
Gunclub's `.github/workflows/android.yml` needs the flags below, so v0.0.2 must be published
before that workflow switch is committed.

Task 1.1 of that change is implemented and committed (breaking CLI change, see the v0.0.2
section of [CHANGELOG.md](CHANGELOG.md)): `--release-notes` and its JSON reader are removed;
`--changelog-dir DIR` and `--version-code N` read
`<DIR>/<locale>/changelogs/<versionCode>.txt` before any edit is created, reject empty or
too long files with code 2 and the file path, and compare the uploaded bundle's version code
with `--version-code`; tests cover missing, empty and long files, two locales, a version-code
mismatch, the removed flag (unknown flag, code 2) and publishing without notes.

- [x] **M1 — P1: Add `--release-name`.**
  Pass the value to `tracks.update` through `play.Release.Name`, which the adapter already
  sends. An empty value must keep today's behavior (no release name).
  Acceptance: `publish --release-name 1.0.35` sends `1.0.35`; without the flag the release name
  stays empty; tests cover both.

- [x] **M2 — P1: Document the single release-notes source.**
  Files: [docs/03-cli-contract.md](docs/03-cli-contract.md) (flag table, examples, `--dry-run`
  note), [README.md](README.md) (Quick start, release notes), [CHANGELOG.md](CHANGELOG.md)
  (mark the removal of `--release-notes` as breaking, which is allowed in 0.x),
  [docs/02-architecture.md](docs/02-architecture.md) (the `--metadata` prerequisite) and
  `docs/README.md` (close the "Remaining questions" item about the notes format).
  Two corrections came out of the Gunclub migration and belong in the docs: AGP does not write
  `build/outputs/bundle/<variant>/output-metadata.json` for `bundleRelease` (the listing file
  stays under `build/intermediates/bundle_ide_model/...`), so `--metadata` must not be
  advertised for AAB publishing; and `--dry-run` really uploads the bundle, so its version code
  may stay used after the edit is deleted.
  Acceptance: `grep -rn -- "--release-notes" docs README.md` is empty and `CHANGELOG.md`
  mentions the removal only as a breaking change; the docs describe `--changelog-dir`,
  `--version-code` and `--release-name`; `make lint` is clean and every link in
  `docs/README.md` still resolves.

- [x] **M3 — P1: Run the local gate.**
  `make fmt`, `make lint`, `make test`, `make test-race`.
  Acceptance: all four pass; `make cover` reports at least 80% total, matching the CI gate.

- [x] **M4 — P1: Publish v0.0.2.**
  Commit, tag, run goreleaser through the existing release workflow.
  Acceptance: the GitHub release for `v0.0.2` contains
  `google-play-publisher_0.0.2_linux_amd64.tar.gz` and `checksums.txt`, and the checksum in
  that file matches `sha256sum` of the downloaded archive. Release notes name the breaking
  change: `--release-notes` is removed, notes now come from `--changelog-dir`.
  Evidence: [release v0.0.2](https://github.com/vyachin/google-play-publisher/releases/tag/v0.0.2),
  release workflow run 42585932356; `checksums.txt` matches the downloaded
  `google-play-publisher_0.0.2_linux_amd64.tar.gz`
  (`9b7f64a6ea6e0de277c98b544475d6c0ae8cf6adc77a1643daefc0169542552b`).

**DoD:** v0.0.2 is published with `--changelog-dir`, `--version-code` and `--release-name`;
Gunclub's `android.yml` can install the binary and publish with
`--changelog-dir metadata --version-code "$VERSION_CODE"`.
**Gate:** do not commit Gunclub's workflow switch before this release exists; it calls the new
flags.

## S8.2. v0.0.3 — follow-up review fixes [unplanned]

Delivers the R11–R15 and I5–I6 fixes from the follow-up review; the user-facing changes are
listed in the v0.0.3 section of [CHANGELOG.md](CHANGELOG.md).

- [x] **M5 — P1: Publish v0.0.3.**
  Tag `v0.0.3` from `chore(release): prepare v0.0.3`; the release workflow builds and uploads the
  archives.
  Acceptance: the GitHub release for `v0.0.3` contains
  `google-play-publisher_0.0.3_linux_amd64.tar.gz` and `checksums.txt`, and the checksum in that
  file matches `sha256` of the downloaded archive.
  Evidence: [release v0.0.3](https://github.com/vyachin/google-play-publisher/releases/tag/v0.0.3),
  release workflow run 42593559185; `checksums.txt` matches the downloaded
  `google-play-publisher_0.0.3_linux_amd64.tar.gz`
  (`1547f2e0bddf8ce8d676d14dfd174d639bbb252d3970afc70b75b40e199ebc19`).

**DoD:** v0.0.3 is published with the follow-up review fixes and the documented checksum
verification.
**Gate:** none; the next functional work is S9 or the remaining live verification (I3).

## Follow-up code review — 2026-11-09 [unplanned]

Scope: current Go implementation, tests, documentation, and CI/release configuration,
including the fixes recorded in the earlier review. No production code was changed.
Five temporary characterization tests reproduced R11–R15 and were removed after review.
`make fmt`, `make lint`, `make test`, and `make test-race` passed. Race tests required
running outside the sandbox because its listener restriction blocked httptest.
Live Play publishing was not exercised; I3 remains open.
P1 means high priority; P2 means normal priority. Each fix must add regression tests
and update the matching behavior/error document under `docs/`.

### Confirmed findings

- [x] **R11 — P1: Redact secrets that cross the debug capture boundary.**
  Location: [internal/play/transport.go](internal/play/transport.go), `captureBody`,
  `secretJSON`, and `redact`.
  Capture truncates the input to 4 KiB + 1 byte before redaction. The regular expression
  requires a closing quote, so a secret value extending beyond the captured prefix
  is logged verbatim. Reproduced with a JSON `access_token` containing 5,000 characters:
  the debug copy contains the token prefix instead of `[redacted]`.
  Escaped quotes also require proper JSON string handling rather than the current regex.
  Make redaction safe for incomplete JSON and escaped strings; suppress body logging
  when safe redaction cannot be established, while preserving replay and memory bounds.
  Acceptance: secrets beginning before or at the capture limit, long values, escaped
  quotes, and partial-read payloads never expose any secret value in logs.

- [x] **R12 — P1: Preserve cancellation and deadlines during token acquisition.**
  Location: [internal/play/client.go](internal/play/client.go), `New` and `tokenSource`.
  The production path wraps the context-aware cloud auth provider with
  `oauth2adapt.TokenSourceFromTokenProvider`. In the pinned adapter, `Token()` invokes
  the provider with `context.Background()`. `oauth2.NewClient(ctx, ts)` does not restore
  the request deadline for that call. Reproduced with the same adapter/client pipeline:
  a request with a 5 ms deadline waits approximately 81 ms for a test token provider,
  which receives no deadline. A stalled token endpoint can therefore delay timeout
  handling and shutdown even though ordinary Play requests use the operation context.
  Use a transport/provider integration that passes the operation context to token
  acquisition and refresh. Keep the injected API factory and existing error mapping.
  Acceptance: a blocked fake token endpoint/provider is interrupted by both the
  operation deadline and cancellation, including refresh; no real credentials required.

- [x] **R13 — P2: Reject conflicting explicit and metadata version codes before networking.**
  Location: [internal/app/publish.go](internal/app/publish.go), `Prepare`,
  `knownVersionCode`, and the existing-version branch in `PublishPrepared`.
  `Prepare` validates the metadata package but does not compare its version with
  `PublishOptions.VersionCode`. The explicit value wins during the existing-version
  check, so metadata version 41 plus `--version-code 42 --if-exists=skip` returns
  success when 42 is on the track. Without a skip, the contradiction is discovered
  only after an edit and upload, although it is detectable locally.
  Acceptance: reject conflicting positive version codes with exit code 2 before
  API construction, even with `--if-exists=skip`; matching values continue to work.

- [x] **R14 — P2: Print the required operator action for JSON manual-review outcomes.**
  Location: [internal/cli/publish.go](internal/cli/publish.go), `reportManualAction`;
  [internal/cli/root.go](internal/cli/root.go), JSON `renderedError` handling.
  The automatic save-without-review fallback returns exit code 8 and one success
  JSON object, but wrapping its error as `renderedError` suppresses the final error
  and hint on stderr. The INFO log only says that the changes were saved; it does
  not tell the operator to submit them. This contradicts the explicit stderr action
  contract in [03-cli-contract.md](docs/03-cli-contract.md#5-json-result).
  Acceptance: keep exactly one saved-release JSON object and exit code 8, and print
  the manual submission instruction on stderr exactly once.

- [x] **R15 — P2: Classify non-timeout connectivity failures consistently.**
  Location: [internal/play/errors.go](internal/play/errors.go), `mapError`.
  The transport-error allowlist covers timeout, EOF, reset, and broken pipe but
  omits connection refusal and unreachable-network/host failures. Reproduced with
  `net.OpError` wrapping `syscall.ECONNREFUSED`: the result is Internal (1), so the
  bounded transient retry policy is bypassed and CI receives the wrong category.
  Acceptance: classify recoverable connectivity failures as Transient (6), add
  wrapped-error cases and retry coverage, and preserve Auth/Interrupted semantics.
  Do not indiscriminately retry permanent TLS/certificate or malformed-URL errors.

### Additional improvements

- [x] **I5 — P2: Enforce shared publish invariants in the app layer.**
  `Prepare` claims to validate every local input, but policy validation and
  metadata-based package resolution are partly CLI-only. A direct `Publisher.Publish`
  caller can pass unsupported `IfExists`/`InReviewBehavior` values or combine
  `DryRun` with `ChangesNotSentForReview`; `PublishPrepared` also accepts a separate,
  potentially inconsistent options/prepared pair. Centralize invariants and consider
  an opaque prepared operation carrying all validated options. Keep CLI parsing
  focused on collecting input and formatting output. Test direct app callers.

- [x] **I6 — P2: Propagate human-readable output errors.**
  The earlier R10 fix propagates doctor JSON failures, but `printPublish`,
  `printStatus`, and `printChecks` still discard writer errors, including
  `tabwriter.Flush`. A closed pipe or full output device can leave exit code 0
  despite a missing result. Return and propagate output errors consistently; keep
  saved-release context and clearly define precedence over manual-action errors.
  Add failing-writer tests for text output without rerunning a publication.

**Status 2026-11-09:** R11–R15 and I5–I6 are fixed with regression tests; I3 (live
verification) remains open until S0 access exists. The fixes are listed in the v0.0.3
section of [CHANGELOG.md](CHANGELOG.md).

## S10. GitHub Actions usability [unplanned] — analyzed, planned

**Goal:** replace repeated binary-installation and credentials scripts with official actions,
then remove Gradle-source parsing by resolving release identity from the signed AAB.
Build, signing, Android checks, artifact retention, and workflow concurrency remain explicit
responsibilities of the consuming workflow.

**Status 2026-11-09:** the proposals were reviewed against v0.0.3 and turned into S10.0–S10.6.
The interfaces remain proposals, not features available in v0.0.3. S10.1–S10.2 add actions on
top of the existing CLI; S10.3–S10.4 extend the CLI without changing its layering; S10.5–S10.6
document, verify and release the result. S10 can proceed independently of the S9
rollout/promote work. S10.0 is accepted in
[ADR-0004](docs/adr/0004-github-actions.md); implementation continues with S10.1.

### Analysis

Reusable pieces that already exist:

- goreleaser publishes `google-play-publisher_<version>_<os>_<arch>.tar.gz` for
  linux/darwin/windows amd64/arm64 plus `checksums.txt` ([.goreleaser.yaml](.goreleaser.yaml)),
  so the setup action only has to download, verify and expose an asset.
- `--json` writes exactly one stable object to `stdout`
  ([03-cli-contract.md](docs/03-cli-contract.md#5-json-result)), so an action can capture
  `versionCode`, `editId` and `review` without parsing human output.
- local validation already happens before authentication (`app.Prepare`), which keeps action
  tests possible without Play credentials; the fake API in `internal/play/playtest`, the
  injected API factory and `--endpoint` keep tests network-free.
- the Gunclub workflow installs the binary with a curl/checksum/tar script and parses Gradle
  sources for the version code; S10.1 and S10.3 replace both.

Gaps in the original proposals, addressed below:

1. No design gate: composite actions have no `pre`/`post` steps, so credential cleanup,
   outputs and summary behavior must be designed before the YAML is written.
2. No release/versioning stage: an action ref and a CLI version are different coordinates.
   Documentation may reference released refs only, and Gunclub must not switch before the
   release that contains the actions exists (the S8.1 gate repeats here).
3. AAB identity is not trivial: the bundle stores `base/manifest/AndroidManifest.xml` as
   protobuf XML (`aapt.pb.XmlNode`), not binary AXML. The parser choice, `versionCodeMajor`,
   resource references and malformed manifests need explicit decisions and fixtures.
4. `artifact.Inspect` closes the file and publish reopens the path, so the resolved identity
   and the uploaded bytes are not guaranteed to describe the same inode; I2 detects a
   replacement only after the upload.
5. Changelog lookup lives in [internal/cli/changelog.go](internal/cli/changelog.go) before
   `app.Prepare`, so an AAB-derived version code cannot drive it until the lookup moves into
   the app layer.
6. Credential input modes, masking, path resolution and cleanup on failure or cancellation are
   unspecified; a composite action cannot rely on `post`.
7. Action outputs for success, skip, manual review (exit code 8) and failure are undefined;
   parsing JSON adds a `jq` dependency that must be validated.
8. There is no CI tooling for action YAML and scripts (lint, shell tests, runner matrix), and
   live verification remains blocked by S0.

**Estimate:** 7.5–12 person-days for S10.0–S10.6, excluding live verification (S0/I3);
with the 20% buffer: 9–14.5 days.

**Critical path:** S10.0 → S10.3 → S10.4 → S10.6. S10.1 and S10.2 can be delivered in
parallel with S10.3, because the first publish action keeps an explicit `--version-code`.

| Stage | Estimate | Depends on |
| --- | --- | --- |
| S10.0 | 0.5–1 d | — |
| S10.1 | 1–1.5 d | S10.0 |
| S10.2 | 1.5–2 d | S10.0, S10.1 |
| S10.3 | 2.5–3.5 d | S10.0; parallel with S10.1 |
| S10.4 | 0.5–1 d | S10.3 |
| S10.5 | 1.5–2 d | S10.1–S10.4 |
| S10.6 | 0.5–1 d | S10.5 and a published release |

### S10.0. Design gate — 0.5–1 d — done

- [x] **D1 — Action packaging.** Use composite actions with bash entry points
      (`setup/action.yml`, root `action.yml`, shared scripts) so there is no Node build step
      and the CLI remains the only component that talks to Play. Record that composite actions
      have no `pre`/`post`: cleanup is a `trap` inside the same `run` step plus a documented
      `$RUNNER_TEMP` lifecycle. Revisit a JavaScript action only if guaranteed cleanup on
      cancellation or shared cross-platform logic becomes a requirement.
- [x] **D2 — Version and ref policy.** Accept `X.Y.Z` and `vX.Y.Z` for the CLI version input;
      reject ranges, branches and `latest`. Use `github.action_ref` when the runner exposes it
      and it is a release tag; otherwise require an explicit input. Decide whether the
      repository ever gets a floating major tag and do not advertise one before it exists;
      document pinning by release tag and by commit SHA.
- [x] **D3 — AAB identity source.** Read `base/manifest/AndroidManifest.xml` from the AAB ZIP.
      Compare a hand-written `protowire` decoder for `aapt.pb.XmlNode`/`XmlElement`/
      `XmlAttribute`/`Item` with generated code from a vendored AOSP `Resources.proto`
      (Apache-2.0); choose the smaller option that keeps the schema readable and testable.
      Handle `versionCodeMajor`, reject resource references (`@string/...`) for
      `versionCode`/`versionName` with exit code 2 and a hint, and bound the decompressed
      manifest size. Add fixtures from real Gradle/bundletool AABs.
- [x] **D4 — Identity precedence and compatibility.** The AAB is authoritative for values not
      passed explicitly; explicit `--package`/`--version-code` must match it, and `--metadata`
      stays an optional legacy cross-check. Decide whether `versionName` becomes the default
      `--release-name` and how an explicitly empty name is expressed. Decide the new JSON
      fields (`versionName`, identity source) and whether they need a `schemaVersion` bump.
- [x] **D5 — Artifact identity.** Open the AAB once, keep the `*os.File` inside the opaque
      `app.Prepared`, compute size and sha256 from it and upload from the same handle; close
      it on every path, including `--dry-run` and errors. Cover a path replacement during a
      run.
- [x] **D6 — Credentials lifecycle.** Support mutually exclusive `credentials-base64` and
      `credentials-file` inputs and fall back to ADC when neither is set; write decoded
      material to a `umask 077` file under `$RUNNER_TEMP`, mask both raw and encoded values,
      never print contents, and clean up on success, failure and termination. Document the
      self-hosted caveat: hosted runners wipe `$RUNNER_TEMP`, self-hosted runners may not.
- [x] **D7 — Outputs and exit codes.** Always write the CLI JSON result to a file and expose
      `version-code`, `edit-id`, `review`, `result-file`, `skipped`, `manual-action` and
      `error-code`; keep the CLI exit status, including 8; write a Job Summary that explains
      manual submission. Decide whether a summary-write failure fails the action
      (recommended: no, log a warning).
- [x] **D8 — Platform scope.** Support Linux amd64/arm64 on `ubuntu-latest` and
      `ubuntu-24.04-arm`; fail fast with a clear message on other OS/architectures. Treat
      macOS/Windows action support as a separate follow-up even though the binaries exist.

**DoD:** D1–D8 are recorded in [ADR-0004](docs/adr/0004-github-actions.md); the behavior
documents are updated by the stages that implement each decision (S10.3, S10.5). No action
implementation started before the decisions were settled.

### S10.1. Provide a setup action — 1–1.5 d

- [ ] Fix the action contract and supported runners in the new guide under `docs/` (completed
      in S10.5), linked from `docs/README.md`. Start with Linux amd64/arm64; reject
      unsupported platforms explicitly.
- [ ] Add `setup/action.yml`, usable as `vyachin/google-play-publisher/setup@<ref>`, with an
      explicit CLI `version` input and `version`/`path` outputs. Document action revision and
      CLI version separately.
- [ ] Implement the downloader in a shared shell script: normalize the version (strip a
      leading `v` for the asset name but keep the tag form for the release URL), map
      `uname -s`/`uname -m` to the goreleaser asset name (`x86_64` → `amd64`,
      `aarch64` → `arm64`), download the archive and `checksums.txt` with
      `curl --fail --location --retry`, verify the exact asset line's SHA-256 before
      extraction, install under `$RUNNER_TEMP`, and append the directory to `GITHUB_PATH`.
      Fail clearly for a missing release, asset or checksum. Allow a test-only base-URL
      override so CI fixtures do not depend on GitHub.
- [ ] Test platform selection, version validation (`v` prefix, garbage, ranges), checksum and
      404 failures, paths containing spaces, and successful installation without Play
      credentials. Run the smoke test on `ubuntu-latest` and `ubuntu-24.04-arm` against the
      latest published release, never against an unreleased version.
- [ ] Add `actionlint`, `shellcheck` and the script tests to CI (wired in S10.5).

**DoD:** a setup step replaces the current curl/checksum/tar script, and subsequent `run`
steps invoke the requested `google-play-publisher` version.

### S10.2. Provide a publish action over the existing CLI — 1.5–2 d

- [ ] Add root `action.yml`, usable as `vyachin/google-play-publisher@<ref>`. Reuse the setup
      script through `$GITHUB_ACTION_PATH`; delegate publication behavior to the CLI; do not
      duplicate Play API calls or release validation in action scripts.
- [ ] Define validated inputs: CLI version, package, AAB path, track (default `internal`),
      changelog directory, version code, release name, existing-version policy (default
      `fail`), expected tag (wired in S10.4), credentials base64/file, timeout and debug.
      Inputs stay strings and are validated in the script; pass them to the CLI as quoted
      array elements, never through `eval` or interpolated shell source.
- [ ] Implement the publish script as one `run` step with `trap ... EXIT`: resolve relative
      paths against `GITHUB_WORKSPACE` (document that `defaults.run.working-directory` does
      not affect action inputs), materialize credentials as decided in D6, call
      `google-play-publisher --json ... > "$RESULT_FILE"`, preserve the exit code, publish
      outputs, and write `$GITHUB_STEP_SUMMARY` with package, track, version, edit, review and
      the manual-submission instruction for exit code 8.
- [ ] Define output semantics for success, idempotent skip (no `editId`), saved-without-review
      and failure (error JSON when available); expose the result file as an
      artifact-friendly path.
- [ ] Keep `doctor` available as setup plus a separate CLI step before the expensive build;
      do not add an implicit access check to the publish action.
- [ ] Test input forwarding, both credential modes and cleanup, paths containing spaces and
      shell metacharacters, success, skip, ordinary failure and exit code 8 with a stub CLI
      (no live uploads); assert secrets are masked and absent from logs and summaries.

**DoD:** a single publish step replaces installation, Google Play key decoding and the shell
publish command in the supplied Android workflow; build and signing steps stay explicit.

### S10.3. Resolve release identity from the AAB in the CLI — 2.5–3.5 d

- [ ] Record the D3/D4 decisions and implement `artifact.Identity` (`applicationId`,
      `versionCode`, `versionName`, source path) from `base/manifest/AndroidManifest.xml`.
      Reject malformed or missing identity fields with exit code 2 and a hint, before any API
      client is constructed.
- [ ] Handle `versionCodeMajor`, duplicate attributes, namespace-qualified names and resource
      references explicitly; add fixtures for a minimal bundle, a real AGP bundle, and
      malformed manifests (truncated, missing manifest, bad protobuf, oversized manifest).
- [ ] Move changelog lookup from `internal/cli` into the app/release layer so it runs after
      identity resolution and before authentication; keep missing, empty and too-long file
      errors local with exit code 2 and the checked paths.
- [ ] Extend `app.Prepare`/`Prepared`: resolve package and version code from the AAB (explicit
      flags must agree), keep `--metadata` as an optional cross-check, apply
      `--if-exists=skip` without a metadata sidecar (closes the remaining I4 concern), and
      validate `--expected-tag` before the API client exists.
- [ ] Open the AAB once and upload from the same `*os.File` (D5), preserving size, sha256 and
      the existing post-upload hash check; close the handle on every path and add a
      file-replacement test.
- [ ] Add the optional `--version-code` behavior and `--expected-tag` to the CLI contract, the
      architecture document, README and CHANGELOG; describe the compatibility change
      (`--version-code` still verifies when supplied) explicitly. Apply the JSON/schema
      decision from D4.
- [ ] Test conflicts (AAB vs flag, AAB vs metadata, AAB vs package), changelog selection from
      the AAB, skip without metadata, malformed identity, tag mismatch and direct app
      callers; assert every local failure happens before API construction or upload.

**DoD:** publishing with `--changelog-dir` resolves the version code from the AAB without
parsing `build.gradle.kts` or depending on an AGP intermediate metadata path, and the uploaded
bytes are the inspected bytes.

### S10.4. Validate the expected tag and simplify action inputs — 0.5–1 d

- [ ] Define the `--expected-tag` contract: exact comparison against `v<versionName>` from the
      AAB, exit code 2 when the tag or `versionName` is missing or mismatched, before API
      construction and upload. Document the behavior when `versionName` is absent.
- [ ] Add the `expected-tag` action input and pass `${{ github.ref_name }}` in the documented
      tag-triggered workflow; keep the input empty by default for branch runs.
- [ ] Make the `version-code` and `release-name` action inputs optional according to D4,
      keeping `package` as an explicit guard against publishing the wrong app.
- [ ] Update the documented workflow to drop Gradle `sed` parsing and duplicate changelog
      validation once the released CLI provides equivalent checks.
- [ ] Test matching and mismatched tags, a missing version name, explicit overrides, and
      failures before upload; test that omitting the version code selects the right changelog
      files.

**DoD:** the target workflow supplies the signed AAB, expected package, changelog directory,
credentials and release tag; the tool validates their consistency before uploading.

### S10.5. Document and verify the GitHub Actions integration — 1.5–2 d

- [ ] Create `docs/07-github-actions.md` and link it from `docs/README.md`; add a short example
      and link to README. Also add the action ADR to the `docs/README.md` decision list. Cover
      the support matrix, setup usage, publish usage, inputs and outputs, both credential
      modes and ADC, path semantics, version/action pinning, concurrency, managed publishing,
      artifact retention and troubleshooting.
- [ ] Publish a complete, copyable Android GitHub Actions workflow: checkout, Java setup,
      Android checks, signing/build, publication, per-app concurrency and artifact retention.
      Show explicit `android/...` paths for a subdirectory project and both usage patterns:
      setup plus CLI (including an early `doctor`) and the publish action.
- [ ] Document the initial explicit-version workflow after S10.2, then update the example to
      the AAB-derived workflow after S10.4. Use released refs and versions that actually
      exist; do not present a proposed floating tag or an unreleased CLI version as available.
- [ ] Add CI smoke coverage for both actions on `ubuntu-latest` and `ubuntu-24.04-arm` using a
      published CLI version; validate workflow and action YAML (`actionlint` plus a schema
      check for `action.yml`), lint the scripts with `shellcheck`, and check documentation
      links.
- [ ] Verify the stub-based action tests require no Play credentials and record live
      verification separately. When S0 credentials and a dedicated signed test AAB are
      available, run the opt-in live publication through the action; never consume a
      production version code for a dry run.
- [ ] Run `make fmt`, `make lint`, `make test`, `make test-race` and `make cover` for the CLI
      changes; keep total coverage above the 80% gate. Add CHANGELOG entries for the actions
      and for every CLI behavior change in the same release.

**DoD:** the guide and both action interfaces are complete and verified with stub-based tests;
setup-only usage, authentication, path semantics, outputs and failure handling are explained;
local stub verification and pending live verification have separate recorded statuses. The
release itself is S10.6.

### S10.6. Release the CLI and switch the Gunclub workflow — 0.5–1 d

- [ ] After S10.3/S10.4 are merged, cut the next CLI release (for example `v0.0.4`): tag, run
      the release workflow, verify the archives and `checksums.txt` exactly as in M4/M5, and
      record the evidence here.
- [ ] Publish the action code at the release tag and confirm the setup action installs that
      release on both supported runner architectures; record the exact action ref used in the
      documentation.
- [ ] Update Gunclub's `.github/workflows/android.yml`: replace the curl/checksum/tar script
      with the setup or publish action, remove Gradle `sed` parsing and duplicate changelog
      validation, and add a per-app concurrency group. Keep build and signing explicit.
- [ ] Run the Gunclub workflow with stub/local checks; record live publication evidence only
      when S0 access exists. Do not reference the action tag before the release that contains
      it exists (same gate as S8.1).

**DoD:** the next release ships the actions and AAB identity; Gunclub's workflow uses them and
no longer parses Gradle sources or installs the binary with ad-hoc scripts.
**Gate:** live evidence is blocked by S0; local/stub evidence is required for closure.

### S10 risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| AAB manifest format and edge cases | S10.3 slips or resolves a wrong version code | Fixtures from real AABs, explicit rejection of unsupported forms, `--version-code` override |
| Composite action has no `post` | Credentials may survive on self-hosted runners | Single-step `trap`, `$RUNNER_TEMP`, masking, documented caveat, JavaScript action if required |
| Action ref and CLI version diverge | Confusing or unreproducible failures | Explicit version input, release policy, tests against published versions only |
| Runner labels or images change | CI coverage gaps | Pin `ubuntu-24.04`/`ubuntu-24.04-arm` in the matrix and keep the support matrix current |
| Shell injection or bad path handling | Secret leaks or the wrong file published | Environment plus arrays, quoted paths, metacharacter tests, workspace-relative resolution |
| `jq` missing on self-hosted runners | Outputs and summary unavailable | Detect and fail clearly, document the prerequisite, always keep the raw result file |
| No Play access (S0) | Live verification blocked | Stub/fake-endpoint tests; track live evidence separately under I3 |
| Breaking CLI change in 0.x | Consumer friction | CHANGELOG entry, updated docs, defaults that preserve behavior when flags are supplied |
| Gunclub switches too early | Broken workflow | Release gate in S10.6, same as S8.1 |

**S10 DoD:** setup and publish actions work on Linux amd64/arm64 with checksum verification and
safe credentials; the publish action preserves CLI exit codes and exposes documented outputs;
`--changelog-dir` and `--if-exists=skip` work from AAB identity without Gradle parsing; docs
are verified against a released ref; local/stub and live verification statuses are recorded
separately.
