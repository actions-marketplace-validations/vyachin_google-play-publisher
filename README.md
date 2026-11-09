# google-play-publisher

A Go CLI that publishes a signed Android App Bundle (AAB) to Google Play.

**Status:** v0.1.0. Publishing targets the `internal` track.

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
# 1. Check access, the target track and the AAB identity
google-play-publisher doctor -p com.example.app -f app-release.aab

# 2. Publish to internal; package and versionCode come from the AAB
google-play-publisher publish -f app-release.aab -t internal

# 3. Show releases
google-play-publisher status -p com.example.app -t internal
```

The CLI reads `package`, `versionCode` and `versionName` from the AAB manifest. Passing
`--package` or `--version-code` is optional and acts as an assertion: a value that does not
match the AAB fails with exit code 2 before any network call. `doctor` applies the same
manifest checks when `--aab` is passed.

`--json` prints a machine-readable result to stdout; logs go to stderr:

```bash
google-play-publisher --json publish -p com.example.app -f app-release.aab > publish.json
```

## Release notes

Release notes live in the same metadata tree as listings and images: one file per locale named
after the `versionCode`. A changelog-only tree is valid.

```
metadata/
  en-US/changelogs/42.txt   # "Bug fixes"
  de-DE/changelogs/42.txt   # "Fehlerbehebungen"
```

```bash
google-play-publisher publish -f app-release.aab \
  --metadata-dir metadata --release-name 1.0.42
```

The AAB version code selects the changelog files, so `--version-code` is not required; when
supplied it is checked against the AAB and the upload. Locales without a file for that version
code are skipped; when the directory contains neither store metadata nor a changelog for the
AAB version code, the command fails with exit code 2 before any edit is created. `--release-name` is optional; when it is empty,
Play generates the name from the AAB version name.

## Store metadata

`--metadata-dir` applies store listings, images and tester groups in the same edit as the AAB,
so one commit publishes the bundle and its store presence together:

```
metadata/
  en-US/
    title.txt                # <= 30 characters
    short_description.txt    # <= 80 characters
    full_description.txt     # <= 4000 characters
    video.txt                # optional
    images/
      icon.png               # 512x512
      featureGraphic.png     # 1024x500
      phoneScreenshots/001.png
    changelogs/42.txt        # release notes
  testers/
    internal.txt             # one Google Group per line
```

```bash
google-play-publisher publish -f app-release.aab -t internal --metadata-dir metadata
```

The image sets and tester files that are present are authoritative: screenshots removed from a
directory are removed from Play when the edit commits, and an empty screenshot directory clears
that type. Every listing, image and tester file is validated before the API client is created
(text limits, PNG/JPEG, icon 512x512, feature graphic 1024x500, 8 MiB per file), so a typo fails
with exit code 2 instead of consuming an edit. The same directory also carries release notes
under `changelogs/`; a tree with only changelogs is accepted.

### Recreating the tree from Play

`pull` downloads the current listings, images, tester groups and release notes and writes a
metadata directory in the same layout, ready to commit:

```bash
google-play-publisher pull -p com.example.app --metadata-dir metadata
# alias
google-play-publisher download -p com.example.app --metadata-dir metadata
```

The target must be absent or empty. `--force` replaces a non-empty tree; the download is staged
and validated first, so a failure leaves the previous tree untouched. `--track` selects the
track whose release notes are pulled (default `internal`). Without `--version-code`, notes from
every release on the track are pulled; pass it to pull one specific release. Tester groups are
pulled for testing tracks only; production is skipped because Play exposes no testers there.
Pull creates a read-only edit, never commits it and deletes it best-effort, so it consumes the
daily edit quota but changes nothing in Play.

The generated images are full-size originals (not Play's preview thumbnails), and screenshots
are numbered in display order. A pulled tree round-trips through
`publish --metadata-dir`: review it, commit it, and publish the next release with the same
store presence.

## Staged rollouts and promotion

`rollout` changes a release that is already on a track without uploading a bundle:

```bash
# raise a staged rollout to 20% of users
google-play-publisher rollout -p com.example.app -t production --fraction 0.2

# halt it, then resume from the stored fraction
google-play-publisher rollout -p com.example.app -t production --version-code 42 --halt
google-play-publisher rollout -p com.example.app -t production --version-code 42 --fraction 0.2

# finish the rollout
google-play-publisher rollout -p com.example.app -t production --version-code 42 --complete
```

`promote` declares the version codes of one track on another, so no second build is needed:

```bash
google-play-publisher promote -p com.example.app --from internal --to beta
google-play-publisher promote -p com.example.app --from beta --to production \
  --version-code 42 --rollout 0.1 --yes
```

`--to production` requires `--yes`. `promote --metadata-dir` applies listings, images, testers
and release notes in the same edit. Release notes are read only together with `--version-code`;
without it, changelog files are ignored and the source release notes and name are reused.

## GitHub Actions

A setup action installs a released CLI and a publish action runs the same command as the CLI,
with key decoding, JSON capture and Job Summary:

```yaml
- name: Write the service-account key
  env:
    SA_JSON: ${{ secrets.GOOGLE_PLAY_SA_JSON }}
  run: |
    umask 077
    printf '%s' "$SA_JSON" > "$RUNNER_TEMP/sa.json"

- uses: vyachin/google-play-publisher@v0.1.0
  with:
    version: 0.1.0
    package: com.example.app
    aab: android/app/build/outputs/bundle/release/app-release.aab
    track: internal
    metadata-dir: android/metadata
    credentials-file: ${{ runner.temp }}/sa.json
```

`credentials-file` is the preferred mode: the value is passed to the CLI as
`--credentials /path/to/sa.json`. `credentials-base64` remains available when the secret is
stored base64-encoded.

See [docs/07-github-actions.md](docs/07-github-actions.md) for both usage patterns, credential
modes, outputs and troubleshooting. The actions are released as v0.1.0; do not use `@main` in
production.

## Useful options

- `--dry-run` — upload the AAB and validate the edit without committing. The upload still
  consumes the `versionCode`, so never dry-run a release build.
- `--debug` — detailed HTTP, phase and retry logs (secrets are redacted).
- `--if-exists=skip` — idempotent rerun when the AAB version code is already on the track.
- `--in-app-update-priority 0..5` — in-app update priority of the release; it cannot change once
  the rollout has started.
- `--country DE,AT --include-rest-of-world` — country targeting; Play accepts it only for an
  `inProgress` release on the `production` track.
- `pull --force` — replace an existing metadata directory after a successful, validated pull.
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
make test       # go test ./... and the action script tests
make lint       # golangci-lint
make fmt        # gofumpt + go mod tidy
make snapshot   # goreleaser snapshot
```
