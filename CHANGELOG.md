# Changelog

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
