# TODO — план разработки `gplay-publisher`

**Статус:** планирование (design-only). Код ещё не создан.
**Цель:** CLI на Go, который публикует подписанный AAB во `internal` трек Google Play, с debug-режимом,
машиночитаемым результатом и расширяемой структурой команд.

## Как пользоваться

- Этапы идут последовательно; `S2` и `S3` можно вести параллельно после `S1`.
- Оценки — человеко-дни одного разработчика без учёта ревью и ожидания доступов.
- Этап закрывается только при выполнении **DoD**; «гейт» — то, без чего следующий этап не начинается.
- Чекбоксы отмечаются по факту; незапланированные работы добавляются в конец с пометкой `[unplanned]`.

## Сводка

| Этап | Результат | Оценка | Гейт |
| --- | --- | --- | --- |
| S0 | Доступы и тестовое приложение | 0.5–1 д | Ручная публикация в `internal` возможна |
| S1 | Каркас: cobra, App, logging, exitcode, CI | 1–1.5 д | `gplay --help` / `version` / тесты / линт |
| S2 | Play-адаптер | 2 д | AAB загружается и коммитится на тестовом приложении |
| S3 | AAB и домен релиза | 1 д | Невалидные входы отсекаются до сети |
| S4 | `publish` | 1.5–2 д | Публикация в `internal` end-to-end |
| S5 | `doctor`, `status` | 1 д | Диагностика и чтение релизов |
| S6 | Полировка CLI | 0.5–1 д | UX/справка/автодополнение |
| S7 | Качество и CI | 1–1.5 д | Зелёный пайплайн, интеграционный прогон |
| S8 | Релиз v0.1 | 0.5–1 д | Бинарники и инструкция |
| S9 | После MVP | 3–5 д | `rollout`/`promote` |

**Итого до v0.1: 9–12 рабочих дней + буфер 20%.**

---

## S0. Подготовка окружения — 0.5–1 д

> Пропущен по согласованию с владельцем: доступы к Play Console настраиваются параллельно.
> Проверка появится в S2 (интеграционный прогон).

- [ ] Выбрать тестовое приложение и `applicationId` (или согласовать использование рабочего).
- [ ] Убедиться, что в Play Console есть хотя бы одна загруженная сборка: без неё Edits API не работает.
- [ ] Включить `androidpublisher.googleapis.com` в GCP-проекте.
- [ ] Пригласить service account в Play Console с правом «Release to testing tracks».
- [ ] Положить JSON-ключ по известному пути; проверить передачу через `GOOGLE_APPLICATION_CREDENTIALS`.
- [ ] Подтвердить, что managed publishing не влияет на `internal` (официальное исключение).
- [ ] Зафиксировать текущий `versionCode` и `applicationId` тестового приложения.
- [ ] Проверить сборку: `jarsigner -verify` на AAB проходит.

**DoD:** ручная публикация в `internal` через консоль работает; доступ по ключу проверен;
известны package и следующий `versionCode`.

**Гейт:** без S0 нельзя начинать S2 (некуда публиковать и нечем авторизоваться).

## S1. Каркас проекта — 1–1.5 д — выполнено

- [x] `go.mod`, module path, версия Go; `go mod tidy`.
- [x] `cmd/gplay/main.go`: сборка `App`, `cli.Execute`, `os.Exit(code)`.
- [x] `internal/cli/root.go`: cobra root + `PersistentFlags`
      (`--debug`, `--json`, `--log-format`, `--timeout`, `--credentials`, `--endpoint`).
- [x] `App`: `Stdout/Stderr`, `Now`; фабрика `play.API` — в S2 (когда появится интерфейс).
- [x] `internal/logging`: slog, уровни, `text`/`json`, включение `DEBUG`.
- [x] `internal/exitcode`: типы ошибок и маппинг 0–8/130; `SilenceUsage/SilenceErrors`.
- [x] `internal/version` + `gplay version` (ldflags).
- [x] `Makefile` (`build`, `test`, `test-race`, `lint`, `cover`, `snapshot`).
- [x] `.golangci.yml`, GitHub Actions: `build`, `test`, `lint`.
- [x] Тесты: глобальные флаги, коды выхода, `version`.
- [ ] Группы `Release`/`Inspect` — добавить в S4/S5 вместе с командами (пустые группы портят `--help`).

