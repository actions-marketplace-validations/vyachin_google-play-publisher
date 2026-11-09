# Changelog

## v0.1.1 — 2026-11-09

### Added

- The publish action declares GitHub Marketplace branding: the `upload` icon and the
  green color. The Marketplace listing now shows the action badge.

## v0.1.0 — 2026-11-09

### Added

- `pull` (alias `download`) recreates a Play-style metadata directory from Google Play:
  localized listings, full-size images, tester groups and the release notes of a selected
  track. The target must be absent or empty; `--force` replaces a non-empty tree after the
  download is staged and validated with the same rules as `publish --metadata-dir`. Pull uses
  a read-only edit, never commits it and deletes it best-effort. `--track` chooses the track;
  without `--version-code` the notes of every release on it are pulled, and with it only the
  selected version. Tester groups come from testing tracks only; production is skipped because
  Play rejects `testers.get` there. A pulled tree round-trips through `publish --metadata-dir`.
- `publish --in-app-update-priority 0..5` sets the in-app update priority of the release. Play
  does not allow changing it once the release has been rolled out.
- `publish --country` and `--include-rest-of-world` restrict an `inProgress` production release
  to CLDR country codes. Codes are upper-cased, de-duplicated and sorted; any other status or
  track is rejected locally with exit code 2.
- `rollout` changes a release that is already on a track without uploading a bundle:
  `--fraction` sets the user share and also resumes a halted release, `--halt` stops serving
  the update and keeps the stored fraction, `--complete` rolls it out to everyone. The release
  is read inside the edit, so its notes, name, version codes, in-app update priority and
  country targeting are preserved.
- `promote` declares the version codes of a source release on another track, optionally as a
  staged rollout. The source is selected by `--version-code` or as the highest non-draft
  release; notes and the release name are inherited unless overridden. Promotion to
  `production` requires `--yes`.
- `rollout` and `promote` support `--dry-run`, `--on-in-review` and
  `--changes-not-sent-for-review`, reuse the ambiguous-commit reconciliation and report the
  same JSON envelope as `publish` (`versionCodes`, optional `userFraction`, `from` for
  `promote`).
- `publish --metadata-dir` applies store listings, images and tester groups in the same edit as
  the AAB: one commit publishes the bundle and its store presence together. The tree is
  validated before authentication (title 30 / short 80 / full 4000 characters, PNG or JPEG,
  icon 512x512, feature graphic 1024x500, 8 MiB per image), image sets and tester files are
  authoritative, and `--metadata-dir` supplies changelogs for the AAB version code. The JSON
  result gains a `metadata` object with the applied counts.

### Changed

- `--metadata-dir` is now the only metadata flag for `publish` and `promote`. It accepts a
  full tree or a changelog-only tree, so `--changelog-dir` was removed from the CLI
  (breaking change). The publish action adds a `metadata-dir` input; its old `changelog-dir`
  input remains as a deprecated alias.
- `promote --metadata-dir` applies listings, images and testers as well as changelogs in the
  promotion edit. Changelogs are read only together with `--version-code`; the JSON result
  gains the `metadata` counts when the flag is used.

### Removed

- `--metadata` (AGP `output-metadata.json`) was removed from `publish` and `doctor`; the AAB
  manifest is the only identity source. `--package` and `--version-code` remain optional
  assertions, and `artifact.ReadMetadata` was deleted.
- `--log-format` was removed. Logs are always text on `stderr`; `--json` affects only the
  result on `stdout`. This leaves one JSON-related flag instead of two.
- `--endpoint` was removed from the CLI. It was a test hook that could redirect an
  authenticated client, and therefore its OAuth token, to an arbitrary host. `play.Options`
  keeps the endpoint override for `httptest`-based tests.

### Fixed

- The Play image type for the app icon was spelled `appIcon`, but `edits.images` expects
  `icon`. `publish --metadata-dir` now uploads `images/icon.png` with the correct type, and
  `pull` reads the same type, so a pulled tree can be published again.
