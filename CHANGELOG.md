# Changelog

## v0.1.0 — 2026-11-09

Первый релиз: публикация подписанного AAB во внутренний трек Google Play.

### Команды

- `gplay publish` — preflight, загрузка AAB, релиз в `internal`, коммит edit, `--dry-run`,
  `--json`, release notes, `--if-exists`.
- `gplay doctor` — доступ к API, трек, AAB, JAR-подпись, метаданные AGP.
- `gplay status` — релизы трека (read-only).
- `gplay completion` — bash/zsh/fish/powershell.

### Качество

- 83 % покрытия, тесты на fake Play API, `-race`, `golangci-lint`, `govulncheck`.
- Debug-режим с редактированием секретов.
- Релизные сборки goreleaser: linux/darwin/windows, amd64/arm64, checksums.

### Ограничения

- Первая публикация приложения — вручную в Play Console.
- Managed publishing не влияет на `internal`.
- AAB должен быть подписан; инструмент не подписывает артефакты.
