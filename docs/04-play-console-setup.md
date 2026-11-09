# 04 — Play Console, GCP and signing setup

Instructions for the person who puts the tool into service. Until this is done,
`google-play-publisher doctor` fails with code 3 or 4.

## 1. Prerequisites

| Requirement | Why |
| --- | --- |
| The app already exists in Play Console and has at least one uploaded build | The API only works with existing apps |
| The first app publication was done manually (including legal declarations) | The API cannot publish an app or accept consents |
| The AAB is built and signed with the upload key | The tool does not sign artifacts |
| The `applicationId` is known | It is the key of every API call |

## 2. Google Cloud: project and API

1. Create or select a project in
   [Google Cloud Console](https://console.cloud.google.com/projectcreate).
2. Enable **Google Play Developer API**:
   [API console](https://console.developers.google.com/apis/api/androidpublisher.googleapis.com/)
   → Enable.
3. Create a service account (`google-play-publisher`) under IAM → Service Accounts.

> Linking a GCP project to the Play developer account is no longer required
> ([Getting Started](https://developers.google.com/android-publisher/getting_started)), but
> the service account still has to be added to Play Console as a user.

## 3. Play Console: service account access

1. Open [Users and permissions](https://play.google.com/console/users-and-permissions).
2. **Invite new users** → the service account email
   (`google-play-publisher@<project>.iam.gserviceaccount.com`).
3. Grant the minimum required permissions:

| Permission | Purpose | Required |
| --- | --- | --- |
| Release to testing tracks | `internal` | **Yes** (enough for the MVP) |
| Release to production, exclude devices, and use Play App Signing | `production`: `publish --status inProgress`, `rollout`, `promote --to production` | Yes, to manage production releases |
| Manage store presence | only if listings are added later | No |
| Admin / Owner | — | **No, never** |

4. Restrict access to specific apps (App permissions) if the account manages several apps.
5. No separate acceptance is needed for a service account — access is active immediately.

Check: `google-play-publisher doctor --package <applicationId>` must pass all checks.

## 4. Passing the JSON key to the application

The tool does not create, store or rotate keys — it only receives the **path to the service
account JSON file**. The key lifecycle (issuing, storage, rotation) is outside the
requirements.

There are two ways to pass it:

1. **ADC (default).** The `GOOGLE_APPLICATION_CREDENTIALS` environment variable contains the
   path:

   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json
   google-play-publisher publish -p com.example.app -f app-release.aab -t internal
   ```

2. **Explicit flag.** `--credentials /path/to/sa.json`; it takes precedence over ADC and is
   convenient for local runs.

The file requirements are minimal: it is a service account JSON key, the file exists and is
readable by the process. How the key got to the machine (CI secret, volume, local directory)
is the pipeline's responsibility, not the tool's.

Check: `google-play-publisher doctor --package <applicationId>` — the token exchange must
succeed.

## 5. AAB signing

Play accepts only a signed bundle. Signing happens before the tool is called.

### Gradle (the main path)

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

### bundletool (when the AAB is built manually)

```bash
java -jar bundletool.jar build-bundle --modules=base.zip --output=app.aab
jarsigner -keystore upload.jks -sigalg SHA256withRSA -digestalg SHA-256 app.aab upload
```

### Play App Signing

- The uploaded AAB is signed with the **upload key**; Google rebuilds APKs and signs them with
  the **app signing key**.
- Play App Signing is mandatory for new apps; the key is stored by Google.
- If the upload key is lost, request a reset in Play Console — the app is not lost.
- Sign of correct setup: `jarsigner -verify` passes on the local AAB and Play returns no
  signature errors during upload.

## 6. Managed publishing

**What it is.** A Play Console setting (Publishing overview) that separates "changes approved
by review" from "changes published". Normally Play publishes approved changes itself. With
managed publishing enabled, approved changes stay in "Changes ready to publish" until a human
clicks **Publish changes**. It is used to tie a release to a date or campaign, review the full
change list and release everything with one action.

**It does not apply to `internal`.** Play Console Help states: "Internal test tracks are not
covered by Managed publishing" — changes on the internal track usually become available
immediately, without review. For our scenario (always `internal`) managed publishing is not a
blocker and its state does not need to be checked.

Two exceptions still require review:

- the app's first rollout runs on `internal`;
- the previous submission was rejected — the next one is not available until approval.

**If `beta`/`production` are added later.** Managed publishing holds releases: after approval a
manual "Publish changes" is needed, and the API cannot do it. Check the mode in Play Console →
**Publishing overview** → "Managed publishing status". The API does not expose the flag, so the
tool reports `"managedPublishing": "unknown"` and does not pretend the publication is complete.

## 7. Production checklist

- [ ] `androidpublisher.googleapis.com` is enabled in the project.
- [ ] The key is passed to the tool: path in `GOOGLE_APPLICATION_CREDENTIALS` or
      `--credentials`.
- [ ] The service account is invited to Play Console with release-only permissions
      ("Release to testing tracks").
- [ ] `google-play-publisher doctor --package <id>` is green.
- [ ] The AAB is signed; `jarsigner -verify` succeeds.
- [ ] A test publication to `internal` passed and the version code is visible in
      `google-play-publisher status`.
- [ ] The CI pipeline has a concurrency group (one run per app).
- [ ] Managed publishing does not affect `internal`; if other tracks are planned, the mode has
      been checked.

## 8. Common setup errors

| Symptom | Cause | Fix |
| --- | --- | --- |
| `401 invalid_grant` / file not found | Wrong JSON key path or unreadable file | Pass the correct path in `GOOGLE_APPLICATION_CREDENTIALS` or `--credentials` |
| `403 The caller does not have permission` | Service account not invited or no app permissions | Invite + App permissions |
| `404 Application not found` | Wrong `applicationId` or no access to the app | Check the package and access |
| `403 API has not been used in project ...` | API not enabled | Enable the API |
| `400 ... cannot be sent for review automatically` | The app requires manual submission | Use `--changes-not-sent-for-review`, then submit from the console |
| Release "succeeded" but testers got nothing | Managed publishing (closed/open/production only) | Publish manually; irrelevant for `internal` |
| `403 Daily edit creation quota exceeded` | Daily edit quota exhausted | Wait until the next day; remove create/delete loops |
| Signature error | AAB signed with the wrong key / not signed | Rebuild with the upload key |
