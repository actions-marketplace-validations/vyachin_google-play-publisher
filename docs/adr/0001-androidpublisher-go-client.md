# ADR-0001 — Go client `google.golang.org/api/androidpublisher/v3` and the Edits API

- **Status:** proposed (design)
- **Date:** 2026-11-09
- **Context:** we need a Go CLI that publishes a signed AAB to Google Play.

## Context

We need to: upload an AAB, declare a release on a track, submit changes for review, and
return a machine-readable result. All of this is done by the Google Play Developer Publishing
API v3 through its transactional edits model. We need to choose how to talk to the API and
assess the risks.

Constraints:

- the tool must be a single static binary without a Ruby/JVM runtime;
- resumable uploads of large files are required;
- the new commit parameters (`changesInReviewBehavior`, `changesNotSentForReview`) are
  required;
- publishing is rare (a few times a day); performance is not critical, reliability and
  predictable errors are.

## Considered options

1. **Official Go client** `google.golang.org/api/androidpublisher/v3` (module
   `google.golang.org/api`). Generated from the discovery document, covers `edits`, `bundles`,
   `tracks`, `commit` with the required parameters, supports `Media`/resumable uploads and
   chunk retry deadlines.
2. **Hand-written REST client** on `net/http` + `golang.org/x/oauth2`. Full control and
   immediate access to any new field, but we would implement resumable upload (including
   308/404/410), pagination, `googleapi.Error` parsing and API changes ourselves.
3. **fastlane `supply`** (Ruby) or **Gradle Play Publisher** (JVM). Mature, but they add a
   foreign runtime and configuration layer; GPP is tied to a Gradle build.
4. **Unofficial Go wrappers.** Less code, but no maintenance or security guarantees.

## Decision

Use **option 1**: the official Go client and the Edits API, behind a thin `internal/play`
wrapper (an `API` interface plus an implementation) so that:

- Google client types are isolated from the rest of the code;
- the implementation can be replaced in tests (`httptest` + `option.WithEndpoint`);
- if needed, the client can be swapped for hand-written REST without rewriting the CLI or the
  domain.

The `google.golang.org/api` version is pinned in `go.mod` and updated deliberately, with
contract tests.

## Consequences

**Pros**

- minimal code: OAuth, retry transport, resumable upload and types out of the box;
- coverage of every required method, including the new commit parameters (verified on
  `v0.311.0`);
- testability by replacing the endpoint and the token source.

**Cons and risks**

- the package is `v0`, so structs may change between releases → pin the version and run
  contract tests;
- Google lists only Java and Python in
  [Client Libraries](https://developers.google.com/android-publisher/libraries), so the Go path
  is not "first class" → if the client lags, a spot call via `svc.BasePath`/`net/http` for one
  field is acceptable;
- the generator sometimes lags with new fields → check that the needed options exist before a
  release.

**What this changes in the code**

- `internal/play` is the only place that imports `androidpublisher`;
- the CLI depends on the interface, not on `*androidpublisher.Service`;
- tests never hit real Play, except the separate integration suite.

## When to revisit

- the client stops being generated or breaks compatibility without a way to pin a version;
- a field or method is missing from the client and is needed now (then use a local REST call);
- Google publishes an official Go client for the Play API.
