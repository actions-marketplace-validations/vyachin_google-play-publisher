# 07 — GitHub Actions

**Status:** released as v0.1.2. The examples pin that release; do not reference `@main` from a
production workflow.

The action revision and the CLI `version` input are separate; use the same release for both
unless a workflow intentionally mixes them.

## Support matrix

| Runner | Architecture | Supported |
| --- | --- | --- |
| `ubuntu-latest` | x86_64 | yes |
| `ubuntu-24.04-arm` | aarch64 | yes |
| other Linux runners | x86_64/aarch64 | yes, if `curl`, `tar` and a SHA-256 tool exist |
| macOS, Windows | any | no — the action fails with a clear message |

The CLI binaries for macOS and Windows are still published on GitHub Releases; only the
action wrapper is Linux-only.

## Setup action

Installs a released `google-play-publisher` archive, verifies it against `checksums.txt` and
adds the binary directory to `PATH`.

```yaml
- uses: vyachin/google-play-publisher/setup@v0.1.2
  with:
    version: 0.1.2
```

Inputs:

| Input | Required | Description |
| --- | --- | --- |
| `version` | yes | Released CLI version. `X.Y.Z` and `vX.Y.Z` are accepted; ranges, branches and `latest` are rejected |

Outputs:

| Output | Description |
| --- | --- |
| `version` | Normalized version without the leading `v` |
| `path` | Directory that contains the `google-play-publisher` binary |

Behavior:

- downloads `google-play-publisher_<version>_linux_<arch>.tar.gz` and `checksums.txt` from the
  matching GitHub release;
- verifies the exact archive entry's SHA-256 before extraction;
- installs under `$RUNNER_TEMP/google-play-publisher/<version>`;
- appends that directory to `GITHUB_PATH`, so later `run` steps can invoke
  `google-play-publisher`;
- fails clearly for a missing release, asset or checksum, an unsupported platform, or an
  invalid version.

After the setup step, use the CLI directly:

```yaml
- uses: vyachin/google-play-publisher/setup@v0.1.2
  with:
    version: 0.1.2

- name: Check access
  run: |
    google-play-publisher doctor \
      --package "$PACKAGE" \
      --credentials "$RUNNER_TEMP/sa.json"
```

The setup action does not handle Play credentials; pass them to the CLI step as described in
[04 — Play Console and access setup](04-play-console-setup.md).

## Publish action

Uploads a signed AAB, declares the release on a track and commits. The action is a thin
wrapper around the CLI: input validation, Play calls, retries and exit codes stay in
`google-play-publisher`.

```yaml
- name: Write the service-account key
  env:
    SA_JSON: ${{ secrets.GOOGLE_PLAY_SA_JSON }}
  run: |
    umask 077
    printf '%s' "$SA_JSON" > "$RUNNER_TEMP/sa.json"

- uses: vyachin/google-play-publisher@v0.1.2
  with:
    version: 0.1.2
    package: com.example.app
    aab: android/app/build/outputs/bundle/release/app-release.aab
    track: internal
    metadata-dir: android/metadata
    credentials-file: ${{ runner.temp }}/sa.json
```

Inputs:

| Input | Required | Default | Description |
| --- | --- | --- | --- |
| `version` | no | action release tag | CLI version; required when the action ref is a branch or SHA |
| `package` | yes | — | `applicationId` in Play Console |
| `aab` | yes | — | Path to the signed AAB |
| `track` | no | `internal` | Release track |
| `metadata-dir` | no | — | Play-style metadata tree with listings, images, testers and `<locale>/changelogs/<versionCode>.txt`; a changelog-only tree is accepted |
| `changelog-dir` | no | — | Deprecated alias for `metadata-dir`; kept for existing workflows |
| `version-code` | no | AAB | Optional assertion checked against the AAB and upload; used for changelog lookup |
| `release-name` | no | — | Release name; empty lets Play generate it from the AAB version name |
| `if-exists` | no | `fail` | `fail` or `skip` when the version code is already on the track |
| `credentials-file` | no | — | **Preferred.** Path to a service-account JSON key, passed to the CLI as `--credentials`; relative paths use `GITHUB_WORKSPACE` |
| `credentials-base64` | no | — | Alternative: base64-encoded service-account JSON, decoded to a temporary file |
| `timeout` | no | CLI default | Overall operation timeout |
| `debug` | no | `false` | Debug logging; secrets are redacted |

`credentials-base64` and `credentials-file` are mutually exclusive. With neither input the
CLI falls back to Application Default Credentials. Prefer `credentials-file`: it maps
directly to `google-play-publisher --credentials /path/to/sa.json` and avoids passing the key
through a second encoding.

Outputs:

