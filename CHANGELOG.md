# Changelog

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
  credential handling, JSON outputs and a Job Summary. They are published with the next
  release.
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
