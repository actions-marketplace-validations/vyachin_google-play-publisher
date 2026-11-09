# 03 — Контракт CLI

Документ фиксирует внешний интерфейс: команды, флаги, коды выхода и форматы вывода. Это контракт для CI и для
будущей реализации; менять его без обновления документа нельзя.

## 1. Общие правила

- Один бинарник `gplay` (рабочее имя; альтернативы: `gpp`, `playpub`).
- `stdout` — результат (данные), `stderr` — логи и диагностика. В JSON-режиме в `stdout` попадает **только**
  один JSON-объект.
- Коды выхода — стабильные, см. §4. CI принимает решения по ним, а не по тексту.
- Секреты не попадают ни в вывод, ни в логи.
- Команды идемпотентны в описанном смысле: повторный запуск после успеха — no-op (`--if-exists=skip`) либо
  понятная ошибка.

### Глобальные флаги

Флаги root-команды (cobra `PersistentFlags`), действуют для всех подкоманд и принимаются до или после имени
команды: `gplay --debug publish ...` ≡ `gplay publish --debug ...`.

| Флаг | Тип | По умолчанию | Описание |
| --- | --- | --- | --- |
| `--debug`, `-d` | bool | `false` | Детальное логирование в `stderr` (HTTP, ретраи, тела ошибок) |
| `--json` | bool | `false` | Машиночитаемый результат в `stdout` |
| `--log-format` | enum | `text` | `text` \| `json` — формат логов в `stderr` |
| `--timeout` | duration | `20m` | Общий дедлайн операции |
| `--credentials` | path | ADC | Путь к JSON-ключу; приоритетнее ADC |
| `--endpoint` | url | prod | Для тестов на `httptest` |

## 2. Команды

Дерево команд (cobra), группы для читаемого `gplay help`:

```
gplay
├── publish      Release
├── doctor       Inspect
├── status       Inspect
├── version
├── completion   (генерируется cobra)
└── rollout, promote — после MVP
```

`gplay completion bash|zsh|fish|powershell` и `gplay help` доступны без дополнительного кода.

### `gplay publish` — MVP

Загружает AAB, объявляет релиз в треке, коммитит. **Целевой трек MVP — `internal`**; другие треки появятся
вместе с `promote`/`rollout`.

```
gplay publish --package com.example.app --aab app/build/outputs/bundle/release/app-release.aab \
  --track internal
```

| Флаг | Тип | По умолчанию | Описание |
| --- | --- | --- | --- |
| `--package`, `-p` | string | из `--metadata` | `applicationId`; обязателен, если нет метаданных |
| `--aab`, `-f` | path | — | Путь к подписанному AAB; обязателен |
| `--track`, `-t` | string | `internal` | Имя трека; в MVP используется `internal` |
| `--status` | enum | `completed` | `completed` \| `draft`; `inProgress` — post-MVP |
| `--rollout` | float | `0` | Post-MVP: доля пользователей, `0 < f < 1` |
| `--release-notes` | path | — | JSON `{"en-US": "текст", "ru-RU": "текст"}`, ≤ 500 символов на локаль |
| `--changes-not-sent-for-review` | bool | `false` | Не отправлять на ревью |
| `--on-in-review` | enum | `error` | `error` → `ERROR_IF_IN_REVIEW`; `replace` → `CANCEL_IN_REVIEW_AND_SUBMIT` |
| `--if-exists` | enum | `fail` | `fail` \| `skip` — что делать, если versionCode уже в треке |
| `--dry-run` | bool | `false` | Полный прогон без `commit` + удаление edit |
| `--metadata` | path | — | `output-metadata.json` от AGP: `applicationId`, `versionCode` |

Остальные флаги (`--debug`, `--json`, `--log-format`, `--timeout`, `--credentials`, `--endpoint`) — глобальные,
см. §1.

Валидация комбинаций (код 2 при нарушении, `--rollout`/`inProgress` — post-MVP):

- `--rollout` задан, `--status` не задан → статус `inProgress`;
- `--rollout` вместе с явным `--status=completed` → ошибка;
- `--status=inProgress` без `--rollout` → ошибка;
- `--rollout` вне диапазона `(0,1)` → ошибка;
- `--changes-not-sent-for-review` вместе с `--dry-run` → ошибка.

`--dry-run` — это не «ничего не делать»: AAB реально загружается, edit валидируется и удаляется. Это расходует
трафик и создаёт edit (суточная квота). В выводе это явно указывается.

### `gplay doctor` — MVP

Проверяет готовность окружения, ничего не меняет. Без `--aab` проверяются только доступ и трек.

| Проверка | Как |
| --- | --- |
| `api` | токен и доступ к приложению (read-only запрос релизов) |
| `track` | трек существует, показано число релизов |
| `artifact` | ZIP с `BundleConfig.pb`, размер и sha256 |
| `signature` | наличие JAR-подписи (`META-INF/*.RSA` / `*.EC` / `*.DSA`); отсутствие — код 7 |
| `metadata` | если передан `--metadata`: `applicationId` совпадает с `--package`, `versionCode > 0` |