**DoD:** `gplay --help`, `gplay version`, `gplay --debug version` работают; тесты и линт зелёные;
panic/exit в `main` только в одном месте.

## S2. Play-адаптер — 2 д — выполнено (кроме live-прогона)

- [x] `internal/play`: интерфейс `API` и реализация на `androidpublisher`.
- [x] Аутентификация: ADC и `--credentials` через `cloud.google.com/go/auth`, scope `androidpublisher`,
      User-Agent в transport; таймаут — контекстом операции.
- [x] Методы: `CreateEdit`, `DeleteEdit`, `UploadBundle`, `UpdateTrack`, `ValidateEdit`, `CommitEdit`,
      `TrackReleases` (read-only через `applications.tracks.releases.list`).
- [x] Загрузка: `ChunkSize` 8 MiB, `ChunkRetryDeadline` 30 s, `ProgressUpdater` (INFO каждые 5 %);
      маленькие файлы уходят multipart, большие — resumable.
- [x] Маппинг `googleapi.Error` → типизированные ошибки по [05-errors.md](docs/05-errors.md).
- [x] `debugTransport`: HTTP-обмен, тела ≤ 4 KiB, redaction; секреты вырезаются.
- [x] Fake API на `httptest` + `option.WithEndpoint`; тесты happy path и 400/401/403/404/429/5xx,
      а также multipart- и resumable-загрузки.
- [x] Env-gated контрактный тест (`GPLAY_INTEGRATION=1`).
- [ ] Live-прогон на тестовом приложении — после настройки доступов (S0).

**DoD:** unit-часть закрыта; на реальном приложении AAB ещё не загружался (нет доступов).
**Гейт:** без маппинга ошибок не начинать S4 (иначе нечего показывать пользователю) — закрыт.

## S3. AAB и домен релиза — 1 д — выполнено

- [x] `internal/artifact`: проверка ZIP и `BundleConfig.pb`, sha256, чтение `output-metadata.json`.
- [x] `internal/release`: `Plan`, валидация `status`/`rollout`, release notes ≤ 500 символов,
      имена треков, `--if-exists`.
- [x] Табличные тесты на все комбинации флагов (код 2 до сети).
- [x] Проверка соответствия `package` метаданным.

**DoD:** невалидный файл/флаги/локаль отсекаются до создания edit; тесты покрывают границы.

## S4. Команда `publish` — 1.5–2 д — выполнено (кроме live-прогона)

- [x] Use-case `internal/app/publish.go`: `insert → upload → tracks.update → commit`.
- [x] Preflight: AAB/BundleConfig.pb, метаданные AGP, версия в треке, `--if-exists=fail|skip`, доступ к треку.
- [x] `--dry-run`: `validate` + удаление edit.
- [x] Очистка edit при ошибке и отмене контекста (короткий контекст, best effort).
- [x] Коммит: `ERROR_IF_IN_REVIEW` по умолчанию, `--on-in-review=replace`,
      повтор с `changesNotSentForReview=true` и код 8.
