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
not retried. Token retrieval failures arrive as `*oauth2.RetrieveError`: a 4xx answer from
the token endpoint is an authentication problem (3), a network failure or 5xx answer is
transient (6).

## 2. Error catalog

| HTTP | Marker | Cause | Exit code | Retry | Action |
| --- | --- | --- | --- | --- | --- |
| — | `context.DeadlineExceeded`, `io.EOF`, `ECONNRESET`, `ECONNREFUSED`, `EHOSTUNREACH`, `ENETUNREACH` | Network/timeout | 6 | Yes | Rerun |
| — | `context.Canceled` | Interrupted by a signal | 130 | No | Rerun |
| — | `*oauth2.RetrieveError` with 4xx | Token endpoint rejected the credentials | 3 | No | Check the key, its scopes and the account |
| — | `*oauth2.RetrieveError` without a response, or 5xx | Token endpoint unreachable | 6 | Yes | Rerun |
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
| Token retrieval | No application retry | The auth library makes one attempt; a rejected credential is code 3, a network/5xx failure is code 6 |
| `edits.insert` | Yes, but ≤ 2 | A repeated insert invalidates the previous edit; allowed but consumes quota |
| `bundles.upload` | Yes, at chunk level | `ResumableMedia` + `ChunkRetryDeadline`; if the session dies, a new session in the same edit |
| `tracks.update` | Yes, ≤ 5 attempts, 5xx/429/network | Validation errors are not retried |
| `tracks.releases.list` | Yes, ≤ 5 attempts | Read-only; used by preflight, `status` and commit reconciliation |
| `edits.validate` | Yes, ≤ 5 attempts | For `--dry-run` |
| `edits.commit` | **Careful** | No blind retry: see §4, the actual state is checked first |
| `edits.delete` | 1 attempt, separate 15 s context | Cleanup must not mask the original error |

Backoff parameters: `delay = min(cap, base * 2^retry) + rand(0..1s)`, `base=1s`, `cap=32s`,
at most 5 attempts (Google's recommendation for 5xx). `Retry-After` is honored when present,
as seconds or as an HTTP date, and capped at 2 minutes. Retrying stops as soon as the context
is done; each retry is logged at `DEBUG` with the attempt number and delay.

After exhaustion — code 6 and a message with the last error and the number of attempts.

## 4. Ambiguous commit

`edits.commit` may fail with a timeout even though the server already applied the changes. A
blind retry is dangerous: the edit may already be closed, and a repeated commit returns an
error that is easy to mistake for a failure.

Algorithm:

```
commit → error?
  → "changes cannot be sent for review automatically"?
      → retry once with changesNotSentForReview=true
         success → saved; review=notSent; exit code 8
  → otherwise request track releases (read-only)
      versionCode present?
          yes → success; review comes from the options of the request
                actually sent; exit code 0
          no  → retry commit once if the error is transient
                (not for an invalid edit, not after a canceled context)
                  → verify again
                      versionCode present → success, code 0
                      still absent → code 6, attach editId
```

Every ambiguous attempt is reconciled before the tool decides that it failed: a retry that
the server applied but whose response was lost is recognized by the following read and the
edit is not deleted.

The reported `review` value always comes from the `commit` options that were actually used —
including the automatic `changesNotSentForReview=true` fallback and ambiguous attempts
resolved by verification. An automatic fallback that saved the changes returns code 8 even
when the retry itself succeeded; an explicit `--changes-not-sent-for-review` returns 0.

If the state check itself is unavailable (network), the tool returns code 6 and does **not**
claim that the publication failed: the message says "state unknown, check the track".

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
