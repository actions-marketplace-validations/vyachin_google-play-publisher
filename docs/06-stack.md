# 06 — Стек и структура проекта

Документ фиксирует технологические решения. Инструмент будет расширяться новыми командами, поэтому стек
выбирается не «под одну публикацию», а под рост: тонкий CLI-слой, переиспользуемые use-case'ы, один адаптер
к Play API.

## 1. Принципы

1. **Stdlib по умолчанию.** Внешняя зависимость появляется только там, где stdlib даёт заметно худший результат.
2. **CLI — тонкий.** Команда разбирает флаги и вызывает use-case; бизнес-логика и HTTP живут вне `cobra`.
3. **Один адаптер к Play.** Все типы `androidpublisher` — только в `internal/play`.
4. **Тестируемость без сети.** Use-case'ы зависят от интерфейсов, а не от `*androidpublisher.Service`.
5. **Один бинарник, статическая сборка.** Кросс-платформенные релизы через goreleaser.

## 2. Стек

| Слой | Выбор | Почему | Когда пересмотреть |
| --- | --- | --- | --- |
| CLI-фреймворк | [`spf13/cobra`](https://github.com/spf13/cobra) + `pflag` | Подкоманды, группы, автодополнение, генерация man/help, зрелость | Если понадобится полностью декларативная схема — `kong` |
| Конфигурация | Флаги + env вручную | Минимум магии, предсказуемый приоритет | При появлении конфиг-файла — `koanf` |
| Логирование | `log/slog` (stdlib) | Структурные логи, уровни, JSON, нулевые зависимости | Практически никогда |
| Play API | `google.golang.org/api/androidpublisher/v3` | Полное покрытие Edits API, resumable upload | Если клиент отстанет — точечный REST ([ADR-0001](adr/0001-androidpublisher-go-client.md)) |
| Аутентификация | ADC через `option` (JSON-ключ по пути) | Стандарт Google, без ручного OAuth | Не планируется |
| Ретраи | Свой `internal/retry` (~80 строк) | Нужны конкретные правила: 5xx/429, джиттер, cap, «не ретраить commit» | Если политика усложнится — `cenkalti/backoff/v5` |
| AAB | `archive/zip` + `crypto/sha256` | Инспекция без bundletool | Если понадобится разбор манифеста — protobuf + Android resources |
| JSON | `encoding/json` | Стандарт, стабильные теги | Не планируется |
| Человеческий вывод | `fmt` + `text/tabwriter` | Таблицы без TUI-зависимостей | Если появится интерактив — `survey`/`bubbletea` |
| Ошибки/коды выхода | Свой `internal/exitcode` + `%w` | Единая точка маппинга ошибок в коды | Не планируется |
| Версия | `runtime/debug` + `-ldflags` | Версия и commit в бинарнике | Не планируется |
| Тесты | `testing`, `httptest`, golden-файлы + `testify/require`, `go-cmp` | Минимум, но удобные diff'ы | Не планируется |
| Линт | `golangci-lint` (staticcheck, govet, errcheck, revive, gofumpt) | Один инструмент в CI | Не планируется |
| Сборка/релиз | `Makefile` + `goreleaser` | Кросс-платформенные архивы, checksums, changelog | Если появится пакетный менеджер — добавить brew tap |
| CI | GitHub Actions | Уже используется | При переезде — GitLab CI |
| Безопасность зависимостей | `govulncheck` + Dependabot/Renovate | Уязвимости и обновления | Не планируется |

Прямых runtime-зависимостей — три: `cobra`, `google.golang.org/api`, `golang.org/x/oauth2` (последняя может
прийти транзитивно). Всё остальное — stdlib. `testify`/`go-cmp` — только в тестах.

## 3. Чего сознательно не берём

| Кандидат | Почему нет |
| --- | --- |
| `spf13/viper` | Тянет конфиг-файлы, watch, форматы; для флагов + env избыточен |
| `zap` / `logrus` | `slog` покрывает потребности, не нужен второй логгер |
| `urfave/cli` | Cobra даёт лучшую модель подкоманд и автодополнение |
| `wire` / `fx` | DI вручную: конструкторы и один `App`-структ проще отлаживать |
| `mockgen` | Интерфейсы маленькие; fakes пишутся руками и читаются лучше |
| TUI-библиотеки | MVP — неинтерактивный CLI для CI |

## 4. Дерево команд

```
gplay
├── publish     (group: Release)  — MVP
├── doctor      (group: Inspect)  — MVP
├── status      (group: Inspect)  — MVP
├── version                       — MVP
├── completion                    — из cobra, бесплатно
├── rollout     (group: Release)  — после MVP
└── promote     (group: Release)  — после MVP
```

Правило роста: новая команда = один файл в `internal/cli` + use-case в `internal/app` (если есть логика) +
тесты. `internal/play` не растёт, пока не появляется новый вызов API.

## 5. Структура пакетов

```
cmd/gplay/main.go              сборка App, Execute, os.Exit(code)
internal/cli/                  cobra: root + команды (тонкие)
  root.go                      PersistentFlags, groups, ExecuteContext
  publish.go                   флаги → app.Publish
  doctor.go                    флаги → app.Doctor
  status.go                    флаги → app.Status
  version.go
internal/app/                  use-case'ы, без cobra и без androidpublisher
  publish.go                   preflight → insert → upload → track → commit
  doctor.go
  status.go
internal/play/                 адаптер Play API: интерфейс API + реализация
internal/artifact/             инспекция AAB: ZIP, BundleConfig.pb, sha256, метаданные
internal/release/              домен: ReleasePlan, валидация статусов/дробей/notes
internal/retry/                классификация ошибок и backoff
internal/logging/              slog, уровни, debugTransport, redaction
internal/exitcode/             типы ошибок → коды выхода
internal/version/              версия, commit, дата сборки
```

Зависимости направлены внутрь: `cli → app → play/artifact/release`; `play` не знает про `cli`; `app` не знает
про `cobra` и про `*androidpublisher.Service`.

## 6. Соглашения по cobra

- **Никаких `init()`.** Дерево собирается конструкторами: `NewRootCmd(app *App) *cobra.Command`.
- **Глобальные флаги — `PersistentFlags`:** `--debug`, `--json`, `--log-format`, `--timeout`,
  `--credentials`, `--endpoint`. Доступны до и после имени команды.
- **`RunE`, не `Run`.** Ошибка возвращается наверх; `main` один раз печатает сообщение и вызывает `os.Exit`.
- **`SilenceUsage: true`, `SilenceErrors: true`.** При runtime-ошибке не печатается usage; usage — только при
  ошибке флагов (код 2).
- **Валидация аргументов** — `Args: cobra.NoArgs` (все параметры через флаги).
- **Группы** — `cobra.Group`: `Release` и `Inspect`, чтобы `gplay help` читался как продукт.
- **Автодополнение** — `ValidArgsFunction` для `--track` (сейчас статический список, позже — из API).
- **Тесты команд** — `cmd.SetArgs`, `cmd.SetOut/SetErr`, `cmd.ExecuteContext`; проверяем коды выхода и JSON,
  а не текст справки.
- **Env-биндинг** — в `PersistentPreRunE`, только для незаданных флагов (приоритет: флаг → env → дефолт).

## 7. App-структ и внедрение зависимостей

```go
type App struct {
    Stdout, Stderr io.Writer
    Version        string
    Now            func() time.Time
    NewAPI         func(ctx context.Context) (play.API, error) // ленивая инициализация
}
```

- `main` создаёт `App` и передаёт в `cli.NewRootCmd`.
- Команды получают `*App`, а не глобальные переменные.
- `play.API` создаётся лениво: `doctor` и `version` не должны требовать валидный ключ, если он не нужен.
- Часы и writer'ы подменяются в тестах.

## 8. Сборка и релиз

- `go build -trimpath -ldflags "-s -w -X main.version=... -X main.commit=... -X main.date=..."`.
- `goreleaser`: `linux/amd64`, `linux/arm64`, `darwin/arm64`, `darwin/amd64`, `windows/amd64`; tar.gz/zip,
  `checksums.txt`, changelog из коммитов.
- `Makefile`: `build`, `test`, `test-race`, `lint`, `cover`, `snapshot`.
- Версия также доступна из `debug.ReadBuildInfo` (commit, dirty).

## 9. Политика зависимостей

- Пин версий в `go.mod`; обновление — отдельным PR с прогоном тестов и `govulncheck`.
- Новая зависимость допускается, если: покрывает то, что stdlib не даёт; поддерживается; не тянет
  транзитивный лес; меньше ~1 МБ кода или очевидная польза.
- Запрещено добавлять зависимости ради «удобного сахара» в одном месте.
- `go mod tidy` и `go mod verify` — в CI.

## 10. Что понадобится дальше (по мере роста)

| Потребность | Решение | Триггер |
| --- | --- | --- |
| Конфиг-файл (несколько приложений/треков) | `koanf` + схема + валидация | Появление `gplay.yaml` |
| Несколько приложений за один запуск | `errgroup` + ограничение параллелизма | Команда `release --all` |
| Интерактивное подтверждение | `survey` или простой prompt + `--yes` | `promote` в production |
| Разбор манифеста AAB | protobuf + Android `Resources.proto` | Валидация до загрузки |
| Метрики/аудит | JSON-события в stderr + внешний сбор | Требование аудита |
