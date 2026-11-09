# google-play-publisher

A Go CLI that publishes a signed Android App Bundle (AAB) to Google Play.

**Status:** v0.0.2. Publishing targets the `internal` track.

## Installation

From source:

```bash
git clone https://github.com/vyachin/google-play-publisher.git
cd google-play-publisher
go install ./cmd/google-play-publisher
```

Prebuilt binaries for linux/darwin/windows (amd64/arm64) are published on GitHub Releases;
verify them with `checksums.txt`.

## Authentication

You need a service account JSON key whose account is added to Play Console with the
"Release to testing tracks" permission. Pass the key in one of two ways:

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json
# or
google-play-publisher publish ... --credentials /path/to/sa.json
```

Full setup instructions: [docs/04-play-console-setup.md](docs/04-play-console-setup.md).

## Quick start

```bash
# 1. Check access and the target track
google-play-publisher doctor -p com.example.app

# 2. Publish to internal
google-play-publisher publish -p com.example.app -f app-release.aab -t internal

# 3. Show releases
google-play-publisher status -p com.example.app -t internal
```

`--json` prints a machine-readable result to stdout; logs go to stderr:

```bash
google-play-publisher --json publish -p com.example.app -f app-release.aab > publish.json
```

## Release notes

Release notes come from a Play-style metadata directory: one file per locale named after the
`versionCode`.

```
metadata/
  en-US/changelogs/42.txt   # "Bug fixes"
  de-DE/changelogs/42.txt   # "Fehlerbehebungen"
```

```bash
google-play-publisher publish -p com.example.app -f app-release.aab \
  --changelog-dir metadata --version-code 42 --release-name 1.0.42
```

`--version-code` is required with `--changelog-dir`: it selects the changelog files and is
checked against the `versionCode` returned by the upload. Locales without a file for that
version code are skipped; when no file exists at all, the command fails with exit code 2 and
the path it checked. `--release-name` is optional, but the release name is what Play Console
shows next to the version code.

## Useful options

- `--dry-run` — upload the AAB and validate the edit without committing. The upload still
  consumes the `versionCode`, so never dry-run a release build.
- `--debug` — detailed HTTP, phase and retry logs (secrets are redacted).
- `--if-exists=skip` — idempotent rerun when the version code is already on the track, if the
  version code is known from `--metadata` or `--version-code`.
- `google-play-publisher completion bash|zsh|fish|powershell` — shell completion.

## Limitations

- The app must already exist in Play Console and have at least one uploaded build.
- The first app publication and legal declarations are done manually in Play Console.
- Managed publishing does not affect `internal`; for other tracks publishing may require
  a manual "Publish changes".
- The tool does not sign AABs.

## Documentation

- [Design and constraints](docs/README.md)
- [CLI contract](docs/03-cli-contract.md)
- [Play Console setup](docs/04-play-console-setup.md)
- [Errors and exit codes](docs/05-errors.md)
- [Development plan](TODO.md)
- [Project rules](AGENTS.md)

## Development

```bash
make build      # bin/google-play-publisher
make test       # go test ./...
make lint       # golangci-lint
make fmt        # gofumpt + go mod tidy
make snapshot   # goreleaser snapshot
```
