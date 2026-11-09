# 05 — Errors, retries and exit codes

This document connects Play API responses to actions for humans and pipelines. The
implementation must follow the tables, not heuristics on message text.

## 1. Reading an API error

The Go client returns `*googleapi.Error`:

```go
var gerr *googleapi.Error
if errors.As(err, &gerr) {
    // gerr.Code    — HTTP status
    // gerr.Message — human-readable Play message
    // gerr.Errors  — []googleapi.ErrorItem{Reason, Message, Domain, Location}
    // gerr.Header  — headers (including Retry-After)
}
```

Classification uses three signals in priority order:

1. transport (network, timeout, 5xx) and token retrieval;
2. HTTP status;
3. `reason` and message text (for Play validation rejections).

Message text is only a fallback and is matched by stable fragments, because Play changes
wording.

Transport failures arrive as plain `net`/`url` errors, not `googleapi.Error`:
`context.DeadlineExceeded`, `io.EOF`, `io.ErrUnexpectedEOF`, `ECONNRESET`, `ECONNREFUSED`,
`EHOSTUNREACH`, `ENETUNREACH` and network timeouts map to exit code 6; `context.Canceled`
maps to 130. Permanent TLS/certificate failures and malformed URLs stay internal (1) and are
not retried. Token retrieval failures arrive as `*auth.Error`: a 4xx answer from the token
endpoint is an authentication problem (3), a network failure or 5xx answer is transient (6).
Token acquisition and blocking refreshes use the request context, so `--timeout` and
cancellation also interrupt a stalled token endpoint.

## 2. Error catalog

| HTTP | Marker | Cause | Exit code | Retry | Action |
| --- | --- | --- | --- | --- | --- |
| — | `context.DeadlineExceeded`, `io.EOF`, `ECONNRESET`, `ECONNREFUSED`, `EHOSTUNREACH`, `ENETUNREACH` | Network/timeout | 6 | Yes | Rerun |
| — | `context.Canceled` | Interrupted by a signal | 130 | No | Rerun |
| — | `*auth.Error` with 4xx | Token endpoint rejected the credentials | 3 | No | Check the key, its scopes and the account |
| — | `*auth.Error` without a response, or 5xx | Token endpoint unreachable | 6 | Yes | Rerun |
| — | uploaded `sha256` differs from the inspected file | The AAB changed between inspection and upload | 1 | No | Rebuild and rerun without touching the file |
| 500/502/503/504 | — | Play failure | 6 | Yes | Retry, then escalate |
| 429 | `rateLimitExceeded` | Per-minute limit exceeded | 6 | Yes, with `Retry-After` | Wait |
| 401 | `UNAUTHENTICATED` | Token not obtained/expired | 3 | No | Check credentials |
| 403 | `PERMISSION_DENIED`, "The caller does not have permission" | No Play Console permissions | 3 | No | Grant access to the service account |
| 403 | "API has not been used in project … or it is disabled" | API not enabled | 3 | No | Enable the API |
| 403 | `Daily edit creation quota exceeded.` | Daily edit quota | 6 | No | Wait until the next day; remove edit loops |
| 404 | `Application not found` | Wrong package or no access | 4 | No | Check `applicationId` and access |
| 404 | `Track not found` | Track does not exist | 4 | No | Create the track in the console / change the name |
| 400 | `badRequest` without a specific marker | Play validation | 7 | No | Read the message, fix it |
| 400 | "version code … has already been used" | Reused version code | 5 | No | Bump versionCode / `--if-exists=skip` |
| 400 | "App bundle … is not signed" / signature error | AAB not signed or wrong key | 7 | No | Rebuild with the upload key |
| 400 | "release notes … exceed" | More than 500 characters per locale | 7 | No | Shorten |
| 400 | "userFraction must be …" | Fraction outside `(0,1)` | 2 (local) / 7 | No | Fix the flags; local validation should prevent this |
| 400 | edit invalidated / `Cannot find edit` | The console or a parallel client broke the edit | 5 | No | Rerun; check the track |
| 400 | "Changes cannot be sent for review automatically…" | Manual submission required | 8 | **Once** with `changesNotSentForReview=true` | Submit from the console |
| 400 | "Changes are already in review" (with `ERROR_IF_IN_REVIEW`) | Someone else's review is running | 5 | No | Wait for the review or `--on-in-review=replace` |
| 409 | `conflict` | Concurrent change | 5 | No | Rerun |
| Other | — | Unexpected | 1 | No | Bug/escalation; attach `editId` and the log |

## 3. Retry policy

Applies to idempotent or safely repeatable steps.

| Step | Retry | Notes |
| --- | --- | --- |
| Token retrieval | No application retry | The auth library makes one attempt under the operation context; a rejected credential is code 3, a network/5xx failure is code 6 |
| `edits.insert` | Yes, but ≤ 2 | A repeated insert invalidates the previous edit; allowed but consumes quota |
| `bundles.upload` | Yes, at chunk level | `ResumableMedia` + `ChunkRetryDeadline`; if the session dies, a new session in the same edit |
| `tracks.update` | Yes, ≤ 5 attempts, 5xx/429/network | Validation errors are not retried |
| `tracks.releases.list` | Yes, ≤ 5 attempts | Read-only; used by preflight, `status` and commit reconciliation |
| `listings.list`, `images.list`, `testers.get`, `tracks.list` | Yes, ≤ 2 attempts | Read-only calls inside the `pull` edit |
| image CDN download (`pull`) | No application retry | The URL from `images.list`; 404 → 4, 429/5xx → 6, other HTTP status → 7 |
| `edits.validate` | Yes, ≤ 5 attempts | For `--dry-run` |
| `edits.commit` | **Careful** | No blind retry: see §4, the actual state is checked first |
| `edits.delete` | 1 attempt, separate 15 s context | Not attempted after a signal; must not mask an earlier error; successful dry-run validation requires successful deletion |

