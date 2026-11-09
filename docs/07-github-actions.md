# 07 — GitHub Actions

**Status:** interface under development. The setup action is implemented, but no release that
contains it exists yet. Do not reference `@main` from a production workflow; pin the release
tag published in S10.6.

This guide documents the setup action. The publish action, complete workflow examples and
release pinning are added in the same stage before the first action release.

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

## Action revision and CLI version

The action revision (`@<release-tag>`) and the CLI `version` input are separate coordinates.
Pin the action to a release tag, optionally to a commit SHA for stricter supply-chain control,
and pass the CLI version that the workflow should run. Both must exist as released artifacts.
