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

1. transport (network, timeout, 5xx);
2. HTTP status;
3. `reason` and message text (for Play validation rejections).

Message text is only a fallback and is matched by stable fragments, because Play changes
wording.

## 2. Error catalog

| HTTP | Marker | Cause | Exit code | Retry | Action |
| --- | --- | --- | --- | --- | --- |
| — | `context.DeadlineExceeded`, `io.EOF`, `ECONNRESET` | Network/timeout | 6 | Yes | Rerun |
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
| Token retrieval | Yes, 2 attempts | Then code 3 |
| `edits.insert` | Yes, but ≤ 2 | A repeated insert invalidates the previous edit; allowed but consumes quota |
| `bundles.upload` | Yes, at chunk level | `ResumableMedia` + `ChunkRetryDeadline`; if the session dies, a new session in the same edit |
| `tracks.update` | Yes, 5xx/429 | Validation errors — no |
| `edits.validate` | Yes | For `--dry-run` |
| `edits.commit` | **Careful** | See §4: check the actual state first |
| `edits.delete` | 1 attempt, separate 15 s context | Cleanup must not mask the original error |

Backoff parameters: `delay = min(cap, base * 2^attempt) + rand(0..1s)`, `base=1s`, `cap=32s`,
at most 5 attempts (Google's recommendation for 5xx). For 429, prefer `Retry-After` when
present.

After exhaustion — code 6 and a message with the last error and the number of attempts.

## 4. Ambiguous commit

`edits.commit` may fail with a timeout even though the server already applied the changes. A
blind retry is dangerous: the edit may already be closed, and a repeated commit returns an
error that is easy to mistake for a failure.

Algorithm:

```
commit → error (timeout/5xx)?
  → request track releases (read-only)
  → versionCode present?
      yes → success (review: sent/unknown), code 0
      no  → retry commit once (if the error is not about an invalid edit)
              → error again → code 6, attach editId
```

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
  contents).
- For auditing, `edit_id` + `version_code` + time are enough: they identify the attempt.

## 7. What is not a tool error

| Event | Interpretation |
| --- | --- |
| Changes "processing" for several hours | Normal: Play applies changes asynchronously |
| Managed publishing waits for a manual step | Normal when enabled (except `internal`, where it does not apply); code 0 + warning |
| Review not finished | Outside the CLI's responsibility; monitor separately |
| `--if-exists=skip` and the version is already on the track | Idempotent success, code 0 |
