# google-play-publisher — publishing signed AABs to Google Play

**Status:** implemented as v0.0.7. The executable is `google-play-publisher`.

## Short answer

**Yes, it is possible.** Publishing a signed AAB to Google Play is fully automated through the
[Google Play Developer Publishing API v3](https://developers.google.com/android-publisher)
(`androidpublisher.googleapis.com`). The whole flow is one transactional edit:

```
edits.insert → bundles.upload → tracks.update → edits.commit
```

For Go there is a generated client,
[`google.golang.org/api/androidpublisher/v3`](https://pkg.go.dev/google.golang.org/api/androidpublisher/v3)
(`v0.311.0` at design time). It covers every call we need, including resumable uploads,
`changesNotSentForReview` and `changesInReviewBehavior`.

## What the API cannot do

These are platform limits, not gaps in the tool. Accept them before building further.

| Limit | Consequence for the tool |
| --- | --- |
| The app must already exist in Play Console and have at least one uploaded build | A brand-new app is published by a human through Play Console ([Edits](https://developers.google.com/android-publisher/edits)) |
| The API cannot create an app, unpublish it, or accept legal declarations | Those actions stay in Play Console |
| Managed publishing holds releases on closed/open testing and production | `internal` is not covered; other tracks may need a manual "Publish changes" |
| Using Play Console while an edit is active invalidates the edit | The tool fails fast and asks for a rerun; concurrent changes are never merged |
| One active edit per user per app | Two CI runs at the same time are forbidden; use a pipeline concurrency lock |
| Google Play Developer API terms | The tool must not be offered as a publishing service to third parties ([API Usage Instructions](https://developers.google.com/android-publisher/api_usage)) |
| Signing | The tool **does not sign** AABs; it publishes an already signed artifact (upload key / Play App Signing) |

## What the tool does

1. Validates the artifact and access (`doctor`).
2. Uploads a signed AAB to the `internal` track.
3. Sets the release status: `completed` by default, `draft`, or a staged `inProgress` rollout
   with a user fraction.
4. Attaches localized release notes, an in-app update priority and, for production rollouts,
   country targeting.
5. Commits the edit and returns a machine-readable result (version code, sha256, status,
   edit id).
6. Changes an existing rollout without a new upload (`rollout`: fraction, halt, complete) and
   promotes a built version from one track to another (`promote`).
7. Handles state conflicts safely (invalidated edit, reused version code) and does not wait
   for review: `internal` builds are usually available immediately.
8. Supports `--debug`: detailed logs of every phase and HTTP exchange on `stderr`, with
   secrets redacted.

## Non-goals of the MVP

- store listing management (icons, screenshots, descriptions);
- tester groups and tester lists;
- managed publishing ("Publish changes") and the first app publication;
- signing and building AABs (that is Gradle/bundletool);
- creating an app and its first publication;
- APK support (AAB only);
- a local state database or "smart" migrations — the tool is stateless
  (see [ADR-0002](adr/0002-stateless-single-edit.md)).

## Key decisions

| Decision | Document |
| --- | --- |
| Official Go client + Edits API instead of hand-written REST or fastlane | [ADR-0001](adr/0001-androidpublisher-go-client.md) |
| Stateless model, one edit per run, idempotency by version code | [ADR-0002](adr/0002-stateless-single-edit.md) |
| Never cancel changes in review by default (`ERROR_IF_IN_REVIEW`) | [Architecture](02-architecture.md#6-review-policy) |
| Cobra with layered `cli → app → play` structure | [Stack](06-stack.md), [ADR-0003](adr/0003-cobra-command-layout.md) |
| GitHub Actions packaging, version pinning and AAB identity | [ADR-0004](adr/0004-github-actions.md) |
| CLI contract, exit codes, JSON for CI | [CLI](03-cli-contract.md) |
| Play Console, GCP, key passing and signing setup | [Setup](04-play-console-setup.md) |
| Play error catalog and retry policy | [Errors](05-errors.md) |

## Documents

- [01 — API capabilities and limits](01-api-feasibility.md)
- [02 — Architecture](02-architecture.md)
- [03 — CLI contract](03-cli-contract.md)
- [04 — Play Console and access setup](04-play-console-setup.md)
- [05 — Errors and retries](05-errors.md)
- [06 — Stack and project structure](06-stack.md)
- [07 — GitHub Actions](07-github-actions.md)
- [ADR-0001 — Go client and Edits API](adr/0001-androidpublisher-go-client.md)
- [ADR-0002 — Statelessness and one edit](adr/0002-stateless-single-edit.md)
- [ADR-0003 — Cobra and layered commands](adr/0003-cobra-command-layout.md)
- [ADR-0004 — GitHub Actions packaging and AAB identity](adr/0004-github-actions.md)
- [Development plan](../TODO.md)

External sources the project relies on:

- [Google Play Developer API — Edits](https://developers.google.com/android-publisher/edits)
- [Google Play Developer API — APKs and Tracks](https://developers.google.com/android-publisher/tracks)
- [Google Play Developer API — Uploading Files](https://developers.google.com/android-publisher/upload)
- [Google Play Developer API — Quotas](https://developers.google.com/android-publisher/quotas)
- [Concurrency considerations](https://developers.google.com/android-publisher/concurrency-considerations)
- [API Usage Instructions](https://developers.google.com/android-publisher/api_usage)
- [Play Console Help — app size limits](https://support.google.com/googleplay/android-developer/answer/9859372)

## Remaining questions

1. **Release notes format.** Resolved in v0.0.2: notes come only from `--changelog-dir` with
   the Play/fastlane layout `<locale>/changelogs/<versionCode>.txt`; the JSON variant was
   removed as a breaking CLI change.
2. **Do we need `promote`/`rollout`** in the next iteration, or is `publish` to `internal`
   enough?
3. **Other tracks.** Managed publishing does not matter for `internal`; if `beta`/`production`
   are added, decide how to publish after review (manual "Publish changes" or managed
   publishing disabled).
