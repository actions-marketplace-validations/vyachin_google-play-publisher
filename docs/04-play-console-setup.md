# 04 — Настройка Play Console, GCP и подписи

Инструкция для человека, который вводит инструмент в эксплуатацию. Пока она не выполнена, `gplay doctor`
будет падать с кодом 3 или 4.

## 1. Предусловия

| Условие | Почему |
| --- | --- |
| Приложение уже создано в Play Console и имеет хотя бы одну загруженную сборку | API работает только с существующими приложениями |
| Первая публикация приложения выполнена вручную (включая юридические декларации) | API не умеет публиковать приложение и принимать согласия |
| AAB собран и подписан upload key | Инструмент не подписывает артефакты |
| Известен `applicationId` | Это ключ всех вызовов API |

## 2. Google Cloud: проект и API

1. Создать или выбрать проект в [Google Cloud Console](https://console.cloud.google.com/projectcreate).
2. Включить **Google Play Developer API**:
   [консоль API](https://console.developers.google.com/apis/api/androidpublisher.googleapis.com/) → Enable.
3. Создать service account (`gplay-publisher`) в разделе IAM → Service Accounts.

> Связывать GCP-проект с аккаунтом разработчика в Play Console больше не требуется
> ([Getting Started](https://developers.google.com/android-publisher/getting_started)), но service account
> всё равно добавляется пользователем в Play Console.

## 3. Play Console: доступ service account

1. Открыть [Users and permissions](https://play.google.com/console/users-and-permissions).
2. **Invite new users** → email service account (`gplay-publisher@<project>.iam.gserviceaccount.com`).
3. Выдать минимально необходимые права:

| Право | Зачем | Обязательность |
| --- | --- | --- |
| Release to testing tracks | `internal` | **Да** (для MVP достаточно только его) |
| Release to production, exclude devices, and use Play App Signing | `production`, роллауты | Нет в MVP |
| Manage store presence | только если позже добавим listing | Нет |
| Admin / Owner | — | **Нет, никогда** |

4. Ограничить доступ конкретными приложениями (App permissions), если аккаунт ведёт несколько приложений.
5. Отдельного подтверждения для service account не требуется — доступ активируется сразу.

Проверка: `gplay doctor --package <applicationId>` должен пройти все пункты.

## 4. Передача JSON-ключа приложению

Инструмент не создаёт, не хранит и не ротирует ключи — он лишь получает **путь к JSON-файлу service account**.
Жизненный цикл ключа (выпуск, хранение, ротация) — вне требований.

Способов передачи два:

1. **ADC (по умолчанию).** Переменная `GOOGLE_APPLICATION_CREDENTIALS` содержит путь к файлу:

   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json
   gplay publish -p com.example.app -f app-release.aab -t internal
   ```

2. **Явный флаг.** `--credentials /path/to/sa.json`; приоритетнее ADC, удобен для локального запуска.

Требования к файлу минимальны: это JSON-ключ service account, файл существует и читается процессом.
Как ключ попал на машину (секрет CI, том, локальный каталог) — ответственность пайплайна, не инструмента.

Проверка: `gplay doctor --package <applicationId>` — обмен токена должен пройти.

## 5. Подпись AAB

Play принимает только подписанный bundle. Подпись выполняется до вызова инструмента.

### Gradle (основной путь)

```kotlin
android {
    signingConfigs {
        create("release") {
            storeFile = file(System.getenv("UPLOAD_KEYSTORE")!!)
            storePassword = System.getenv("UPLOAD_STORE_PASSWORD")
            keyAlias = System.getenv("UPLOAD_KEY_ALIAS")
            keyPassword = System.getenv("UPLOAD_KEY_PASSWORD")
        }
    }
    buildTypes {
        release { signingConfig = signingConfigs.getByName("release") }
    }
}
```

```bash
./gradlew :app:bundleRelease
jarsigner -verify -verbose -certs app/build/outputs/bundle/release/app-release.aab
```

### bundletool (если AAB собирается вручную)

```bash
java -jar bundletool.jar build-bundle --modules=base.zip --output=app.aab
jarsigner -keystore upload.jks -sigalg SHA256withRSA -digestalg SHA-256 app.aab upload
```

### Play App Signing

- Загружаемый AAB подписывается **upload key**; Google пересобирает APK и подписывает их **app signing key**.
- Для новых приложений Play App Signing обязателен; ключ хранится у Google.
- При потере upload key запрашивается его сброс в Play Console — это не потеря приложения.
- Признак корректной настройки: `jarsigner -verify` на локальном AAB проходит, а Play не возвращает ошибок
  подписи при загрузке.

## 6. Managed publishing

**Что это.** Настройка Play Console (Publishing overview), которая разделяет два события: «изменения одобрены
ревью» и «изменения опубликованы». Обычно после одобрения Play публикует изменения сам. При включённом
managed publishing одобренные изменения остаются в разделе «Changes ready to publish», пока человек не нажмёт
**Publish changes**. Зачем включают: привязать публикацию к дате или рекламной кампании, проверить полный
список изменений и выпустить всё одним действием.

**Для `internal` это не применяется.** Play Console Help прямо указывает: «Internal test tracks are not covered
by Managed publishing» — изменения во внутреннем треке обычно становятся доступны сразу, без ревью. Поэтому
для нашего сценария (всегда `internal`) managed publishing не блокер, и его состояние можно не проверять.

Два исключения, когда ревью всё же потребуется:

- первый роллаут приложения выполняется в `internal`;
- предыдущая отправка была отклонена — следующая не станет доступна до одобрения.

**Если позже появятся `beta`/`production`.** Managed publishing удерживает роллауты релизов: после одобрения
понадобится ручной «Publish changes», а API этого не умеет. Проверить режим: Play Console → **Publishing
overview** → «Managed publishing status». Через API флаг не читается, поэтому инструмент возвращает
`"managedPublishing": "unknown"` и не делает вид, что публикация завершена.

## 7. Чек-лист ввода в эксплуатацию

- [ ] API `androidpublisher.googleapis.com` включён в проекте.
- [ ] Ключ передан инструменту: путь в `GOOGLE_APPLICATION_CREDENTIALS` или флаг `--credentials`.
- [ ] Service account приглашён в Play Console с правами только на релизы («Release to testing tracks»).
- [ ] `gplay doctor --package <id>` — зелёный.
- [ ] AAB подписан, `jarsigner -verify` — успех.
- [ ] Тестовая публикация в `internal` прошла, versionCode виден в `gplay status`.
- [ ] В CI настроена concurrency-группа (один запуск на приложение).
- [ ] Managed publishing не влияет на `internal`; если планируются другие треки — режим проверен.

## 8. Типичные ошибки настройки

| Симптом | Причина | Решение |
| --- | --- | --- |
| `401 invalid_grant` / файл не найден | Неверный путь к JSON-ключу или файл не читается | Передать корректный путь в `GOOGLE_APPLICATION_CREDENTIALS` или `--credentials` |
| `403 The caller does not have permission` | Service account не приглашён или нет прав на приложение | Invite + App permissions |
| `404 Application not found` | Неверный `applicationId` или нет доступа к приложению | Сверить package, проверить доступ |
| `403 API has not been used in project ...` | API не включён | Enable API |
| `400 ... cannot be sent for review automatically` | Приложение требует ручной отправки | Флаг `--changes-not-sent-for-review`, затем отправка из консоли |
| Релиз «прошёл», но тестеры не получили | Managed publishing (только closed/open/production) | Публикация вручную; для `internal` не актуально |
| `403 Daily edit creation quota exceeded` | Исчерпана суточная квота edits | Ждать сутки; убрать циклы «создал-удалил» |
| Ошибка подписи | AAB подписан не тем ключом / не подписан | Пересобрать с upload key |
