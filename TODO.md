# TODO — development plan for `google-play-publisher`

**Status:** v0.1.2 released. Stages S1–S8, S8.1, S8.2, S10, S11, S12, S13 and the unplanned
S14 (metadata pull), S15 (single metadata flag), S16 (text-only logs), S17 (no CLI endpoint
override), S19 (Marketplace branding) and S20 (maintenance release v0.1.2) are done; S0 (Play
Console access) was deferred by agreement, and the former S9 post-MVP list was dropped as out
of scope. The post-S10 review (R16–R19, I7–I9)
is fixed and released, including the removal of `--expected-tag` in S11.8; S11.9 fixes the
publish action invocation and supersedes v0.0.6. The downstream workflow switch is done in the
consuming repository, and the live publish/upload runs succeeded against a real app; the
remaining open items are the S0 configuration checklist and the I3 evidence update.
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
| S8.1 | v0.0.2: downstream migration (notes from changelogs, release name) | 1–1.5 d | v0.0.2 published with `--changelog-dir` |
| S8.2 | v0.0.3: follow-up review fixes (R11–R15, I5–I6) | — | v0.0.3 published with verified checksums |
| S10 | GitHub Actions setup/publish actions and AAB identity | 7.5–12 d | Actions tested without Play access; v0.0.5 released; downstream switch done |
| S11 | Post-S10 review fixes: commit outcomes, doctor identity, action result files | 3.5–6 d | R16–R19 and I7–I9 fixed with tests/docs; v0.0.6 released |
| S12 | Release fields, `rollout` and `promote` | 1.5–2.5 d | Priority/targeting on publish; track changes without a new upload |
| S13 | Store metadata in the publish edit | 1.5–2 d | Listings, images and testers committed with the AAB |
| S14 | `pull`: recreate the metadata tree from Play | 1–1.5 d | A pulled tree round-trips through `publish --metadata-dir` |
| S15 | Consolidate metadata flags on `--metadata-dir` | 0.5 d | One metadata flag; `promote` applies the full tree |
| S16 | Remove JSON log format | 0.25 d | One JSON flag: `--json` for the result only |
| S17 | Remove the CLI endpoint override | 0.25 d | Production flags cannot redirect the Play client |
| S18 | Release v0.1.0 | 0.25 d | Tagged release with verified archives and checksums |
| S19 | Release v0.1.1 with Marketplace branding | 0.25 d | Action badge is publishable in Marketplace |
| S20 | Maintenance release v0.1.2 | 0.25 d | Dependency bump and README restructure shipped |

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

## S2. Play adapter — 2 d — done

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
- [x] Live run against the test app — repeated AAB uploads against a real app succeeded.

**DoD:** the unit part and the live upload against the test app are closed.
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

## S4. `publish` command — 1.5–2 d — done

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
- [x] Live run against the test app — repeated end-to-end publications succeeded.

**DoD:** the unit/CLI part and publication against a real app are both verified.

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

## Risks and buffer

| Risk | Impact | Mitigation |
| --- | --- | --- |
| No test app/access | S0 and S2 stall | Agree at the start; the working app with `internal` can be used |
| Edit quota exhausted | Publishing is unavailable until the next day | One edit per run; integration tests run rarely |
| API/client changes | Tests break | Pinned version, contract test |
| Managed publishing on other tracks | Publication "hangs" | Irrelevant for the MVP |
| Optimistic estimates | Schedule slip | 20% buffer; work beyond the MVP is out of scope |

## Definition of Done for v0.1