### `gplay status` — MVP (read-only)

Показывает треки и релизы, версии и статусы. Использует read-only метод
`applications.tracks.releases.list`; если он недоступен в используемой версии клиента — одноразовый edit
(`insert → tracks.list → delete`), что дороже по квоте.

```
gplay status --package com.example.app [--track production] [--json]
```

### `gplay rollout` — после MVP

```
gplay rollout --package ... --track production --version-code 42 --fraction 0.2
gplay rollout --package ... --track production --version-code 42 --halt
gplay rollout --package ... --track production --version-code 42 --complete
```

Реализуется тем же `insert → tracks.update → commit`, но без загрузки AAB.

### `gplay promote` — после MVP

```
gplay promote --package ... --from internal --to beta
gplay promote --package ... --from beta --to production --rollout 0.1
```

Семантика: взять versionCode из исходного трека и объявить его релизом в целевом. Требует явного подтверждения
для production (`--yes` или интерактивный prompt).

## 3. Конфигурация

Приоритет: флаги → переменные окружения → значения по умолчанию. Конфиг-файл в MVP не поддерживается
(меньше способов ошибиться).

| Переменная | Аналог флага |
| --- | --- |
| `GOOGLE_APPLICATION_CREDENTIALS` | Путь к JSON-ключу (ADC); альтернатива — `--credentials` |
| `GPLAY_PACKAGE` | `--package` |
| `GPLAY_TRACK` | `--track` |
| `GPLAY_TIMEOUT` | `--timeout` |
| `GPLAY_DEBUG` | `--debug` (значение `1`/`true`) |
| `NO_COLOR` | отключение цвета в текстовых логах |

## 4. Коды выхода

| Код | Имя | Когда |
| --- | --- | --- |
| 0 | `OK` | Успех; также идемпотентный no-op (`--if-exists=skip`) |
| 1 | `INTERNAL` | Непредвиденная ошибка/паника |
| 2 | `USAGE` | Неверные флаги, отсутствует файл, несовместимая комбинация |
| 3 | `AUTH` | 401/403: неверный путь к ключу, нет прав, service account не добавлен, API не включён |
| 4 | `NOT_FOUND` | Приложение или трек не найдены, нет доступа к приложению |
| 5 | `CONFLICT` | Edit инвалидирован, изменения на ревью (`ERROR_IF_IN_REVIEW`), versionCode уже существует |
| 6 | `TRANSIENT` | Сеть/5xx после исчерпания ретраев, обрыв загрузки, временные лимиты (в т.ч. суточная квота edits) |
| 7 | `REJECTED` | Play отклонил изменения: подпись, versionCode, release notes, конфигурация трека |
| 8 | `MANUAL_ACTION` | Изменения сохранены, но требуют ручной отправки на ревью |
| 130 | `INTERRUPTED` | SIGINT/SIGTERM; edit удалён best-effort |

Правило: код отражает **действие**, которое должен предпринять человек/пайплайн, а не HTTP-статус.
Детали — в [05-errors.md](05-errors.md).

## 5. JSON-результат

Успех:

```json
{
  "ok": true,
  "command": "publish",
  "package": "com.example.app",
  "track": "internal",
  "status": "completed",
  "versionCode": 42,
  "sha256": "b1946ac92492d2347c6235b4d2611184...",
  "editId": "0123456789",
  "review": "sent",
  "managedPublishing": "unknown",
  "durationMs": 41230
}
```

Ошибка:

```json
{
  "ok": false,
  "command": "publish",
  "package": "com.example.app",
  "error": {
    "code": "version_already_used",
    "exitCode": 5,
    "message": "versionCode 42 already exists in track internal",
    "apiCode": 400,
    "reason": "badRequest",
    "hint": "Bump versionCode and rebuild the AAB, or pass --if-exists=skip"
  }
}
```

Поля:

| Поле | Значения |
| --- | --- |
| `review` | `sent` \| `notSent` \| `replaced` |
| `managedPublishing` | `unknown` — API не отдаёт флаг; для `internal` не применяется |
| `error.code` | стабильный machine-readable идентификатор |
| `error.exitCode` | дублирует код процесса для парсеров |

Схема версионируется полем `schemaVersion` при первом же изменении; до тех пор — `1` неявно.

## 6. Примеры

### Локально, внутренний трек

```bash
export GOOGLE_APPLICATION_CREDENTIALS=$HOME/.config/gplay/sa.json
gplay doctor --package com.example.app
gplay publish -p com.example.app -f app-release.aab -t internal --release-notes notes.json
```

### CI (GitHub Actions, фрагмент)

Инструменту нужен только путь к JSON-ключу; как файл оказался на раннере — вне зоны ответственности инструмента.

