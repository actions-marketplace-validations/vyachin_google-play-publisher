# gplay-publisher — публикация подписанного AAB в Google Play

**Статус:** проектирование (design-only). Код намеренно не создаётся: пакет документов фиксирует ответ на вопрос «возможно ли это», границы применимости и контракт будущего CLI.

## Короткий ответ

**Да, возможно.** Публикация подписанного AAB в Google Play полностью автоматизируется через
[Google Play Developer Publishing API v3](https://developers.google.com/android-publisher) (`androidpublisher.googleapis.com`).
Нужный сценарий покрывается одной транзакционной «правкой» (edit):

```
edits.insert → bundles.upload → tracks.update → edits.commit
```

Для Go есть автогенерируемый клиент [`google.golang.org/api/androidpublisher/v3`](https://pkg.go.dev/google.golang.org/api/androidpublisher/v3)
(актуальная версия на момент проектирования — `v0.311.0`, 04.11.2026). Он содержит все необходимые вызовы,
включая resumable-загрузку, `changesNotSentForReview` и `changesInReviewBehavior`.

Ориентировочная трудоёмкость MVP: 1–2 дня на `publish` + `doctor` с тестами; 1 неделя — до production-grade
(ретраи, разбор ошибок Play, CI, документация для команды).

## Чего через API сделать нельзя

Это жёсткие ограничения платформы, а не недоработка инструмента. Их нужно принять до начала реализации.

| Ограничение | Следствие для инструмента |
| --- | --- |
| Приложение должно уже существовать в Play Console и иметь хотя бы одну загруженную сборку | Первую публикацию нового приложения делает человек через Play Console ([Edits](https://developers.google.com/android-publisher/edits)) |
| Нельзя создать приложение, сменить статус «published → unpublished», заполнить юридические согласия | Эти действия остаются в Play Console |
| При managed publishing публикация в тестовых/продакшен-треках задерживается | На `internal` это не распространяется: внутренние сборки обычно доступны сразу. Для `beta`/`production` публикация была бы ручной |
| Параллельная работа в Play Console и через API ломает edit | Инструмент падает быстро и просит повторить запуск; конкурентные изменения не «мержатся» |
| Один пользователь — один активный edit на приложение | Два одновременных запуска в CI недопустимы; нужен concurrency-lock на уровне пайплайна |
| Правила Google Play Developer API | Нельзя предлагать этот инструмент третьим лицам как сервис публикации ([API Usage Instructions](https://developers.google.com/android-publisher/api_usage)) |
| Подпись | Инструмент **не подписывает** AAB; он публикует уже подписанный артефакт (upload key / Play App Signing) |

## Принятые решения (2026-11-09)

| Решение | Следствие |
| --- | --- |
| Публикуем всегда во `internal` | MVP не трогает `beta` и `production`; флаг `--track` остаётся, но другие треки — вне MVP |
| Аутентификация — JSON-ключ service account | Инструмент получает путь к файлу через `GOOGLE_APPLICATION_CREDENTIALS` или `--credentials`; выпуск и ротация ключа — вне scope ([Setup](04-play-console-setup.md#4-передача-json-ключа-приложению)) |
| Managed publishing | На `internal` не действует (официальное исключение), для MVP не блокер; при выходе на другие треки — учесть ([подробно](04-play-console-setup.md#6-managed-publishing)) |
| Debug-режим обязателен | `--debug` (`-d`, `GPLAY_DEBUG=1`): HTTP-обмен, ретраи, тела ошибок — в `stderr`; секреты редактируются ([CLI](03-cli-contract.md#7-debug-режим)) |

## Что делает инструмент (scope MVP)

1. Проверяет артефакт и доступы (`doctor`).
2. Загружает подписанный AAB во внутренний трек `internal`.
3. Выставляет статус релиза: `completed` (обычный дефолт) или `draft`; поэтапный роллаут — вне MVP.
4. Прикладывает release notes по локалям.
5. Коммитит правку и возвращает машиночитаемый результат (version code, sha256, статус, edit id).
6. Безопасно обрабатывает конфликты состояния (инвалидированный edit, повтор versionCode) и не ждёт ревью:
   сборки в `internal` обычно доступны сразу.
7. Поддерживает `--debug`: детальный лог каждого шага и HTTP-обмена в `stderr`, с редактированием секретов.

## Non-goals MVP

- управление store listing (иконки, скриншоты, описания);
- управление тестовыми группами и списками тестировщиков;
- публикация в `beta`/`production`, поэтапный роллаут и promotion между треками (`rollout`/`promote` — после MVP);
- подпись AAB и сборка (это Gradle/bundletool);
- создание приложения и первая публикация;
- работа с APK (только AAB);
- локальная база состояния и «умные» миграции — инструмент stateless (см. [ADR-0002](adr/0002-stateless-single-edit.md)).

## Ключевые проектные решения

| Решение | Документ |
| --- | --- |
| Официальный Go-клиент + Edits API, а не самописный REST или fastlane | [ADR-0001](adr/0001-androidpublisher-go-client.md) |
| Stateless-модель, один edit на запуск, идемпотентность по versionCode | [ADR-0002](adr/0002-stateless-single-edit.md) |
| По умолчанию не отменять changes in review (`ERROR_IF_IN_REVIEW`) | [Архитектура](02-architecture.md#5-политика-ревью) |
| Cobra + слои `cli → app → play` | [Стек](06-stack.md), [ADR-0003](adr/0003-cobra-command-layout.md) |
| CLI-контракт, коды выхода, JSON для CI | [CLI](03-cli-contract.md) |
| Настройка Play Console, GCP, передача ключа, подпись | [Setup](04-play-console-setup.md) |
| Каталог ошибок Play и политика ретраев | [Ошибки](05-errors.md) |

## Документы

- [01 — Возможности и ограничения API](01-api-feasibility.md)
- [02 — Архитектура](02-architecture.md)
- [03 — Контракт CLI](03-cli-contract.md)
- [04 — Настройка Play Console и доступов](04-play-console-setup.md)
- [05 — Ошибки и ретраи](05-errors.md)
- [06 — Стек и структура проекта](06-stack.md)
- [ADR-0001 — Go-клиент и Edits API](adr/0001-androidpublisher-go-client.md)
- [ADR-0002 — Stateless и один edit](adr/0002-stateless-single-edit.md)
- [ADR-0003 — Cobra и слои команд](adr/0003-cobra-command-layout.md)
- [План разработки по этапам](../TODO.md)

Внешние источники, на которые опирается проект:

- [Google Play Developer API — Edits](https://developers.google.com/android-publisher/edits)
- [Google Play Developer API — APKs and Tracks](https://developers.google.com/android-publisher/tracks)
- [Google Play Developer API — Uploading Files](https://developers.google.com/android-publisher/upload)
- [Google Play Developer API — Quotas](https://developers.google.com/android-publisher/quotas)
- [Concurrency considerations](https://developers.google.com/android-publisher/concurrency-considerations)
- [API Usage Instructions](https://developers.google.com/android-publisher/api_usage)
- [Play Console Help — размеры приложений](https://support.google.com/googleplay/android-developer/answer/9859372)

## Оставшиеся вопросы

1. **Формат release notes.** Предлагается JSON-файл `{"en-US": "...", "ru-RU": "..."}`; альтернатива — каталог
   `<locale>.txt`, как в fastlane.
2. **Нужны ли `promote`/`rollout`** в ближайшей итерации или достаточно `publish` в `internal`.
3. **Другие треки.** Для `internal` managed publishing не важен; если позже появятся `beta`/`production`,
   понадобится решение, как публиковать после ревью (ручной «Publish changes» либо выключенный managed publishing).