- `promoGraphic` is not a valid `AppImageType` in the current Play API and made `images.list`
  fail with HTTP 400. The unsupported slot was removed from the metadata tree and `pull`; use
  `featureGraphic`, `tvBanner` or the screenshot types instead.
- `pull` skipped `testers.get` for production tracks. Play answers 400 ("not a testing track")
  for `production` and its form-factor variants, which aborted an otherwise successful pull.

## v0.0.7 — 2026-11-09

### Fixed

- The publish action invoked the CLI without the `publish` subcommand, so every real
  publication failed with `unknown flag: --package`. The action now runs
  `google-play-publisher publish ...`; a shell regression test asserts the subcommand, and a CI
  job runs the action with the real CLI and a missing AAB to verify that the CLI parses the
  forwarded flags.
- `v0.0.6` is superseded by this release because its publish action could not publish.

## v0.0.6 — 2026-11-09

### Fixed

- A commit rejected by Play is no longer reported as success when another operation puts the
  same version code on the track. Only failures typed as transport or 5xx are reconciled; a
  definitive rejection keeps its exit code and does not read the track.
- An ambiguous commit observed as `NOT_SENT_FOR_REVIEW` reports `review: "notSent"` and exit
  code 8 with the Play Console instruction, unless `--changes-not-sent-for-review` was passed
  explicitly (then 0), or the run requested `--status draft`.
- An unverifiable commit keeps its edit and returns code 6 with the package, track, version
  code and edit ID instead of deleting the edit as a failed publication.
- A commit whose response is lost at the `--timeout` deadline is reconciled under a separate
  15 s recovery budget. SIGINT/SIGTERM interrupts an already running recovery and still
  returns 130.
- `doctor` validates the AAB manifest identity: a bundle for another application or version is
  rejected locally with code 2, `--metadata` is compared with the AAB, and the file is closed
  on every path.
- `--json doctor` interrupted while running writes exactly one JSON object and exits with
  code 130.
- Reusing a closed or already consumed `Prepared` publish operation fails locally before an
  edit is created.

### Changed

- Each publish action invocation writes its result file to a fresh `$RUNNER_TEMP` directory,
  so two publish steps in one job keep separate `result-file` outputs.
- `doctor --aab` now requires a readable `base/manifest/AndroidManifest.xml`, matching
  publish. Bundles without it were never valid release artifacts.

### Removed

- `--expected-tag` and the `expected-tag` publish-action input. Tag validation belongs to the
  CI workflow; the AAB `versionName` is still reported in logs and the JSON result.

## v0.0.5 — 2026-11-09

### Added

- Release identity (`package`, `versionCode`, `versionName`) is read from the AAB base
  manifest, so `--package` and `--version-code` are optional assertions instead of required
  inputs.
- `--changelog-dir` selects notes with the AAB version code; `--version-code` is no longer
  required.
- `--if-exists=skip` works without a metadata sidecar.
- `--expected-tag` asserts `v<AAB versionName>` before any upload.
- GitHub Actions setup and publish actions, with checksum-verified installation, safe
  credential handling, JSON outputs and a Job Summary. Included in this release.
- `--json` publish results include `schemaVersion` and `versionName`.

### Changed

- The AAB is opened once and uploaded from the inspected file handle, so replacing the path
  during a run cannot change the uploaded bytes.
- `--metadata` is a legacy cross-check: its `applicationId` and `versionCode` must match the
  AAB.
- A non-zero `android:versionCodeMajor`, resource-referenced identity values and malformed
  manifests are rejected locally with exit code 2.

## v0.0.4 — 2026-11-09

### Fixed

- AAB uploads declare `application/octet-stream` as the media type. Content sniffing
  classified the ZIP-based bundle as `application/zip` and Play rejected the upload with
  `Media type 'application/zip' is not supported. (HTTP 400)`.

## v0.0.3 — 2026-11-09

### Fixed

- Failed `--json` commands also print the final error and hint to `stderr`, keeping CI
  diagnostics visible when the JSON result is redirected to a file.
- `--json` for a release saved without review (exit code 8) prints the required Play Console
  action on `stderr` while keeping exactly one success object on `stdout`.