- [x] Неоднозначный коммит: проверка релизов до повтора, повтор только для `Transient`.
- [x] JSON-результат по схеме [03-cli-contract.md](docs/03-cli-contract.md#5-json-результат).
- [x] Тесты сценариев сбоев: upload, commit, retry, ambiguous, skip, dry-run.
- [ ] Live-прогон на тестовом приложении — после настройки доступов (S0).

**DoD:** unit/CLI-часть закрыта; публикация на реальном приложении ещё не проверялась.

## S5. `doctor` и `status` — 1 д — выполнено (кроме live-прогона)

- [x] `doctor`: доступ к API и приложению, трек, AAB (ZIP/`BundleConfig.pb`), подпись (эвристика),
      соответствие метаданным.
- [x] `status`: релизы трека, состояние и version codes, `--json`; read-only.
- [x] Тесты с fake API; понятные сообщения (код 3/4/7).

**DoD:** unit/CLI-часть закрыта; проверка на реальном аккаунте — после настройки доступов (S0).

## S6. Полировка CLI — 0.5–1 д

- [ ] `gplay completion bash|zsh|fish|powershell`, `ValidArgsFunction` для `--track`.
- [ ] `Short`/`Long`/`Example` для всех команд; группы в `gplay help`.
- [ ] `--json` + `--debug`: `stdout` — один объект, логи — в `stderr`.
- [ ] Критерии приёмки debug-режима из [03-cli-contract.md](docs/03-cli-contract.md#7-debug-режим).
- [ ] Сообщения об ошибках: проблема → причина → действие.

**DoD:** новая команда добавляется без правки `main`; справка читается; completion работает.

## S7. Качество и CI — 1–1.5 д

- [ ] Покрытие: `app`, `play`, `release`, `artifact` — не ниже 80%; `cli` — по ключевым путям.
- [ ] `go test -race`, `golangci-lint`, `govulncheck` в CI.
- [ ] Dependabot/Renovate; пин `google.golang.org/api`.
- [ ] Интеграционный job: только по метке/вручную, отдельное тестовое приложение.
- [ ] Golden-тест debug-лога: нет `private_key`, `Authorization`, токенов.
- [ ] Проверка отсутствия флаки-тестов (3 прогона подряд).

**DoD:** пайплайн зелёный на пустом PR; интеграционный прогон воспроизводим; артефакты логов без секретов.

## S8. Релиз v0.1 — 0.5–1 д

- [ ] `goreleaser`: linux/darwin/windows, amd64/arm64, tar.gz/zip, checksums, changelog.
- [ ] `-trimpath -ldflags "-s -w -X main.version=..."`; версия видна в `gplay version`.
- [ ] Smoke-тест linux/amd64 в контейнере.
- [ ] Инструкция: установка, `doctor`, первый `publish` в `internal`.
- [ ] Тег `v0.1.0`, заметки о релизе.

**DoD:** бинарник скачивается, запускается, публикует на тестовом приложении; документация совпадает с
поведением.

## S9. После MVP — 3–5 д

- [ ] `rollout`: increase/halt/complete (production, `userFraction`).
- [ ] `promote`: `internal → beta`, подтверждение для production.
- [ ] `--config`: файл с несколькими приложениями (koanf), если понадобится.
- [ ] Несколько приложений за запуск (`errgroup`), если понадобится.
- [ ] Разбор манифеста AAB (protobuf) — только при необходимости валидации до загрузки.
- [ ] Метрики/аудит: JSON-события, внешний сбор.

## Риски и буфер

| Риск | Влияние | Мера |
| --- | --- | --- |
| Нет тестового приложения/доступов | S0 и S2 стоят | Согласовать на старте; можно использовать рабочее приложение с `internal` |
| Квота edits исчерпана | Публикации недоступны до суток | Один edit на запуск, интеграционные тесты — редко |
| Изменения API/клиента | Ломаются тесты | Пин версии, контрактный тест |
| Managed publishing на других треках | Публикация «зависает» | В MVP не актуально; учесть в `promote` |
| Оценки оптимистичны | Сдвиг сроков | Буфер 20%, S9 не входит в v0.1 |

## Definition of Done для v0.1

- [ ] `gplay publish -p <id> -f app.aab -t internal` публикует сборку end-to-end.
- [ ] `--json` даёт стабильный результат; коды выхода соответствуют [03-cli-contract.md](docs/03-cli-contract.md#4-коды-выхода).
- [ ] `--debug` показывает HTTP-обмен и не раскрывает секреты.
- [ ] `doctor` объясняет проблемы доступов.
- [ ] Тесты, линт, `-race`, `govulncheck` зелёные.
- [ ] Релизные бинарники собираются goreleaser'ом.
- [ ] Документация в `docs/` соответствует реализации.