| Output | Description |
| --- | --- |
| `version-code` | Version code reported by the upload |
| `edit-id` | Play edit id, empty for a skip |
| `review` | `sent`, `notSent`, `replaced`, `skipped` or `validated` |
| `skipped` | `true` for an idempotent skip |
| `manual-action` | `true` when the release was saved without review |
| `error-code` | Stable CLI error code for a failure |
| `result-file` | Path to the captured CLI JSON result |

Behavior:

- relative input paths resolve against `GITHUB_WORKSPACE`; `defaults.run.working-directory`
  does not change action input paths;
- inputs are passed as quoted CLI arguments, never interpolated into shell source;
- the AAB manifest supplies `package` and `versionCode`; `package` and `version-code` remain
  optional assertions, and `metadata-dir` uses the AAB version code;
- base64 credentials are decoded into a `umask 077` temporary file under `$RUNNER_TEMP`,
  masked in the log, and removed after success or failure;
- the CLI runs once with `--json`; stderr is forwarded to the job log, the JSON result is
  captured in `result-file`;
- the CLI exit code is preserved, including 8 for a release saved without review; the Job
  Summary explains the required manual submission;
- `jq` is used to derive outputs on GitHub-hosted Linux runners; when it is missing the raw
  result file is kept and a warning is emitted instead of failing the publication.

### Exit codes