- Connection-refused and unreachable-host/network failures are classified as transient (6)
  and retried instead of being reported as internal (1).
- A `versionCode` conflict between `--metadata` and `--version-code` is rejected with exit
  code 2 before the API client is created, including with `--if-exists=skip`.
- Text output failures in `publish`, `status` and `doctor` are no longer ignored: they
  return a nonzero code, while a saved release keeps exit code 8 and `doctor` keeps the code
  of the failed check.
- Debug body capture redacts secrets that cross the 4 KiB capture limit or contain escaped
  quotes, and omits a body that cannot be redacted safely; the replayed payload is unchanged.
- Token acquisition and blocking refresh run under the operation context, so `--timeout` and
  cancellation also stop a stalled token endpoint; token endpoint failures are mapped from
  the cloud auth transport.
- Publish invariants (`--if-exists`, `--on-in-review`, `--dry-run` with
  `--changes-not-sent-for-review`) and metadata-based package resolution are validated in the
  app layer; `PublishPrepared` accepts only the opaque result of `Prepare`.

## v0.0.2 — 2026-11-09

**Breaking:** `--release-notes` is removed. Release notes now come from `--changelog-dir`
(the Play/fastlane layout `<locale>/changelogs/<versionCode>.txt`) and require
`--version-code`.

### Added

- `--changelog-dir DIR` and `--version-code N`: localized release notes read from the
  metadata directory and validated before any edit is created; the uploaded version code is
  compared with the declared one.
- `--release-name NAME`: the release name sent to `tracks.update`.
- JSON error contract: with `--json` a failure writes exactly one object on `stdout` with a
  stable `error.code`, `error.exitCode`, message, hint and, when available, `apiCode` and
  `reason`.
- Transport and token-retrieval failures are now classified: network/timeout errors return
  code 6, rejected credentials return code 3.
- The `sha256` returned by the upload is verified against the inspected artifact before the
  track is updated.
- Safely repeatable API calls retry transient failures with bounded exponential backoff and
  honor `Retry-After`; `edits.insert` is retried at most twice and `edits.commit` keeps state
  reconciliation instead of blind retries.
- `--package` can be resolved from `--metadata` when it is omitted.
- `--if-exists=skip` now also works with `--version-code`, without the metadata sidecar.

### Fixed

- Changes saved without review because Play refused automatic submission now return exit
  code 8 instead of 0.
- The reported `review` state is derived from the commit options actually sent, including
  ambiguous attempts resolved by verification.
- Every ambiguous commit attempt is reconciled against the track before a retry or an error,
  so a release applied by a failed retry is no longer reported as a failure.
- `doctor --json` reports checks and the error in one object and propagates output failures.
- `--rollout` rejects `NaN` and infinities locally instead of failing after the upload.
- Debug logging replays HTTP bodies unchanged: partial reads keep the original bytes, the read
  error and the close semantics, and large JSON bodies are buffered only up to the log budget.
- Local validation (package, track, status, rollout, notes, AAB, metadata) runs before the API
  client is created, so bad input returns code 2 even without credentials.

## v0.0.1 — 2026-11-09

First public release: publishing signed AABs to the Google Play internal track.

- Renamed the executable to `google-play-publisher`.
- Translated all documentation to English and added `AGENTS.md` with project rules.

### Commands

- `publish` — preflight, AAB upload, release on `internal`, edit commit, `--dry-run`,
  `--json`, release notes, `--if-exists`.
- `doctor` — API access, track, AAB, JAR signature, AGP metadata.
- `status` — releases on a track (read-only).
- `completion` — bash/zsh/fish/powershell.

### Quality

- 83% coverage, tests against a fake Play API, `-race`, `golangci-lint`, `govulncheck`.
- Debug mode with secret redaction.
- GoReleaser builds: linux/darwin/windows, amd64/arm64, checksums.

### Limitations

- The first app publication is done manually in Play Console.
- Managed publishing does not affect `internal`.
- The AAB must be signed; the tool does not sign artifacts.