```yaml
- name: Publish to internal
  env:
    GOOGLE_APPLICATION_CREDENTIALS: /path/to/gplay-sa.json  # путь к JSON-ключу
  run: |
    gplay publish \
      --package "$PACKAGE" \
      --aab app/build/outputs/bundle/release/app-release.aab \
      --track internal \
      --json > publish.json
```

Альтернатива — без переменной окружения: `gplay publish --credentials /path/to/gplay-sa.json ...`.

Требования к пайплайну:

- `concurrency`-группа на приложение (один edit на пользователя; параллельные запуски запрещены);
- service account только с правами на релизы (для MVP достаточно «Release to testing tracks»);
- managed publishing на `internal` не действует; при выходе на другие треки — выключить либо предусмотреть ручной «Publish changes»;
- сохранение `publish.json` как артефакта (versionCode, editId — для аудита);
- при разборе сбоя запускать с `--debug`: секреты в лог не попадают, артефакт с логом безопасен.

### После MVP: production и поэтапный роллаут

Эти сценарии появятся вместе с командами `rollout`/`promote` и отдельным ручным подтверждением; в MVP не
используются.

```bash
gplay publish -p com.example.app -f app-release.aab \
  -t production --status inProgress --rollout 0.05 \
  --release-notes notes.json
# позже:
gplay rollout -p com.example.app -t production --version-code 42 --fraction 0.2
gplay rollout -p com.example.app -t production --version-code 42 --complete
```

## 7. Debug-режим

`--debug` (короткий `-d`, переменная `GPLAY_DEBUG=1`) включает **детальное логирование в stderr**. Режим не
меняет поведение: ни порядок вызовов, ни таймауты, ни код выхода — только объём логов.

| Что | Обычный режим | `--debug` |
| --- | --- | --- |
| Фазы (`preflight`, `insert`, `upload`, `track`, `commit`, `cleanup`) и их длительность | да | да |
| `package`, `track`, `versionCode`, `editId`, `sha256`, размер AAB | да | да |
| HTTP: метод, URL, номер попытки, статус, длительность, размер | нет | **да** |
| Заголовки запроса/ответа (кроме секретных) и correlation id | нет | **да** |
| Тела запросов/ответов (JSON, до 4 KiB) | только сообщение ошибки | **да** |
| Прогресс resumable-загрузки | раз в 5% | раз в секунду: байты, процент, текущий чанк |
| Решения о ретраях и backoff | нет | **да** |
| Источник каждого параметра (флаг / env / дефолт) | нет | **да** |
| Стектрейс для `INTERNAL` | нет | **да** |

Redaction — в лог **никогда** не попадают:

- заголовки `Authorization`, `Proxy-Authorization` и любые OAuth-токены;
- содержимое JSON-ключа (`private_key` и т.п.); путь к файлу логировать можно;
- байты AAB — только размер и sha256;
- тела усекаются до 4 KiB, бинарные тела не дампятся.

Правила вывода:

- debug-логи всегда идут в `stderr`; `stdout` остаётся чистым — при `--json` там по-прежнему ровно один объект;
- `--log-format=json` + `--debug` дают JSON-строки уровня `DEBUG` в `stderr`;
- при `--debug` HTTP-обмен логируется обёрткой над `http.RoundTripper`: для обычных запросов — тело с
  восстановлением, для `bundles.upload` — только заголовки, `Content-Range`/`Content-Length` и прогресс.

Пример:

```bash
gplay publish -p com.example.app -f app-release.aab -t internal --debug
```

```
time=... level=DEBUG msg="config" package=com.example.app (flag) credentials=/path/sa.json (env)
time=... level=DEBUG msg="http request" method=POST url=".../applications/com.example.app/edits" attempt=1
time=... level=DEBUG msg="http response" status=200 duration=412ms bytes=57
time=... level=INFO  msg="bundle uploaded" versionCode=42 sha256=b1946ac9... size=48MB duration=31s
time=... level=DEBUG msg="commit" inReviewBehavior=ERROR_IF_IN_REVIEW changesNotSentForReview=false
```

Критерии приёмки (проверяются тестами и на демо-прогоне):

- в debug-логе видны все HTTP-вызовы публикации — `edits.insert`, `bundles.upload`, `tracks.update`,
  `edits.commit` — с методом, URL, статусом, длительностью и номером попытки;
- `--debug --json`: `stdout` — валидный ровно один JSON-объект, логи — только в `stderr`;
- поиск по логу не находит `private_key`, `Authorization` и значения токенов;
- `GPLAY_DEBUG=1` даёт тот же уровень, что и `--debug`;
- включение/выключение debug не меняет код выхода и последовательность вызовов.

## 8. Совместимость

- Обратная совместимость флагов и кодов выхода обязательна в пределах мажорной версии.
- Новые флаги — только с дефолтами, сохраняющими прежнее поведение.
- Изменение JSON-схемы — только с повышением `schemaVersion`.