The action exits with the CLI code: 0 for success or an idempotent skip, 5 for a conflict,
6 for a transient failure, 8 for manual submission and so on. See the
[CLI contract](03-cli-contract.md#4-exit-codes).

### Concurrency

One edit per user and app is allowed by Play. The action cannot enforce this across jobs;
add a per-app `concurrency` group to the workflow that calls it.

## Complete workflow: setup plus CLI

This keeps the CLI invocation visible and adds an early access check. Paths assume the
Android project lives in `android/`; adjust them for your repository.

```yaml
name: android-release
on:
  push:
    tags: ["v*"]

permissions:
  contents: read

concurrency:
  group: play-${{ github.ref_name }}
  cancel-in-progress: false

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - uses: actions/setup-java@v5
        with:
          distribution: temurin
          java-version: "21"

      - name: Build the signed bundle
        working-directory: android
        run: ./gradlew :app:bundleRelease --no-daemon

      - uses: vyachin/google-play-publisher/setup@v0.1.2
        with:
          version: 0.1.2

      - name: Write the service-account key
        env:
          SA_JSON: ${{ secrets.GOOGLE_PLAY_SA_JSON }}
        run: |
          umask 077
          printf '%s' "$SA_JSON" > "$RUNNER_TEMP/sa.json"

      - name: Check Play access before publishing
        run: |
          google-play-publisher doctor \
            --package com.example.app \
            --credentials "$RUNNER_TEMP/sa.json"

      - name: Publish to internal
        run: |
          google-play-publisher publish \
            --aab android/app/build/outputs/bundle/release/app-release.aab \
            --track internal \
            --metadata-dir android/metadata \
            --credentials "$RUNNER_TEMP/sa.json" \
            --json > publish.json

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: publish-result
          path: publish.json
```

Build, signing, Android checks and artifact retention stay in the workflow; the action only
installs the CLI. `doctor` runs before the publish step and does not create an edit.

## Complete workflow: publish action

The publish action replaces installation, key decoding and the publish command:

```yaml
name: android-release
on:
  push:
    tags: ["v*"]

permissions:
  contents: read

concurrency:
  group: play-${{ github.ref_name }}
  cancel-in-progress: false

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - uses: actions/setup-java@v5
        with:
          distribution: temurin
          java-version: "21"

      - name: Build the signed bundle
        working-directory: android
        run: ./gradlew :app:bundleRelease --no-daemon

      - name: Write the service-account key
        env:
          SA_JSON: ${{ secrets.GOOGLE_PLAY_SA_JSON }}
        run: |
          umask 077
          printf '%s' "$SA_JSON" > "$RUNNER_TEMP/sa.json"

      - name: Publish to internal
        id: play
        uses: vyachin/google-play-publisher@v0.1.2
        with:
          version: 0.1.2
          package: com.example.app
          aab: android/app/build/outputs/bundle/release/app-release.aab
          track: internal
          metadata-dir: android/metadata
          credentials-file: ${{ runner.temp }}/sa.json

      - name: Show the result
        run: |
          echo "version code: ${{ steps.play.outputs.version-code }}"
          echo "review: ${{ steps.play.outputs.review }}"
```

The publish action keeps `package` required as a guard against publishing the wrong app.
`version-code` and `release-name` can be omitted; the AAB supplies the version code and Play
generates the release name from the version name when it is empty.

## Authentication

The publish action accepts two mutually exclusive credential modes; with neither, the CLI
falls back to Application Default Credentials.

| Mode | Input | Notes |
| --- | --- | --- |
| File (preferred) | `credentials-file` | Passed to the CLI as `--credentials /path/to/sa.json`; relative paths resolve against `GITHUB_WORKSPACE`; the action reads the file and never deletes it |
| Base64 (alternative) | `credentials-base64` | Create the secret once: `base64 -w0 < sa.json`; do not add a trailing newline |
| ADC | neither input | Set `GOOGLE_APPLICATION_CREDENTIALS` for the step or on the runner |

Base64 credentials are decoded into a `umask 077` file under `$RUNNER_TEMP`, masked in the
job log, and removed by the step's exit trap. Calling the CLI directly instead:

```yaml
- name: Publish with a credentials file
  run: |
    google-play-publisher publish \
      --aab app-release.aab \
      --credentials "$RUNNER_TEMP/sa.json"
```

Hosted runners destroy `$RUNNER_TEMP` after the job. Self-hosted runners may not: use
ephemeral runners or a secret store, and do not write keys to a path that survives the job.

## Path semantics

- `aab`, `metadata-dir` and `credentials-file` are resolved against `GITHUB_WORKSPACE` when
  they are relative; absolute paths are used as-is.
- `defaults.run.working-directory` applies to `run` steps only and does not change action input
  paths. For a project in `android/`, pass `android/app/...` and `android/metadata`.
- Inputs are passed to the CLI as quoted array elements, so spaces and shell metacharacters in
  paths are preserved and never evaluated as shell source.

## Outputs and artifacts

The action writes the CLI JSON result to `result-file`. Keep it as a workflow artifact for
auditing:

```yaml
- uses: actions/upload-artifact@v4
  if: always()
  with:
    name: play-result
    path: ${{ steps.play.outputs.result-file }}
```

`result-file` is written even when the CLI fails after rendering the error object. Each
invocation creates a fresh directory under `$RUNNER_TEMP`, so two publish steps in the same job
expose distinct result files and neither overwrites the other. `version-code`, `edit-id`,
`review`, `skipped`, `manual-action` and `error-code` are derived with `jq`; when `jq` is
unavailable the action keeps the raw file, omits those outputs and emits a warning.

## Manual review and exit code 8

Play sometimes refuses to submit a release automatically. The CLI then saves the changes
without review, reports the saved release and exits with code 8. The action preserves that
code, so the job fails, and the Job Summary explains that a person must open Play Console and
send the changes for review. Use `continue-on-error: true` only if the pipeline should
continue after that warning; inspect `manual-action` and `review` outputs instead of ignoring
the failure.

## Troubleshooting

| Symptom | Likely cause | Action |
| --- | --- | --- |
| `AUTH`/exit code 3 | Key path, token request, permissions or the API is disabled | Check `doctor`; verify Play Console access and `androidpublisher.googleapis.com` |
| `NOT_FOUND`/exit code 4 | Wrong package or no access to the app | Verify `applicationId` and the service-account invite |
| `CONFLICT`/exit code 5 | Version code already on the track | Bump the version code or use `if-exists: skip` |
| Exit code 8 | Release saved without review | Send the changes from Play Console |
| `unsupported operating system` | Action ran on macOS or Windows | Use a Linux runner |
| Checksum failure | Release asset or `checksums.txt` missing/changed | Pin an existing release tag and retry the download |
| Outputs empty, warning about `jq` | Self-hosted runner without `jq` | Install `jq` or read `result-file` directly |

## Managed publishing

Managed publishing does not affect the `internal` track. For closed/open testing and
production, the commit may succeed while publication waits for a manual "Publish changes"; use
`doctor`/`status` to confirm the state.

## Verification status

- Setup and publish scripts have stub-based tests that need no Play credentials
  ([scripts/setup_test.sh](../scripts/setup_test.sh), [scripts/publish_test.sh](../scripts/publish_test.sh)).
- The setup action is smoke-tested on `ubuntu-latest` and `ubuntu-24.04-arm`; the publish action
  is exercised with a stub CLI in CI.
- Live publication through the action remains pending S0 Play access and a dedicated test app;
  it is tracked separately from local/stub verification (I3).

## Action revision and CLI version

The action revision (`@v0.1.2`) and the CLI `version` input are separate coordinates.
Pin the action to a release tag, optionally to a commit SHA for stricter supply-chain control,
and pass the CLI version that the workflow should run. Both must exist as released artifacts.
The publish action defaults `version` to the action ref only when that ref is a release tag;
with a branch or SHA, pass `version` explicitly.