Backoff parameters: `delay = min(cap, base * 2^retry) + rand(0..1s)`, `base=1s`, `cap=32s`,
at most 5 attempts (Google's recommendation for 5xx). `Retry-After` is honored when present,
as seconds or as an HTTP date, and capped at 2 minutes. Retrying stops as soon as the context
is done; each retry is logged at `DEBUG` with the attempt number and delay.

After exhaustion — code 6 and a message with the last error and the number of attempts.

## 4. Ambiguous commit

`edits.commit` may fail with a timeout even though the server already applied the changes. A
blind retry is dangerous: the edit may already be closed, and a repeated commit returns an
error that is easy to mistake for a failure.

Only failures that may hide an applied edit are reconciled. The Play adapter marks transport
failures and 5xx answers (`play.AmbiguousFailure`). Token retrieval failures, Play validation
rejections and the other definitive outcomes are returned unchanged, so a `versionCode` that
appears on the track through another operation never turns a rejected commit into success.

Algorithm:

```
commit -> error?
  -> "changes cannot be sent for review automatically"?
      -> retry once with changesNotSentForReview=true
         success -> saved; review=notSent; exit code 8
  -> ambiguous failure (typed transport or 5xx)?
      no  -> return the error; delete the edit
      yes -> request track releases (read-only)
          versionCode present?
              yes -> read releaseLifecycleState:
                     NOT_SENT_FOR_REVIEW -> saved without review
                         -> review=notSent; exit code 8
                            (explicit --changes-not-sent-for-review -> 0,
                             --status draft -> 0)
                     otherwise -> success; review comes from the options of
                         the request actually sent; exit code 0
                     (an unset/UNSPECIFIED state keeps the historical
                      version-presence rule and logs a warning)
              no  -> retry commit once if the failure is transient
                     (not after a canceled context)
                       -> verify again
                           versionCode present -> success or code 8 as above
                           still absent -> delete the edit; return the error
```

Every ambiguous attempt is reconciled before the tool decides that it failed. The reported
`review` value comes from the `commit` options that were actually used plus the observed
lifecycle state:

- the automatic `changesNotSentForReview=true` fallback reports `notSent` and code 8;
- an explicit `--changes-not-sent-for-review` reports `notSent` and code 0;
- a release observed in `NOT_SENT_FOR_REVIEW` is never reported as `sent`; without the
  explicit save flag the command returns code 8 with the Play Console instruction.

Cleanup follows the verified outcome:

| Outcome | Meaning | Edit |
| --- | --- | --- |
| `committed` | Play confirmed the release | kept |
| `notCommitted` | The failure is definitive, or the version is absent after reconciliation | deleted best-effort |
| `unknown` | The track could not be read after an ambiguous failure | kept |

For `unknown` the tool exits with code 6 (130 when the run was interrupted) and reports the
package, track, version code and edit ID. It does not claim that the publication failed.

A signal keeps exit code 130: no further request is attempted — including the best-effort
edit deletion — the edit is kept and the process exits promptly. An edit that was already
deleted before the signal is not affected by these rules. An expired `--timeout` is not a
signal: the bounded cleanup still runs.

Cancellation returned by `edits.commit` is an ambiguous failure: the server may have received
the request before the response was interrupted. The adapter preserves code 130 and the
publisher reports the unknown commit state with the edit ID and a check-the-track hint.
The CLI preserves this diagnostic instead of replacing it with a generic interruption; every
other interruption keeps the generic `operation interrupted` message.

If the operation deadline expired before the reconciliation read, a separate bounded recovery
budget of 15 s is used, so a response lost at the deadline is still recognized. The budget is
shared by the recovery attempts. It is parented by the signal context, not by the expired
operation context: a signal interrupts an already running recovery and keeps exit code 130.

## 5. User-facing messages

Format: `<problem>: <cause>. <action>`.

Examples:

```
google-play-publisher: access denied: the service account has no permissions on
      com.example.app. Grant "Release to testing tracks" in Play Console → Users and
      permissions.
```

```
google-play-publisher: versionCode 42 already exists on track internal.
      Bump versionCode and rebuild the AAB, or use --if-exists=skip.
```

```
google-play-publisher: changes saved but not submitted for review.
      Open Play Console → Publishing overview and click "Send for review".
```

Requirements:

- no stack traces in normal mode (`--debug` may show them);
- the Play message is quoted verbatim (truncated to 4 KiB) but does not replace the explanation;
- the hint always contains a concrete action or a documentation link;
- in JSON — a stable `error.code` independent of Play wording.

## 6. Error logging

- Levels: `ERROR` — final failure; `WARN` — retry, warnings (including managed publishing);
  `INFO` — phases; `DEBUG` — details (enabled by `--debug`).
- In debug, an error logs: HTTP status, `reason`, Play response body (up to 4 KiB),
  correlation id and attempt number — enough to investigate without rerunning.
- Fields: `http_code`, `reason`, `attempt`, `phase`, `edit_id`, `version_code`.
- The Play response body is logged only after redaction (never `Authorization`, tokens or key
  contents); a body that cannot be redacted safely is not logged at all.
- For auditing, `edit_id` + `version_code` + time are enough: they identify the attempt.

## 7. What is not a tool error

| Event | Interpretation |
| --- | --- |
| Changes "processing" for several hours | Normal: Play applies changes asynchronously |
| Managed publishing waits for a manual step | Normal when enabled (except `internal`, where it does not apply); code 0 + warning |
| Review not finished | Outside the CLI's responsibility; monitor separately |
| `--if-exists=skip` and the version is already on the track | Idempotent success, code 0 |