- [x] `publish -p <id> -f app.aab -t internal` publishes end-to-end (covered by tests with a
      fake API and confirmed by repeated live runs against a real app).
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
  idempotent rerun, while [the architecture document](docs/02-architecture.md#7-idempotency-and-recovery)
  explicitly allows duplicate-upload code 5 without metadata. The implementation
  only checks existing versions when metadata is supplied; a fake-API probe
  confirmed that skip without metadata still enters the upload/update/commit path.
  Reconcile these contracts: document/enforce the metadata prerequisite or resolve
  identity from the AAB so skip also works without the sidecar. Test both variants.

## S8.1. v0.0.2 — Downstream release pipeline migration [unplanned]

Context: the downstream repository (a separate checkout) replaces Fastlane with this tool. The
contract is the OpenSpec change `android/openspec/changes/replace-fastlane-with-go-publisher/`
there: read `design.md` (decisions D2 and D5) and `tasks.md` (group 1) before starting.
The downstream repository's `.github/workflows/android.yml` needs the flags below, so v0.0.2
must be published before that workflow switch is committed.

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
  Two corrections came out of the downstream migration and belong in the docs: AGP does not
  write `build/outputs/bundle/<variant>/output-metadata.json` for `bundleRelease` (the listing
  file stays under `build/intermediates/bundle_ide_model/...`), so `--metadata` must not be
  advertised for AAB publishing; and `--dry-run` really uploads the bundle, so its version
  code may stay used after the edit is deleted.
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
the downstream repository's `android.yml` can install the binary and publish with
`--changelog-dir metadata --version-code "$VERSION_CODE"`.
**Gate:** do not commit the downstream repository's workflow switch before this release exists;
it calls the new flags.

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
**Gate:** none; the remaining work is live verification (I3).

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
document, verify and release the result. S10.0 is accepted in
[ADR-0004](docs/adr/0004-github-actions.md); S10.1–S10.6 are implemented and v0.0.5 is
released. The downstream workflow switch was performed in the consuming repository, and live
publication has been exercised against a real app.

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
- the downstream workflow installs the binary with a curl/checksum/tar script and parses Gradle
  sources for the version code; S10.1 and S10.3 replace both.

Gaps in the original proposals, addressed below:

1. No design gate: composite actions have no `pre`/`post` steps, so credential cleanup,
   outputs and summary behavior must be designed before the YAML is written.
2. No release/versioning stage: an action ref and a CLI version are different coordinates.
   Documentation may reference released refs only, and the downstream workflow must not switch
   before the release that contains the actions exists (the S8.1 gate repeats here).
3. AAB identity is not trivial: the bundle stores `base/manifest/AndroidManifest.xml` as
   protobuf XML (`aapt.pb.XmlNode`), not binary AXML. The parser choice, `versionCodeMajor`,
   resource references and malformed manifests need explicit decisions and fixtures.
4. `artifact.Inspect` closes the file and publish reopens the path, so the resolved identity
   and the uploaded bytes are not guaranteed to describe the same inode; I2 detects a
   replacement only after the upload.
5. Changelog lookup lived in the CLI before `app.Prepare`, so an AAB-derived version code
   could not drive it until the lookup moved into [internal/app/changelog.go](internal/app/changelog.go).
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

### S10.1. Provide a setup action — 1–1.5 d — done

- [x] Fix the action contract and supported runners in the new
      [GitHub Actions guide](docs/07-github-actions.md), linked from `docs/README.md`. Start
      with Linux amd64/arm64; reject unsupported platforms explicitly.
- [x] Add [setup/action.yml](setup/action.yml), usable as
      `vyachin/google-play-publisher/setup@<ref>`, with an explicit CLI `version` input and
      `version`/`path` outputs. Document action revision and CLI version separately.
- [x] Implement the downloader in [scripts/setup.sh](scripts/setup.sh): normalize the version
      (strip a leading `v` for the asset name but keep the tag form for the release URL), map
      `uname -s`/`uname -m` to the goreleaser asset name (`x86_64` → `amd64`,
      `aarch64` → `arm64`), download the archive and `checksums.txt` with
      `curl --fail --location --retry`, verify the exact asset line's SHA-256 before
      extraction, install under `$RUNNER_TEMP`, and append the directory to `GITHUB_PATH`.
      Fail clearly for a missing release, asset or checksum. Allow a test-only base-URL
      override so CI fixtures do not depend on GitHub.
- [x] Test platform selection, version validation (`v` prefix, garbage, ranges), checksum and
      404 failures, paths containing spaces, and successful installation without Play
      credentials in [scripts/setup_test.sh](scripts/setup_test.sh). Runner smoke tests
      against a published release are part of S10.5.
- [x] Add `actionlint`, `shellcheck` and the script tests to CI.

**DoD:** a setup step replaces the current curl/checksum/tar script, and subsequent `run`
steps invoke the requested `google-play-publisher` version.

### S10.2. Provide a publish action over the existing CLI — 1.5–2 d — done

- [x] Add root [action.yml](action.yml), usable as `vyachin/google-play-publisher@<ref>`.
      Reuse the setup script through `$GITHUB_ACTION_PATH`; delegate publication behavior to
      the CLI; do not duplicate Play API calls or release validation in action scripts.
- [x] Define validated inputs: CLI version, package, AAB path, track (default `internal`),
      changelog directory, version code, release name, existing-version policy (default
      `fail`), credentials base64/file, timeout and debug; the expected-tag input was wired in
      S10.4 and removed in S11.8. Inputs stay strings and are validated in the script; they are passed to the CLI
      as quoted array elements, never through `eval` or interpolated shell source.
- [x] Implement [scripts/publish.sh](scripts/publish.sh) as one `run` step with
      `trap ... EXIT`: resolve relative paths against `GITHUB_WORKSPACE` (document that
      `defaults.run.working-directory` does not affect action inputs), materialize
      credentials as decided in D6, call `google-play-publisher --json ... > "$RESULT_FILE"`,
      preserve the exit code, publish outputs, and write `$GITHUB_STEP_SUMMARY` with package,
      track, version, edit, review and the manual-submission instruction for exit code 8.
- [x] Define output semantics for success, idempotent skip (no `editId`), saved-without-review
      and failure (error JSON when available); expose the result file as an
      artifact-friendly path.
- [x] Keep `doctor` available as setup plus a separate CLI step before the expensive build;
      do not add an implicit access check to the publish action.
- [x] Test input forwarding, both credential modes and cleanup, paths containing spaces,
      success, skip, ordinary failure and exit code 8 with a stub CLI
      ([scripts/publish_test.sh](scripts/publish_test.sh), no live uploads); assert secrets are
      masked and absent from logs and summaries.

**DoD:** a single publish step replaces installation, Google Play key decoding and the shell
publish command in the supplied Android workflow; build and signing steps stay explicit. The
downstream workflow switch itself was performed in the consuming repository (see S10.6).

### S10.3. Resolve release identity from the AAB in the CLI — 2.5–3.5 d — done

- [x] Record the D3/D4 decisions and implement `artifact.Identity` (`applicationId`,
      `versionCode`, `versionName`) from `base/manifest/AndroidManifest.xml`. Reject malformed
      or missing identity fields with exit code 2 and a hint, before any API client is
      constructed.
- [x] Handle `versionCodeMajor`, duplicate attributes, namespace-qualified names and resource
      references explicitly; add fixtures for a minimal bundle, a real aapt2 bundle, and
      malformed manifests (truncated, missing manifest, bad protobuf, oversized manifest).
- [x] Move changelog lookup from `internal/cli` into the app layer so it runs after identity
      resolution and before authentication; keep missing, empty and too-long file errors local
      with exit code 2 and the checked paths.
- [x] Extend `app.Prepare`/`Prepared`: resolve package and version code from the AAB (explicit
      flags must agree), keep `--metadata` as an optional cross-check, and apply
      `--if-exists=skip` without a metadata sidecar (closes the remaining I4 concern).
- [x] Open the AAB once and upload from the same `*os.File` (D5), preserving size, sha256 and
      the existing post-upload hash check; close the handle on every path and add a
      file-replacement test.
- [x] Add the optional `--version-code` behavior to the CLI contract, the architecture
      document, README and CHANGELOG; describe the compatibility change (`--version-code`
      still verifies when supplied) explicitly. Apply the JSON/schema decision from D4
      (`schemaVersion: 1`, `versionName`).
- [x] Test conflicts (AAB vs flag, AAB vs metadata, AAB vs package), changelog selection from
      the AAB, skip without metadata, malformed identity and direct app callers; assert every
      local failure happens before API construction or upload.

**DoD:** publishing with `--changelog-dir` resolves the version code from the AAB without
parsing `build.gradle.kts` or depending on an AGP intermediate metadata path, and the uploaded
bytes are the inspected bytes.

### S10.4. Validate the expected tag and simplify action inputs — 0.5–1 d — done

> Superseded by S11.8: the owner removed `--expected-tag` and its action input; the AAB
> `versionName` is still reported in logs and JSON.

- [x] Define the `--expected-tag` contract: exact comparison against `v<versionName>` from the
      AAB, exit code 2 when the tag or `versionName` is missing or mismatched, before API
      construction and upload. Document the behavior when `versionName` is absent.
- [x] Add the `expected-tag` action input and pass `${{ github.ref_name }}` in the documented
      tag-triggered workflow; keep the input empty by default for branch runs.
- [x] Keep the `version-code` and `release-name` action inputs optional according to D4,
      keeping `package` as an explicit guard against publishing the wrong app.
- [x] Drop Gradle `sed` parsing and duplicate changelog validation from the documented
      workflow; the full copyable workflow is completed in S10.5.
- [x] Test matching and mismatched tags, a missing version name, explicit overrides, and
      failures before upload; test that omitting the version code selects the right changelog
      files.

**DoD:** the target workflow supplies the signed AAB, expected package, changelog directory,
credentials and release tag; the tool validates their consistency before uploading.

### S10.5. Document and verify the GitHub Actions integration — 1.5–2 d — done

- [x] Create [docs/07-github-actions.md](docs/07-github-actions.md) and link it from
      `docs/README.md`; add a short example and link to README. Also add the action ADR to the
      `docs/README.md` decision list. Cover the support matrix, setup usage, publish usage,
      inputs and outputs, both credential modes and ADC, path semantics, version/action
      pinning, concurrency, managed publishing, artifact retention and troubleshooting.
- [x] Publish complete Android GitHub Actions workflows: checkout, Java setup, signing/build,
      publication, per-app concurrency and artifact retention. Show explicit `android/...`
      paths for a subdirectory project and both usage patterns: setup plus CLI (including an
      early `doctor`) and the publish action.
- [x] Document the AAB-derived workflows after S10.4. The examples used placeholders until
      S10.6 published the release and now pin `v0.0.5` and CLI `0.0.5`; no floating tag or
      unreleased version is presented as available.
- [x] Add CI smoke coverage for both actions on `ubuntu-latest` and `ubuntu-24.04-arm` using a
      published CLI version; validate workflow and action YAML (`actionlint` plus
      `@action-validator/cli`), lint the scripts with `shellcheck`, and check documentation
      links with `scripts/check_links.py`.
- [x] Verify the stub-based action tests require no Play credentials and record live
      verification separately. When S0 credentials and a dedicated signed test AAB are
      available, run the opt-in live publication through the action; never consume a
      production version code for a dry run.
- [x] Run `make fmt`, `make lint`, `make test`, `make test-race` and `make cover` for the CLI
      changes; total coverage is 81.1%, above the 80% gate. CHANGELOG entries cover the
      actions and every CLI behavior change in the release.

**DoD:** the guide and both action interfaces are complete and verified with stub-based tests;
setup-only usage, authentication, path semantics, outputs and failure handling are explained;
local stub verification and pending live verification have separate recorded statuses. S10.6
replaced the placeholders with `v0.0.5`/`0.0.5` and verified the release artifacts.

### S10.6. Release the CLI and switch the downstream workflow — 0.5–1 d — done

- [x] Cut the next CLI release `v0.0.5`: tag, run the release workflow, verify the archives
      and `checksums.txt` exactly as in M4/M5, and record the evidence here.
- [x] Publish the action code at `v0.0.5`; the setup script installed the released
      `linux_amd64` archive locally, and the CI smoke job covers `ubuntu-latest` and
      `ubuntu-24.04-arm`. The documentation pins `v0.0.5` and CLI `0.0.5`.
- [x] Update the downstream repository's `.github/workflows/android.yml`: done in the consuming
      repository (a separate checkout). The switch replaced the curl/checksum/tar installation
      with the setup/publish action, removed the Gradle `sed` parsing and the duplicate
      changelog validation, and moved release notes to `--changelog-dir metadata`.
- [x] Run the downstream workflow with stub/local checks: done; the workflow was exercised
      against a released action tag. Live publication evidence is recorded in that repository
      and in the S0/live-verification items here.

**DoD:** `v0.0.5` ships the actions and AAB identity, the documentation pins the released ref,
and the downstream workflow uses the setup/publish action with the AAB-derived identity.
**Gate:** closed; the follow-up work is live evidence for I3, not the workflow switch.

**Evidence:** [release v0.0.5](https://github.com/vyachin/google-play-publisher/releases/tag/v0.0.5),
release workflow run `42612148749` (success, 2m57s). `checksums.txt` contains
`230e0e814eb90f792731b71691cd6433f00486b3dca115e5113f2eaf9dfe382b` for
`google-play-publisher_0.0.5_linux_amd64.tar.gz`; the setup script downloaded and verified that
archive. The released `darwin_arm64` binary reports version `0.0.5`, commit `83f9ad6`, and
exposes `--expected-tag` and the AAB-derived `--version-code` help text (the tag flag was
removed in S11.8).

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
| The downstream workflow switches too early | Broken workflow | Release gate in S10.6, same as S8.1; the switch used a released action tag |

**S10 DoD:** setup and publish actions work on Linux amd64/arm64 with checksum verification and
safe credentials; the publish action preserves CLI exit codes and exposes documented outputs;
`--changelog-dir` and `--if-exists=skip` work from AAB identity without Gradle parsing; docs
are verified against `v0.0.5`; local/stub and live verification statuses are recorded
separately. The downstream workflow switch was completed in the consuming repository.

## Post-S10 code review — 2026-11-09 [unplanned]

Scope: publication recovery, CLI output, doctor validation, and repeated publish-action
invocations. All items below remain open. The review passed `make test`, `make lint`, and
`make test-race`; temporary tests outside the repository reproduced R16–R18 through a Go
overlay. R19 was identified by code inspection. No live Play publication was performed.
Each behavior fix must include regression tests and update the matching document in
`docs/`; run `make fmt`, `make lint`, and `make test` when implementing it.

**Status 2026-11-09:** R16–R19 and I7–I9 were re-checked against the working tree and all
seven findings were confirmed; the analysis and the execution plan are in S11 below. The
fixes are implemented with regression tests and released as v0.0.6 (see S11).

### Confirmed findings

- [x] **R16 — P1: Do not turn a definitive commit rejection into publication success.**
  Location: [internal/app/publish.go](internal/app/publish.go), `commit` and `isReleased`.
  Reconciliation runs after definitive errors as well as ambiguous failures and treats
  the presence of a version code as proof that the current operation succeeded. If another
  operation puts the version on the track, a rejected commit can return success and
  `review=sent` even when the observed lifecycle state is `NOT_SENT_FOR_REVIEW`.
  Preserve definitive rejections; reconcile ambiguous transport/server failures and account
  for the observed release lifecycle state without claiming an unverified submission.
  Acceptance: a conflict followed by an externally appearing version remains an error;
  an ambiguous commit cannot report `sent` solely from version presence when the observed
  state is not sent for review; successful ambiguous-commit recovery remains covered.

- [x] **R17 — P2: Preserve exactly one JSON object when doctor is interrupted.**
  Location: [internal/cli/root.go](internal/cli/root.go), `Execute`.
  Doctor writes its JSON result and returns `renderedError`, but cancellation replaces the
  error and loses that marker. Execute then writes a second JSON object to stdout.
  Preserve the output-rendered state independently of error replacement, or centralize final
  JSON rendering. Acceptance: cancellation during `doctor --json` returns 130, writes exactly
  one JSON object, and retains the available diagnostic checks.

- [x] **R18 — P2: Check doctor inputs against the AAB manifest identity.**
  Location: [internal/app/doctor.go](internal/app/doctor.go), artifact and metadata checks.
  `artifact.Inspect` does not parse the manifest, so doctor accepts an AAB for a different
  application. Metadata is checked against the package flag but not the AAB version code.
  Use `artifact.Open`, compare package and metadata identity against the manifest, and share
  local identity validation with publish where practical. Acceptance: reject wrong-package,
  mismatched-version, missing-manifest, and malformed-manifest inputs; close the AAB on every
  path and keep valid doctor runs working.

- [x] **R19 — P2: Keep a separate result file for every publish-action invocation.**
  Location: [scripts/publish.sh](scripts/publish.sh), `result_dir` and `result_file`.
  Every invocation uses `$RUNNER_TEMP/google-play-publisher/result.json`. A second action in
  the same job deletes and replaces the first result, so the earlier `result-file` output
  points to another publication's JSON. Create an invocation-specific directory with
  `mktemp -d` and preserve its result for downstream steps.
  Acceptance: two sequential invocations with the same runner temporary root expose distinct
  result paths, both original payloads remain readable, and temporary credentials are still
  removed.

### Additional improvements

- [x] **I7 — P2: Model commit outcomes explicitly.**
  Location: [internal/app/publish.go](internal/app/publish.go), commit recovery and cleanup.
  Distinguish `committed`, `notCommitted`, and `unknown` instead of relying only on the
  `committed` boolean. Define cleanup and reporting for each outcome so an unverified commit
  is not treated as a confirmed rejection. Acceptance: tests cover successful commit,
  definitive rejection, failed reconciliation, and the cleanup policy for unknown state;
  document the chosen recovery policy in [the error guide](docs/05-errors.md).

- [x] **I8 — P2: Define reconciliation behavior after the operation deadline expires.**
  Location: [internal/app/publish.go](internal/app/publish.go), `commit`.
  Reconciliation currently reuses the commit context, so a deadline-expired context cannot
  perform the network check. Choose and document either a short separate recovery budget
  or an explicit unknown-state result with the package, track, version code, and edit ID.
  Acceptance: a commit response lost at the deadline follows the documented bounded recovery
  policy, reports uncertainty accurately, and preserves signal cancellation semantics.

- [x] **I9 — P2: Enforce the one-shot lifecycle of Prepared before networking.**
  Location: [internal/app/publish.go](internal/app/publish.go), `Prepared.Close` and
  `PublishPrepared`.
  Closing Prepared leaves `valid` true. Reusing it can perform preflight and create an edit
  before discovering the closed file. Track closed/consumed state shared by copies and reject
  reuse before any API call. Acceptance: publishing a closed or already-consumed Prepared
  fails locally without creating an edit; normal publication and repeated Close remain safe.

## S11. Post-S10 review fixes [unplanned] — done

**Status 2026-11-09:** the seven review items were re-checked against the working tree
(`f2117b4` plus the review notes) and all are confirmed. S11.0–S11.6 are implemented with
regression tests, S11.8 removes `--expected-tag` by owner decision, and S11.9 fixes the
publish action. v0.0.7 is released and supersedes v0.0.6; the local gate is green and the
release assets and checksums are verified (see S11.7 and S11.9).

**Goal:** close the post-S10 review, keep the documented CLI/exit-code contract accurate, and
ship the fixes as v0.0.6.

**Out of scope:** I3/S0 live verification (blocked by Play Console access) and the downstream
workflow switch (outside this repository).

### Analysis

| Item | Priority | Confirmed in | Evidence |
| --- | --- | --- | --- |
| R16 | P1 | [internal/app/publish.go](internal/app/publish.go), `commit`, `isReleased` | Reconciliation runs for every commit error, and success is decided by version presence alone; the returned `review` comes from the request options, not the observed lifecycle state |
| I7 | P2 | [internal/app/publish.go](internal/app/publish.go), `PublishPrepared`, `commit` | A single `committed` bool drives cleanup, so an unverified commit is deleted as if it were a rejection |
| I8 | P2 | [internal/app/publish.go](internal/app/publish.go), `commit` | `isReleased` receives the commit context; after `DeadlineExceeded` reconciliation cannot read the track |
| R17 | P2 | [internal/cli/root.go](internal/cli/root.go), `Execute`; [internal/cli/doctor.go](internal/cli/doctor.go), `runDoctor` | The cancellation branch replaces `err` after doctor returned `renderedError`, so a second JSON object is written |
| R18 | P2 | [internal/app/doctor.go](internal/app/doctor.go), artifact/metadata checks | `artifact.Inspect` skips the manifest; metadata is compared with the flag only, never with the AAB version code |
| R19 | P2 | [scripts/publish.sh](scripts/publish.sh), `result_dir`, `result_file` | The result path is fixed under `$RUNNER_TEMP`, so a later invocation overwrites the earlier payload |
| I9 | P2 | [internal/app/publish.go](internal/app/publish.go), `Prepared.Close`, `PublishPrepared` | `valid` stays true after `Close`; a copy of `Prepared` shares only the file pointer, so reuse fails late with code 1 after an edit exists |

Work grouping, chosen to avoid rewriting the same code twice:

- **Group A — commit state machine (R16, I7, I8).** One code path, one documentation section
  ([05-errors.md §4](docs/05-errors.md#4-ambiguous-commit)) and one test matrix. R16 and I7
  are refactored together; I8 is a small follow-up on the same function.
- **Group B — local validation (R18).** Reuses `artifact.Open` and extracts the identity checks
  currently inlined in `Prepare`. The existing doctor fixtures build AABs without a manifest
  (for example `signedAAB` in [internal/cli/doctor_test.go](internal/cli/doctor_test.go)), so
  they must be updated together with the behavior.
- **Group C — output contract (R17).** Only the top-level execution path in
  [internal/cli/root.go](internal/cli/root.go); independent of Group A.
- **Group D — action script (R19).** Shell-only, independent, testable without Go changes.
- **Group E — `Prepared` lifecycle (I9).** Small and independent, but touches the type that
  Group A also reads.

### Decisions to settle in S11.0

Recommended choices; the stage promotes them to `docs/` as each fix lands.

- **D1 — definitive versus ambiguous commit failures (R16) — agreed.** Definitive failures
  keep their exit code and skip reconciliation: usage (2), authentication (3), not found (4),
  conflict or invalid edit (5), Play validation (7). Only failures typed by the Play adapter
  as transport or server-side failures are reconciled: transient (6), and internal (1) only
  when the error carries that transport marker. A token retrieval failure happens before the
  commit request is sent, is not marked, and receives no reconciliation. The manual-review
  refusal keeps the existing one-time `changesNotSentForReview=true` fallback. Implemented as
  `play.AmbiguousFailure` / `play.IsAmbiguous`.
- **D2 — observation rules (R16) — agreed with one exception.** Reconciliation evaluates the
  version code together with the observed release lifecycle state. `PUBLISHED`/`IN_REVIEW`
  mean success. Presence with `NOT_SENT_FOR_REVIEW` means saved without review: exit code 8,
  `review=notSent` and the Play Console instruction. Exception: an explicit
  `--changes-not-sent-for-review` keeps exit code 0 as the current contract requires, and an
  explicit `--status draft` also stays 0. Presence with an `UNSPECIFIED` state keeps the
  historical version-presence rule and logs a WARN that the state did not confirm the review
  outcome; it is a compatibility fallback, not evidence of submission. Documented in
  [05-errors.md §4](docs/05-errors.md#4-ambiguous-commit).
- **D3 — explicit commit outcomes and cleanup (I7).** Replace the `committed` bool with
  `committed`, `notCommitted` and `unknown`. Only `notCommitted` deletes the edit. `unknown`
  keeps the edit (it may already be closed), reports code 6 with package, track, version code
  and edit ID, and never emits a success object in `--json`.
- **D4 — recovery budget after the deadline (I8) — agreed.** When the commit context is
  expired, reconcile under a shared 15 s `context.WithTimeout` budget. The budget is parented
  by the signal context, not by the expired operation context (`PublisherOption`
  `WithRecoveryBase`), so a signal interrupts an already running recovery; a signal before
  recovery keeps 130, performs no read and does not delete an edit with an unknown outcome.
  Before the commit attempt the normal cleanup is unchanged. An exhausted budget is `unknown`
  with package/track/versionCode/editID and keeps the edit.
- **D5 — JSON rendering ownership (R17).** Track "output already rendered" independently of
  error replacement in `Execute`, or centralize final JSON rendering so doctor cancellation
  still yields exit code 130 and exactly one object with the collected checks.
- **D6 — doctor identity (R18) — agreed.** Extract the local identity validation from
  `Prepare` into a shared app helper; doctor opens the AAB with `artifact.Open`, requires
  manifest identity when `--aab` is passed and compares `--package` and `--metadata` (package
  and version code) against it. The API check stays first, and without `--aab` access and
  track diagnostics are unchanged. The bundle is closed on every path.
- **D7 — result files (R19).** Create an invocation directory with
  `mktemp -d "$tmp_root/google-play-publisher.XXXXXX"`, keep `result.json` and the decoded
  credentials inside it, and let the `EXIT` trap remove only credentials and stderr; the result
  stays readable for downstream steps and `result-file` keeps its meaning.
- **D8 — `Prepared` lifecycle (I9).** Share `valid`/`closed`/`consumed` state through a pointer
  so copies agree; `Close` stays idempotent; `PublishPrepared` rejects zero, closed and
  consumed values with code 1 before any API call and without creating an edit.

### S11.0. Baseline and decisions — 0.5 d

- [x] Re-run the local baseline: `make fmt`, `make lint`, `make test`, `make test-race`,
      `make cover`, `scripts/setup_test.sh` and `scripts/publish_test.sh`; record the starting
      coverage so the 80% gate can be compared after the fixes.
- [x] Settle D1–D8 and keep them in this section until each behavior document is updated.
- [x] Map the affected contracts: [05-errors.md §4](docs/05-errors.md#4-ambiguous-commit),
      [03-cli-contract.md §5](docs/03-cli-contract.md#5-json-result) and
      [§4](docs/03-cli-contract.md#4-exit-codes),
      [03-cli-contract.md doctor](docs/03-cli-contract.md#google-play-publisher-doctor--mvp),
      [07-github-actions.md § Outputs and artifacts](docs/07-github-actions.md#outputs-and-artifacts)
      and the doctor sections of [README.md](README.md).

**DoD:** every decision has one chosen option and one document that will record it; no code
changes yet.

### S11.1. Commit outcome model — R16 + I7 — 1–1.5 d

- [x] Replace the `committed` bool and the deferred delete with an explicit outcome
      (D3) shared by reporting and cleanup; only `notCommitted` deletes the edit.
- [x] Reconcile only ambiguous failures (D1); a definitive rejection can no longer become
      success when another operation puts the version on the track.
- [x] Derive success and `review` from the request actually sent plus the observed lifecycle
      state (D2); a release that exists but was not submitted returns code 8 with
      `review=notSent` and the manual-submission hint.
- [x] Return an `unknown` outcome with code 6, package, track, version code and edit ID when
      reconciliation is inconclusive; keep the edit and emit no success object in `--json`.
- [x] Extend the publish tests to set `ReleaseSummary.State` and cover: conflict plus an
      externally appearing version (code 5, no success); ambiguous commit observed as
      `NOT_SENT_FOR_REVIEW` (code 8, `notSent`, edit kept); ambiguous commit observed as
      `IN_REVIEW`/`PUBLISHED` (code 0, `sent`); failed reconciliation (unknown, edit kept);
      definitive rejection (edit deleted, original code). Keep the existing manual-review and
      transient-retry cases as regression tests.
- [x] Update [05-errors.md §4](docs/05-errors.md#4-ambiguous-commit) and the `review` wording in
      [03-cli-contract.md §5](docs/03-cli-contract.md#5-json-result) in the same commit.

**DoD:** every commit outcome has a test and a documented cleanup/exit-code rule; no definitive
rejection or unsent release is ever reported as success.

### S11.2. Reconciling after the operation deadline — I8 — 0.25–0.5 d

- [x] Reconcile an expired-deadline commit under the bounded recovery budget (D4), with a
      single documented attempt policy and a DEBUG/WARN log.
- [x] Tests: a commit that fails as the deadline expires but was applied is recovered by the
      new read (success or code 8 per the observed state); a signal-canceled run returns 130,
      skips recovery and keeps the edit; an exhausted recovery budget returns `unknown`.
- [x] Document the recovery budget and its interaction with `--timeout` in
      [05-errors.md §4](docs/05-errors.md#4-ambiguous-commit).

**DoD:** a lost response at the deadline follows one documented policy, uncertainty is reported
honestly, and cancellation semantics stay intact.

### S11.3. `Prepared` one-shot lifecycle — I9 — 0.25–0.5 d

- [x] Share `valid`/`closed`/`consumed` through a pointer (D8) so copies and the CLI's deferred
      `Close` agree; keep `Close` idempotent.
- [x] Reject zero, closed and consumed values before `TrackReleases` or `CreateEdit`, with a
      local code 1 and a hint to call `Prepare` again.
- [x] Tests: publish after `Close` creates no edit; a second `PublishPrepared` with the same
      value is rejected; normal publication and repeated `Close` still pass.

**DoD:** reuse fails locally and predictably, before any network call or edit.

### S11.4. Doctor validates the AAB identity — R18 — 0.5–1 d

- [x] Extract the identity checks from `Prepare` into a shared app helper and use it from both
      publish and doctor (D6), keeping the existing error messages and exit codes.
- [x] Doctor: open the AAB with `artifact.Open`, require manifest identity when `--aab` is
      given, compare `--package` and both metadata fields against it, and close the bundle on
      every path without a double close.
- [x] Update doctor fixtures to include `base/manifest/AndroidManifest.xml` via
      `artifacttest.Manifest`; add wrong-package, mismatched-version, missing-manifest and
      malformed-manifest cases, plus a valid run that still reports all checks.
- [x] Update the doctor section of [03-cli-contract.md](docs/03-cli-contract.md#google-play-publisher-doctor--mvp),
      the doctor description in [README.md](README.md) and the CHANGELOG entry.

**DoD:** doctor rejects an AAB for another application or version locally, valid runs keep
working, and publish/doctor share one identity implementation.

### S11.5. Per-invocation result files — R19 — 0.25–0.5 d

- [x] Create an invocation-specific directory with `mktemp -d` (D7); place `result.json` and
      decoded credentials there; keep the `EXIT` trap limited to credentials and stderr.
- [x] Extend [scripts/publish_test.sh](scripts/publish_test.sh): two sequential invocations
      with the same `RUNNER_TEMP` expose distinct result paths, both payloads stay readable,
      and temporary credentials are still removed.
- [x] Update [07-github-actions.md § Outputs and artifacts](docs/07-github-actions.md#outputs-and-artifacts)
      with the result-file lifetime and the CHANGELOG entry.

**DoD:** a second publish action cannot overwrite the first result file, and no credentials
survive the step.

### S11.6. One JSON object on doctor cancellation — R17 — 0.25–0.5 d

- [x] Preserve the rendered-output state independently of error replacement (D5), or centralize
      final JSON rendering so a rendered result is never emitted twice.
- [x] Test doctor with `--json` and a cancellation during the checks: exit code 130, exactly one
      object, the available checks retained, one interruption line on stderr.
- [x] Update the cancellation rules in
      [03-cli-contract.md §5](docs/03-cli-contract.md#5-json-result) and §4 if the exit-code
      wording changes.

**DoD:** cancellation never duplicates or loses the doctor JSON object, and the exit code stays
130.

### S11.7. Documentation, local gate and v0.0.6 — 0.5–1 d

- [x] Add the v0.0.6 CHANGELOG section covering every behavior change (commit outcomes, exit
      codes 8/6/130, doctor identity rejection, result-file path) as a 0.x release.
- [x] Run `make fmt`, `make lint`, `make test`, `make test-race` and `make cover`
      (≥ 80% total), plus `scripts/setup_test.sh`, `scripts/publish_test.sh`, `actionlint`,
      `shellcheck` and `scripts/check_links.py`.
- [x] Update the review checkboxes above only when the matching fix and its regression test are
      merged; leave I3 open.
- [x] Release v0.0.6 through the release workflow and verify the archives and `checksums.txt`
      exactly as in M4/M5, recording the evidence here.
- [x] Confirm the docs pin the released version and that no document advertises unreleased
      behavior.

**Local evidence 2026-11-09:** `make lint` reports 0 issues (including shellcheck and
actionlint); `make test` passes with 24/24 setup-script tests and 37/37 publish-script tests;
`make test-race` is green; `make cover` reports 82.0% total (app 91.0%, play 85.6%, cli 88.5%,
artifact 77.5%), above the 80% gate; `scripts/check_links.py` resolves all 14 documents.

**Release evidence 2026-11-09:** [release v0.0.6](https://github.com/vyachin/google-play-publisher/releases/tag/v0.0.6),
release workflow run `42620073801` (success, 3m2s). `checksums.txt` contains
`3824a583fa09103df88522f810314b0eabdf747d306ac92cb723082a0a5a5eb1` for
`google-play-publisher_0.0.6_linux_amd64.tar.gz`; the downloaded archive has the same SHA-256.
The released `darwin_arm64` binary reports version `0.0.6`, commit `2049abe`. The push-triggered
`actions` workflow on `main` first failed with HTTP 404 because it ran before the release
assets existed; it passed on re-run after publication (runs `42620063841` and `42620342198`).

**DoD:** local gate is green, every review item is closed or explicitly blocked (I3), and
v0.0.6 is released with verified checksums.

### S11.8. Remove `--expected-tag` [unplanned] — 0.25 d — done

Owner decision 2026-11-09: comparing a CI release tag with the AAB `versionName` is not a
property of the artifact and duplicates a workflow concern. The flag and its action input are
removed.

- [x] Remove `PublishOptions.ExpectedTag`, the `--expected-tag` flag, the `Prepare` checks and
      the matching tests.
- [x] Remove the `expected-tag` action input, the `GPP_EXPECTED_TAG` mapping, the forwarding
      assertion in the action tests and the documented workflow examples.
- [x] Update [03-cli-contract.md](docs/03-cli-contract.md),
      [07-github-actions.md](docs/07-github-actions.md), [README.md](README.md),
      [ADR-0004](docs/adr/0004-github-actions.md) (superseded note), the goreleaser release
      header and the v0.0.6 CHANGELOG.
- [x] Keep `versionName` in logs and the JSON result; only the tag assertion is gone.

**DoD:** no code, script, action or current behavior document advertises `expected-tag`; the
ADR, CHANGELOG and TODO keep only historical or removal notes. The local gate is green, and
`versionName` stays available for release naming and traceability.

### S11.9. Fix the publish action invocation and release v0.0.7 [unplanned] — done

A downstream run with the public action exposed a release-blocking bug: `scripts/publish.sh`
called the CLI without the `publish` subcommand, so every real publication failed with
`unknown flag: --package`. The stub-based action tests could not see it because the stub
accepts any arguments.

- [x] Prefix the forwarded arguments with the `publish` subcommand in
      [scripts/publish.sh](scripts/publish.sh).
- [x] Assert the subcommand in [scripts/publish_test.sh](scripts/publish_test.sh).
- [x] Add a CI job that runs the action with the real CLI and a missing AAB, asserts
      `error-code=usage` and `command=publish`, and rejects an `unknown flag` message.
- [x] Reproduce with the released v0.0.6 binary: `unknown flag: --package`; after the fix the
      CLI reaches local AAB validation.
- [x] Release v0.0.7 and verify the archives and `checksums.txt`.
- [x] Update CHANGELOG, version pins, and note that v0.0.6 is superseded.

**Release evidence 2026-11-09:** [release v0.0.7](https://github.com/vyachin/google-play-publisher/releases/tag/v0.0.7),
release workflow run `42621295744` (success, 2m29s). `checksums.txt` contains
`22e9bcceb346fbeffe55220b7b0b128f05aa102b8af020e29d89d7fd6c9a90c9` for
`google-play-publisher_0.0.7_linux_amd64.tar.gz`; the downloaded archive has the same SHA-256.
The released `darwin_arm64` binary reports version `0.0.7`, commit `1f0af6f`, and with that
binary the action script reaches local AAB validation instead of failing on an unknown flag.
The push-triggered `actions` run `42621292134` first failed only because its setup smoke test
ran before the release assets existed; the re-run passed, including the new real-CLI job. The
v0.0.6 release notes are marked superseded.

**DoD:** a real publish through the action reaches the CLI `publish` command, the regression
tests fail without the subcommand, and v0.0.7 is released.

### S11 order and estimates

| Stage | Items | Estimate | Depends on | Can run in parallel with |
| --- | --- | --- | --- | --- |
| S11.0 | Decisions and baseline | 0.5 d | — | — |
| S11.1 | R16, I7 | 1–1.5 d | S11.0 | — |
| S11.2 | I8 | 0.25–0.5 d | S11.1 | S11.3–S11.6 |
| S11.3 | I9 | 0.25–0.5 d | S11.0 | S11.1 |
| S11.4 | R18 | 0.5–1 d | S11.0 | S11.1–S11.3 |
| S11.5 | R19 | 0.25–0.5 d | S11.0 | S11.1–S11.4 |
| S11.6 | R17 | 0.25–0.5 d | S11.0 | S11.1–S11.5 |
| S11.7 | Docs, gate, release | 0.5–1 d | S11.1–S11.6 | — |

**Total:** 3.5–6 person-days; 4.5–7.5 with the 20% buffer. With the independent items
parallelized, the critical path is S11.0 → S11.1 → S11.2 → S11.7, about 2.5–3.5 wall-clock
days. R16 is the first fix because every later commit-outcome change builds on it.

### S11 risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Commit semantics change exit codes in existing CI | Pipelines that treat any result as success start failing correctly, but may break | CHANGELOG, exit-code table, v0.0.6 gate, action tests that assert 0/8/6 |
| Lifecycle state is absent in fixtures or responses | Wrong reconciliation decision | Explicit `UNSPECIFIED` fallback with a WARN log and documented compatibility rule |
| Doctor starts rejecting AABs without a manifest | Existing fixtures, scripts and users see a new local error | Update fixtures and docs, message names the manifest path, separate CHANGELOG entry |
| Shared identity helper changes publish errors | Regression in the publish path | Refactor with table tests for both callers before changing doctor |
| JSON rendering refactor breaks single-object output | CI parsing fails or duplicates objects | Tests across publish/status/doctor with cancellation and failing writers |
| Result-directory change leaves credentials behind | Secret exposure on self-hosted runners | `umask 077`, `trap` cleanup, tests assert credential removal and result survival |
| Optimistic estimates | Slip beyond the planned stages | 20% buffer; independent items parallelizable; no scope creep |

**S11 DoD:** R16–R19 and I7–I9 are fixed with regression tests and matching documents; the
local gate is green; v0.0.6 is released with verified checksums; I3 remains the only open
verification item and is still blocked by S0.

**Gate:** do not present live verification as complete; local/stub evidence and live evidence
stay separate, as in S10.

## S12. Release fields, `rollout` and `promote` — 1.5–2.5 d — done

**Goal:** let a release be changed after the upload: in-app update priority and country
targeting on `publish`, and the missing track operations `rollout` (fraction, halt, complete)
and `promote` (cross-track promotion) without uploading a bundle again.

- [x] `release.Plan`/`release.Input` carry `inAppUpdatePriority`, `countries` and
      `includeRestOfWorld`; validation rejects a priority outside `0..5`, malformed CLDR codes,
      `--include-rest-of-world` without countries and targeting outside an `inProgress`
      production release, before the API client exists.
- [x] `play.Release` sends `inAppUpdatePriority` and `countryTargeting`; `play.API` gains
      `TrackEdit` (`edits.tracks.get`), which reads the releases inside an edit.
- [x] `app.PrepareRollout`/`RolloutPrepared`: fraction, halt and complete on the existing
      release; the release is read in the edit and every other field is preserved.
- [x] `app.PreparePromote`/`PromotePrepared`: source selection by `--version-code` or the
      highest non-draft release, notes and name inheritance, a conflict when the version is
      already on the destination, `--yes` for production.
- [x] Both commands reuse the commit reconciliation and the manual-review fallback (exit 8) of
      `publish`; `commitTarget` and `commitSettings` replace the `release.Plan` parameter of the
      commit helper.
- [x] `--dry-run`, `--on-in-review` and `--changes-not-sent-for-review` are supported by both
      commands; the JSON envelope adds `versionCodes`, `userFraction` and `from`.
- [x] Tests: release table tests, `app` use cases with the fake API, CLI JSON/text/exit codes,
      and client tests for `TrackEdit` and the new `tracks.update` fields.
- [x] Documentation: [03-cli-contract.md](docs/03-cli-contract.md) (commands, flags, JSON,
      examples), [01-api-feasibility.md](docs/01-api-feasibility.md) (release fields),
      [02-architecture.md](docs/02-architecture.md) (new flow, renumbered sections and anchors),
      [06-stack.md](docs/06-stack.md), [04-play-console-setup.md](docs/04-play-console-setup.md),
      README and CHANGELOG.

**DoD:** `publish --in-app-update-priority/--country`, `rollout --fraction/--halt/--complete`
and `promote --from/--to` are implemented, documented and covered by tests; `make fmt`,
`make lint`, `make test` and `make test-race` pass.
**Note:** live verification against a real app remains the S0/I3 item; the new calls are covered
by fake-API and `httptest` tests only.

## S13. Store metadata in the publish edit — 1.5–2 d — done

**Goal:** publish the store presence of a release together with the AAB, from a versioned
directory in the repository, instead of leaving listings, screenshots and testers to manual
Play Console work.

- [x] `internal/storemeta`: reads `<dir>/<locale>/{title,short_description,full_description,
      video}.txt`, `<dir>/<locale>/images/`, `<dir>/testers/<track>.txt` and validates every
      input: title/short/full lengths, PNG/JPEG format, 8 MiB per file, icon 512x512, feature
      graphic 1024x500, Google Group addresses, track and locale names.
- [x] `play.API` gains `UpdateListing`, `DeleteAllImages`, `UploadImage` and `UpdateTesters`;
      image uploads are not retried (a retry would duplicate the image).
- [x] `app.PublishOptions.MetadataDir`: `app.Prepare` reads and validates the tree before the
      API client exists; `PublishPrepared` applies listings, images (deleteall + upload) and
      testers inside the same edit, before the commit.
- [x] `--metadata-dir` also reads release notes from `<locale>/changelogs/<versionCode>.txt`
      (optional, unlike `--changelog-dir`); combining both flags is a usage error.
- [x] The JSON result carries `metadata: {listings, images, testers}`; the human result shows
      the same counts.
- [x] Tests: `storemeta` table tests with generated PNGs, `app` use cases with the fake API
      (ordering before commit, cleanup on failure, notes from the metadata directory), CLI
      JSON/text/conflict tests, and client tests for all four calls.
- [x] Documentation: [03-cli-contract.md](docs/03-cli-contract.md),
      [02-architecture.md](docs/02-architecture.md),
      [01-api-feasibility.md](docs/01-api-feasibility.md), README and CHANGELOG.

**DoD:** `publish --metadata-dir` publishes the AAB, listings, images and testers in one edit;
invalid metadata fails with code 2 before any API call; the local gate is green.
**Note:** live verification against a real app remains the S0/I3 item; image replacement
semantics (`deleteall` + upload) are covered by fake-API tests and documented for the first
live run.

## S14. `pull`: recreate the metadata tree from Play — 1–1.5 d — done [unplanned]

**Goal:** bootstrap and refresh a versioned store-presence directory from the live Play state
instead of copying listings, screenshots and testers by hand, so `publish --metadata-dir` can
be adopted incrementally.

- [x] `play.API` gains read-only `StoreListings`, `StoreImages`, `TesterGroups`, `AllTracks`
      and `DownloadImage`; the edit is deleted without a commit. Image URLs get the `=s0`
      full-size parameter and are fetched without Play credentials.
- [x] `play.API` image type for the app icon is `icon` (the previous `appIcon` spelling was
      rejected by `edits.images`); `storemeta.ImageTypes` lists the types in Play order.
- [x] `internal/storemeta.Writer`: writes listings, singular images, numbered screenshots,
      testers and changelogs into a hidden staging directory, validates the result with
      `Read`, and swaps it into place; `--force` is required for a non-empty target and the
      previous tree survives a failed download.
- [x] `app.Pull`/`ValidatePullOptions`: local validation before the API client; all listings
      and supported image types; testers for testing tracks only (production answers 400);
      release notes from `--track` (without `--version-code`: every release on the track, with
      it: only the selected version), written for every version code of each selected release.
- [x] CLI `pull` (alias `download`) with `--metadata-dir`, `--track`, `--version-code` and
      `--force`; JSON and text results report listings, images, changelogs and testers.
- [x] Tests: `play` client reads and image downloads (including status mapping and no
      Authorization header), `storemeta` writer staging/force/abort/invalid data, `app.Pull`
      selection and failure cleanup, and CLI JSON/text/usage tests.
- [x] Documentation: [03-cli-contract.md](docs/03-cli-contract.md),
      [02-architecture.md](docs/02-architecture.md), [06-stack.md](docs/06-stack.md),
      [01-api-feasibility.md](docs/01-api-feasibility.md), README and CHANGELOG.

**DoD:** `pull` recreates a valid metadata tree from a real or faked Play state; a failed pull
leaves the target untouched; the local gate is green.
**Note:** live verification against a real app remains the S0/I3 item; the read calls and image
downloads are covered by fake-API and `httptest` tests only.

## S15. Consolidate metadata flags on `--metadata-dir` — 0.5 d — done [unplanned]

**Goal:** remove the `--changelog-dir`/`--metadata-dir` split and make the full metadata tree
the single input for store presence, listings, images, testers and release notes.

- [x] `storemeta.Read` accepts a changelog-only (or empty) tree and returns no store metadata;
      `publish` rejects a metadata directory with neither store metadata nor a changelog for
      the AAB version code.
- [x] `publish`: remove `--changelog-dir`, `PublishOptions.ChangelogDir` and the
      metadata/changelog conflict checks. Release notes come from `--metadata-dir`.
- [x] `promote`: replace `--changelog-dir` with `--metadata-dir`; apply listings, images and
      testers in the promotion edit and read changelogs only with `--version-code`; the JSON
      result reports `metadata` counts.
- [x] Remove `--metadata` (AGP `output-metadata.json`) from `publish` and `doctor`, together
      with `artifact.ReadMetadata`; the AAB manifest is the only identity source.
- [x] Publish action: add `metadata-dir`; keep `changelog-dir` as a deprecated alias that maps
      to `--metadata-dir`; reject both inputs together.
- [x] Tests: changelog-only metadata directories, removed publish conflict, promote metadata
      application, action alias/mutual exclusion, CLI JSON/text results.
- [x] Documentation: CLI contract, README, GitHub Actions, architecture and CHANGELOG.

**DoD:** one CLI metadata flag; `publish` and `promote` accept a full tree or a changelog-only
tree; the action supports the new input with a deprecated alias; the local gate is green.

## S16. Remove JSON log format — 0.25 d — done [unplanned]

**Goal:** remove the second JSON-related flag. `--json` describes the result on `stdout`;
logs on `stderr` stay human-readable text.

- [x] Remove `--log-format` from the global flags, `globalFlags.LogFormat` and the root
      validation.
- [x] Simplify `internal/logging.Options` and `logging.New` to a text handler; remove
      `ValidFormat`.
- [x] Remove the JSON log-format tests and update the debug-output tests.
- [x] Documentation: CLI contract, architecture, stack, ADR-0003, README and CHANGELOG.

**DoD:** one JSON flag remains (`--json`); `stderr` logs are text; the local gate is green.

## S17. Remove the CLI endpoint override — 0.25 d — done [unplanned]

**Goal:** keep the endpoint override inside the Play adapter for `httptest`-based tests and
remove it from the user-facing CLI, where it can redirect an authenticated client.

- [x] Remove `--endpoint` from the global flags, `globalFlags.Endpoint` and `newAPIClient`;
      `play.Options.Endpoint` remains the internal test hook.
- [x] Documentation: CLI contract, stack, ADR-0003 and CHANGELOG.
- [x] Tests: all CLI tests use injected Play clients; no CLI flag test was needed.

**DoD:** the CLI exposes only `--debug`, `--json`, `--timeout` and `--credentials` as global
flags; `play.Options.Endpoint` still serves the adapter tests; the local gate is green.

## S18. Release v0.1.0 — 0.25 d — done [unplanned]

**Goal:** publish the accumulated release: `pull`, store metadata in publish and promote, and
the consolidated CLI surface.

- [x] Move the CHANGELOG `Unreleased` section to `v0.1.0` and bump action/CLI pins to `0.1.0`.
- [x] Tag `v0.1.0` and verify the release workflow, archives and `checksums.txt`.
- [x] Record release evidence.

**Release evidence 2026-11-09:** [release v0.1.0](https://github.com/vyachin/google-play-publisher/releases/tag/v0.1.0),
release workflow run 42650000461. The release contains `checksums.txt`, darwin/linux
`tar.gz` and windows `zip` archives for amd64/arm64. The `darwin_arm64` archive matches
`checksums.txt` (SHA-256 `8e8d9413e7926f3ac67de0c5b3da4d0979c42c5e196cb9f34b95770c989481e6`);
the extracted binary reports version `0.1.0`, commit `49fba8d` and Go `1.27.3`.

**DoD:** `v0.1.0` is published with verified archives and checksums, and the documented pins
point at that release.

## S19. Release v0.1.1 with Marketplace branding — 0.25 d — done [unplanned]

**Goal:** satisfy the GitHub Marketplace listing form by declaring an icon and color for the
publish action, and ship them in a tagged release.

- [x] Add `branding` (`upload`, green) to the root `action.yml` and validate the metadata.
- [x] Add the CHANGELOG section and bump action/CLI pins to `0.1.1`.
- [x] Tag `v0.1.1`, verify the release workflow and record evidence.

**Release evidence 2026-11-09:** [release v0.1.1](https://github.com/vyachin/google-play-publisher/releases/tag/v0.1.1),
release workflow run 42652688723. The release contains `checksums.txt`, darwin/linux `tar.gz`
and windows `zip` archives for amd64/arm64. The `darwin_arm64` archive matches
`checksums.txt` (SHA-256 `b76680e05f20a09edee1d3d4af9b734b799bd70bb42d7925783a7d5c3207d81e`);
the extracted binary reports version `0.1.1`, commit `462ac6f` and Go `1.27.3`.

**DoD:** `v0.1.1` is published with the branding metadata, and the Marketplace form accepts the
icon and color.

## S20. Maintenance release v0.1.2 — 0.25 d — in progress [unplanned]

**Goal:** ship the Dependabot `golang.org/x/oauth2` bump and the restructured README in a
tagged maintenance release, with the documented action and CLI pins moved to it.

- [x] Add the v0.1.2 CHANGELOG section and bump the documented action/CLI pins to `0.1.2`.
- [ ] Tag `v0.1.2`, verify the release workflow and record evidence.

**DoD:** `v0.1.2` is published with verified archives and checksums, and the documented pins
point at that release.
