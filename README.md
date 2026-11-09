# google-play-publisher

Publish signed Android App Bundles to Google Play from your terminal or CI pipeline.
Promote existing builds between tracks, manage staged rollouts, and keep store metadata in Git.

[![CI](https://github.com/vyachin/google-play-publisher/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/vyachin/google-play-publisher/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/vyachin/google-play-publisher?sort=semver&style=flat-square)](https://github.com/vyachin/google-play-publisher/releases/latest)
[![Go version](https://img.shields.io/github/go-mod/go-version/vyachin/google-play-publisher?style=flat-square)](https://github.com/vyachin/google-play-publisher/blob/main/go.mod)
[![Platforms](https://img.shields.io/badge/platforms-Linux%20%7C%20macOS%20%7C%20Windows-blue?style=flat-square)](https://github.com/vyachin/google-play-publisher/releases/latest)

## Use cases

- **Automate releases:** upload a signed AAB to a testing or production track, locally or
  [from GitHub Actions](#github-actions).
- **Ship the build you tested:** [promote an existing release](#promote-a-tested-build) from
  internal testing to beta or production without rebuilding or uploading it again.
- **Control delivery:** [increase, pause, resume or complete a staged rollout](#manage-a-staged-rollout)
  without a new bundle.
- **Review store changes in Git:** [pull listings, images, tester groups and release notes](#keep-store-metadata-in-git),
  then apply them with the next release.

The tool publishes an already signed AAB to an app that exists in Play Console. Building,
signing, first publication and legal declarations stay outside the CLI.

## Installation

Download a binary for Linux, macOS or Windows (amd64 or arm64) from
[GitHub Releases](https://github.com/vyachin/google-play-publisher/releases/latest), verify
its archive against `checksums.txt`, and put `google-play-publisher` on your `PATH`.

To install from source with Go:

```bash
git clone https://github.com/vyachin/google-play-publisher.git
cd google-play-publisher
go install ./cmd/google-play-publisher
```

## Quick start

You need an app that already has an uploaded build in Play Console and a service account
with access to it. For internal testing, grant the account the "Release to testing tracks"
permission. Follow the [Play Console setup guide](docs/04-play-console-setup.md) to configure
API access and permissions for your target track.

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json

# Check access, the target track and the signed bundle.
google-play-publisher doctor -p com.example.app -f app-release.aab

# Upload and publish to internal testing (the default track).
google-play-publisher publish -f app-release.aab

# Check the releases on the track.
google-play-publisher status -p com.example.app -t internal
```

You can pass `--credentials /path/to/sa.json` instead of setting the environment variable.
The CLI reads the package name and version code from the AAB manifest. Optional `--package`
and `--version-code` flags assert the expected values; a mismatch fails before any network call.

## Promote a tested build

Move the same build through your release process without rebuilding it:

```bash
google-play-publisher promote -p com.example.app --from internal --to beta

# Start a production rollout for version code 42 at 10%.
google-play-publisher promote -p com.example.app --from beta --to production \
  --version-code 42 --rollout 0.1 --yes
```

Without `--version-code`, promotion selects the source release with the highest version code,
excluding drafts. Promotion to production requires `--yes` and never prompts interactively.

## Manage a staged rollout

After a release reaches a track, adjust its rollout without uploading another AAB:

```bash
# Increase the rollout to 20%.
google-play-publisher rollout -p com.example.app -t production --version-code 42 --fraction 0.2

# Pause, then resume at 20%.
google-play-publisher rollout -p com.example.app -t production --version-code 42 --halt
google-play-publisher rollout -p com.example.app -t production --version-code 42 --fraction 0.2

# Release to all users.
google-play-publisher rollout -p com.example.app -t production --version-code 42 --complete
```

To start a staged rollout with a new AAB, use `publish -t production --rollout 0.1`.

## Keep store metadata in Git

Download an existing app's store metadata into a directory you can review and commit:

```bash
google-play-publisher pull -p com.example.app --metadata-dir metadata
```

The directory must be absent or empty. Use `--force` to replace an existing tree; the download
is staged and validated before replacement. Pull does not change anything in Play, but it
creates a temporary edit and consumes edit quota. Release notes come from `internal` by default;
use `--track` to select another track.

The same layout holds listings, images, release notes and tester groups:

```text
metadata/
  en-US/
    title.txt
    short_description.txt
    full_description.txt
    images/
      icon.png
      featureGraphic.png
      phoneScreenshots/001.png
    changelogs/42.txt
  de-DE/
    changelogs/42.txt
  testers/
    internal.txt
```

Edit the files, then apply them in the same edit as the next AAB:

```bash
google-play-publisher publish -f app-release.aab --metadata-dir metadata
```

Release notes are selected by the AAB version code: version `42` uses
`<locale>/changelogs/42.txt`. A directory containing only release notes is also valid.

Text and images are validated locally before API access. Present image sets and tester files
replace their counterparts in Play; an empty screenshot directory clears that image type.
Review deletions before publishing. See the [CLI reference](docs/03-cli-contract.md)
for the full layout and validation rules.

## GitHub Actions

The publish action installs the CLI, publishes the signed AAB, captures the JSON result and
writes a job summary. Add these steps after your bundle build on a Linux runner:

```yaml
- name: Write the service-account key
  env:
    SA_JSON: ${{ secrets.GOOGLE_PLAY_SA_JSON }}
  run: |
    umask 077
    printf '%s' "$SA_JSON" > "$RUNNER_TEMP/sa.json"

- uses: vyachin/google-play-publisher@v0.1.1
  with:
    version: 0.1.1
    package: com.example.app
    aab: android/app/build/outputs/bundle/release/app-release.aab
    track: internal
    credentials-file: ${{ runner.temp }}/sa.json
```

Serialize publishing jobs for the same app with a shared `concurrency` group to avoid
conflicting Play edits. The [GitHub Actions guide](docs/07-github-actions.md) covers complete
workflows, metadata, outputs and the setup action for running CLI commands directly.

For other CI systems, invoke the CLI with `--json` to capture a machine-readable result;
logs go to stderr:

```bash
google-play-publisher publish -f app-release.aab --json > publish.json
```

## Before publishing

- The CLI does not build or sign AABs, create apps, or complete their first publication.
- Managed publishing can hold changes on testing and production tracks for a manual
  "Publish changes" step; it does not apply to `internal`.
- `publish --dry-run` uploads the AAB and validates the edit without committing it. The version
  code may remain used after cleanup, so reserve a separate version code for dry runs.
- `--if-exists=skip` makes a repeated publish a no-op when the version code is already on the
  target track.
- Exit code `8` means the release was saved but needs manual submission for review in Play
  Console. See [errors and exit codes](docs/05-errors.md) for recovery steps.

## Documentation

- [Documentation index](docs/README.md) — design, architecture and platform constraints.
- [CLI reference](docs/03-cli-contract.md) — commands, flags, metadata layout and JSON output.
- [Play Console setup](docs/04-play-console-setup.md) — API access, credentials and permissions.
- [GitHub Actions](docs/07-github-actions.md) — setup and publish workflows.
- [Errors and retries](docs/05-errors.md) — exit codes and troubleshooting.

Run `google-play-publisher --help` for commands, or append `--help` to any command for its
options. Use `--debug` for detailed diagnostics.

## Development

```bash
make build      # Build bin/google-play-publisher.
make fmt        # Format Go code and tidy dependencies.
make lint       # Run Go, shell and workflow linters.
make test       # Run Go tests, action script tests and documentation link checks.
```

See the [development plan](TODO.md) and [project rules](AGENTS.md) before contributing.

### Development disclosure

This project is developed with substantial AI assistance from DeepSeek Harness
(deepseek-flash) and OpenAI Codex, under human direction and review. The maintainer is
responsible for the architecture, release process and verification.
