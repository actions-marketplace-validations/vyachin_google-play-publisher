# ADR-0002 — Stateless model, one edit per run, idempotency by versionCode

- **Status:** proposed (design)
- **Date:** 2026-11-09
- **Context:** the tool publishes from CI; network drops, timeouts and reruns are possible.

## Context

Publishing consists of several network steps, and Play does not provide "transactions that can
be resumed after a process restart". At the same time:

- one user can have only one active edit per app;
- any new edit invalidates the previous edit of the same user;
- changes in Play Console invalidate active edits;
- `commit` submits changes for review, and it can only be undone by a new commit;
- `versionCode` is unique and is not reused for new releases.

We need to decide whether to keep local state (a journal, "unfinished" publications, resume)
and how to make reruns idempotent.

## Considered options

1. **Stateless, one edit per run, idempotency by versionCode.** No local state; each run creates
   a new edit, uploads the AAB, declares the release and commits. A rerun after success is
   detected by the version code already present on the track.
2. **A local transaction journal** (sqlite/JSON): store `editId`, stage, SHA256 and resume from
   the failure point. Gives "resume", but adds state, migrations, locking and divergence from
   Play reality.
3. **An idempotency key on the Play side** (for example `requestId`). The API has no such
   mechanism.

## Decision

Option 1.

Rules:

- each run = a new edit; `editId` lives only in process memory;
- the step order is fixed: `insert → upload → tracks.update → commit`;
- on an error before `commit` — best-effort `edits.delete` (does not mask the original error);
- a rerun with the same version code:
  - `--if-exists=fail` (default) — code 5 with an instruction to bump the version code;
  - `--if-exists=skip` — success (idempotent no-op) if the version code is already on the
    target track;
- after an ambiguous `commit` error — check the actual state through the read-only release
  list, and only then retry (see [05-errors.md](../05-errors.md#4-ambiguous-commit));
- parallel runs are forbidden at the pipeline level (concurrency group), not inside the tool.

## Consequences

**Pros**

- no state that can go stale or diverge from Play;
- the tool runs on any runner and is not tied to a machine;
- failure behavior is predictable and explainable;
- less code and a smaller error surface.

**Cons**

- no "resume" after a process crash: republishing the same AAB may require a new version code;
- re-uploading a large AAB after a late failure costs traffic (partially mitigated by
  resumable upload within the process);
- idempotency is limited: it guarantees "do not publish the same release twice", not "continue
  exactly from the same step".

**Mitigations**

- a preflight check of the version code on the track before creating an edit;
- one edit per run (saves the daily quota);
- on a `commit` error — verify the state instead of a blind retry;
- logs and JSON carry `editId`, `versionCode`, `sha256` for manual investigation.

## When to revisit

- long multi-step publications appear (multiple tracks, metadata, products in one run);
- Play adds idempotency keys or an edit-continuation API;
- re-uploading becomes unacceptable (very large AABs, slow runners).
