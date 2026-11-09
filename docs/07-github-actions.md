# 07 — GitHub Actions

**Status:** interface under development. The setup and publish actions are implemented, but no
release that contains them exists yet. Do not reference `@main` from a production workflow; pin
the release tag published in S10.6.

This guide documents both actions. Complete workflow examples are added in S10.5 before the
first action release.

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
- uses: vyachin/google-play-publisher/setup@<release-tag>
  with:
    version: 0.0.5
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
- uses: vyachin/google-play-publisher/setup@<release-tag>
  with:
    version: 0.0.5

- name: Check access
  env:
    GOOGLE_APPLICATION_CREDENTIALS: ${{ runner.temp }}/sa.json
  run: google-play-publisher doctor --package "$PACKAGE"
```

The setup action does not handle Play credentials; pass them to the CLI step as described in
[04 — Play Console and access setup](04-play-console-setup.md).

## Publish action

Uploads a signed AAB, declares the release on a track and commits. The action is a thin
wrapper around the CLI: input validation, Play calls, retries and exit codes stay in
`google-play-publisher`.

```yaml
- uses: vyachin/google-play-publisher@<release-tag>
  with:
    version: 0.0.5
    package: com.example.app
    aab: android/app/build/outputs/bundle/release/app-release.aab
    track: internal
    changelog-dir: android/metadata
    expected-tag: ${{ github.ref_name }}   # tag-triggered workflows only
    credentials-base64: ${{ secrets.GOOGLE_PLAY_SA_BASE64 }}
```

Inputs:

| Input | Required | Default | Description |
| --- | --- | --- | --- |
| `version` | no | action release tag | CLI version; required when the action ref is a branch or SHA |
| `package` | yes | — | `applicationId` in Play Console |
| `aab` | yes | — | Path to the signed AAB |
| `track` | no | `internal` | Release track |
| `changelog-dir` | no | — | Play-style metadata directory with `<locale>/changelogs/<versionCode>.txt` |
| `version-code` | no | AAB | Optional assertion checked against the AAB and upload; used for changelog lookup |
| `release-name` | no | — | Release name; empty lets Play generate it from the AAB version name |
| `expected-tag` | no | — | Assert the release tag as `v<AAB versionName>`; leave empty for branch runs |
| `if-exists` | no | `fail` | `fail` or `skip` when the version code is already on the track |
| `credentials-base64` | no | — | Base64-encoded service-account JSON |
| `credentials-file` | no | — | Path to a service-account JSON key; relative paths use `GITHUB_WORKSPACE` |
| `timeout` | no | CLI default | Overall operation timeout |
| `debug` | no | `false` | Debug logging; secrets are redacted |

`credentials-base64` and `credentials-file` are mutually exclusive. With neither input the
CLI falls back to Application Default Credentials.

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
  optional assertions, and `changelog-dir` uses the AAB version code;
- `expected-tag` fails with exit code 2 before any upload when it differs from
  `v<versionName>`; use it only in tag-triggered workflows;
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

## Action revision and CLI version

The action revision (`@<release-tag>`) and the CLI `version` input are separate coordinates.
Pin the action to a release tag, optionally to a commit SHA for stricter supply-chain control,
and pass the CLI version that the workflow should run. Both must exist as released artifacts.
The publish action defaults `version` to the action ref only when that ref is a release tag;
with a branch or SHA, pass `version` explicitly.
