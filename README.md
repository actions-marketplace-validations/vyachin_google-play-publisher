# gplay

CLI на Go для публикации подписанного Android App Bundle в Google Play.

**Статус:** v0.1.0. Публикация — во внутренний трек `internal`.

## Установка

Из исходников:

```bash
go install github.com/vyachin/google-play-publisher/cmd/gplay@v0.1.0
```

Или скачать бинарник из GitHub Releases (linux/darwin/windows, amd64/arm64) и проверить
`checksums.txt`.

## Аутентификация

Нужен JSON-ключ service account, добавленного в Play Console с правом
«Release to testing tracks». Путь передаётся одним из способов:

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json
# или
gplay publish ... --credentials /path/to/sa.json
```

Подробная настройка — [docs/04-play-console-setup.md](docs/04-play-console-setup.md).

## Быстрый старт

```bash
# 1. Проверить доступы и артефакт
gplay doctor -p com.example.app -f app-release.aab --metadata output-metadata.json

# 2. Опубликовать в internal
gplay publish -p com.example.app -f app-release.aab -t internal

# 3. Посмотреть релизы
gplay status -p com.example.app -t internal
```

`--json` даёт машиночитаемый результат в stdout, логи идут в stderr:

```bash
gplay --json publish -p com.example.app -f app-release.aab > publish.json
```

## Release notes

```json
{"en-US": "Bug fixes", "ru-RU": "Исправления"}
```

```bash
gplay publish -p com.example.app -f app-release.aab --release-notes notes.json
```

## Полезное

- `--dry-run` — загрузить AAB и провалидировать edit, не коммитя.
- `--debug` — детальные логи HTTP, фаз и ретраев (секреты редактируются).
- `--if-exists=skip` — идемпотентный повтор, если versionCode уже в треке.
- `gplay completion bash|zsh|fish|powershell` — автодополнение.

## Ограничения

- Приложение должно уже существовать в Play Console и иметь хотя бы одну сборку.
- Первая публикация приложения и юридические декларации — вручную в Play Console.
- Managed publishing не влияет на `internal`; для других треков публикация может требовать
  ручного «Publish changes».
- Инструмент не подписывает AAB.

## Документация

- [Дизайн и ограничения](docs/README.md)
- [CLI-контракт](docs/03-cli-contract.md)
- [Настройка Play Console](docs/04-play-console-setup.md)
- [Ошибки и коды выхода](docs/05-errors.md)
- [План разработки](TODO.md)

## Разработка

```bash
make build      # bin/gplay
make test       # go test ./...
make lint       # golangci-lint
make fmt        # gofumpt + go mod tidy
make snapshot   # goreleaser snapshot
```
