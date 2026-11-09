# ADR-0004 — GitHub Actions packaging and AAB release identity

- **Status:** accepted
- **Date:** 2026-11-09
- **Stage:** S10.0

## Context

v0.0.3 ships one Go binary that publishes a signed AAB to the Play `internal` track. Consuming
Android repositories still install it with ad-hoc curl/checksum/tar scripts, decode the Play
service-account key themselves, and parse `build.gradle.kts` for the version code because the
CLI requires `--version-code` to select changelog files. S10 replaces those steps with two
GitHub Actions and moves release identity from Gradle sources into the CLI, where it is read
from the signed AAB.

Constraints:

- the actions must work from this repository without a Node build step;
- the CLI remains the only component that talks to Play; the actions are thin wrappers;
- tests must not require Play credentials; live verification stays blocked by S0;
- supported action targets are Linux amd64 and arm64;
- an AAB stores `base/manifest/AndroidManifest.xml` as protobuf XML (`aapt.pb.XmlNode`), not
  as binary AXML, so a Go parser is required;
- `--json` is already a stable contract, but it does not expose the AAB version name.

## Considered options

For packaging:

1. **Composite actions with bash entry points** (`setup/action.yml`, root `action.yml`,
   shared scripts). No build step, the CLI stays authoritative.
2. **A JavaScript action** (`@actions/core`, `@actions/exec`) with a bundled `dist/`. Gives a
   real `post` hook and typed helpers, but adds Node, a build pipeline and checked-in bundles.
3. **A Docker action.** Linux-only already, but slow startup and awkward workspace mounts.

For AAB identity:

1. **Parse the protobuf manifest in Go** with a small decoder for `aapt.pb.XmlNode`,
   `XmlElement`, `XmlAttribute` and `Item`.
2. **Vendor `Resources.proto` and generate Go code** with `protoc`/`buf`. Correct schema, but
   adds a code-generation toolchain and a generated file to review.
3. **Shell out to `bundletool dump manifest`.** Adds a large Java dependency to every runner.

## Decision

### D1 — Composite actions with shell entry points

Use option 1. Logic lives in `scripts/` so it can be tested with a stub CLI. Composite actions
have no `pre`/`post` steps (confirmed by the GitHub community discussion and the action
metadata schema), so the publish action runs as **one** `run` step with `trap ... EXIT` for
credential cleanup, and documents the `$RUNNER_TEMP` lifecycle. Revisit a JavaScript action
only if cleanup on job cancellation or shared cross-platform logic becomes a requirement.

### D2 — Version and ref policy

- The setup action's `version` input is required. Accept `X.Y.Z` and `vX.Y.Z`, including a
  prerelease suffix; reject ranges, branches and `latest`.
- The publish action's `version` input is optional. When omitted, use `github.action_ref` if it
  is a release tag; otherwise fail with an actionable message.
- Strip a leading `v` for the asset name (`google-play-publisher_0.0.4_linux_amd64.tar.gz`)
  but use `v<version>` for the release URL.
- Publish the action at every release tag. Do not create a floating major tag before a
  compatibility policy exists, and do not advertise `@v1` in examples.
- Documentation pins a release tag and recommends a commit SHA for stricter supply-chain
  control.

### D3 — AAB identity source and parser

Use option 1: a hand-written decoder built on `google.golang.org/protobuf/encoding/protowire`
(the protobuf module is already in the dependency graph). The schema is small and stable; the
field numbers are taken from the AOSP `aapt.pb` schema and documented next to the decoder.
Vendoring the proto and generating code is rejected because the repository has no codegen
pipeline and the generated file would be larger than the parser.

Rules:

- read exactly `base/manifest/AndroidManifest.xml`; reject a missing or unreadable entry with
  exit code 2;
- bound the decompressed manifest at 1 MiB and reject larger entries;
- extract `package` from the root `<manifest>` element and `android:versionCode` /
  `android:versionName` from its attributes;
- accept literal primitive/string values; reject resource references (`@string/...`) for
  values that must be resolved with an actionable hint;
- reject a non-zero `android:versionCodeMajor` with exit code 2: the tool does not model the
  combined long version code, and guessing it would risk publishing the wrong identity;
- commit fixtures produced by a real `aapt2` plus hand-built malformed protobuf fixtures.

### D4 — Identity precedence and compatibility

- The AAB is the source of truth. Explicit `--package` and `--version-code` are assertions:
  when supplied, they must equal the AAB values, otherwise exit code 2 before API construction.
- `--metadata` stays supported as a legacy cross-check: its package and version code must
  agree with the AAB; a mismatch is a local input error.
- `--changelog-dir` no longer requires `--version-code`; the AAB version code selects the
  files. When `--version-code` is supplied it is still verified against both the AAB and the
  uploaded bundle.
- `--release-name` keeps its empty default. Play generates a release name from the AAB version
  name when the field is empty, so the tool does not duplicate that policy; the resolved
  version name is exposed in logs and JSON for traceability.
- Add `schemaVersion: 1` and `versionName` to the publish JSON result, as required by the
  contract's schema-versioning rule.
- `--expected-tag` compares exactly with `v<versionName>`; a missing version name or a
  mismatch is exit code 2 before any API call.

> Superseded 2026-11-09: the owner removed `--expected-tag` and the `expected-tag` action
> input. A release tag is a CI concern, not a property of the AAB. The AAB `versionName` is
> still reported in logs and the JSON result.

### D5 — Artifact identity

`Prepare` opens the AAB once, computes size and SHA-256 from the open file, parses the
manifest from the same file, and stores the `*os.File` in the opaque `app.Prepared`. The upload
seeks to the start and reads from that handle, so replacing the path during a run cannot change
the bytes Play receives. `Prepared` owns the handle and closes it on every path, including
`--dry-run` and errors. `artifact.Inspect(path)` remains for read-only callers such as
`doctor`.

### D6 — Credentials lifecycle

- `credentials-base64` and `credentials-file` are mutually exclusive; when neither is set the
  CLI uses ADC.
- A base64 value is decoded into a `umask 077` file under `$RUNNER_TEMP`; the raw and encoded
  values are masked with `::add-mask::`, contents are never echoed, and the file is removed by
  the step's `trap` on success, failure and termination.
- A caller-owned `credentials-file` is read in place and never deleted.
- Hosted runners destroy `$RUNNER_TEMP` after the job; self-hosted runners may not, so the
  documentation recommends ephemeral runners or workable secret stores for them.

### D7 — Outputs and exit codes

- Always write the CLI JSON result to a file and expose it as `result-file`; expose
  `version-code`, `edit-id`, `review`, `skipped`, `manual-action` and `error-code` when the
  JSON allows it.
- Preserve the CLI exit status, including 8. Exit code 8 still writes the success object and
  the manual-submission instruction goes to `stderr` and the Job Summary.
- `jq` parses the result on GitHub-hosted Linux runners. If it is absent, the action keeps the
  result file, sets no derived outputs, and emits a warning instead of failing a publication
  that already succeeded.
- A Job Summary write failure is logged as a warning and does not change the exit code.

### D8 — Platform scope

Support Linux amd64 and arm64 on `ubuntu-latest` and `ubuntu-24.04-arm`. Reject any other
`uname -s`/`uname -m` combination with an explicit message. macOS and Windows binaries remain
published, but action support for them is a separate follow-up because install and shell
semantics differ.

## Consequences

**Pros**

- the actions add no runtime beyond bash, `curl`, `tar`, `sha256sum` and optional `jq`;
- publication behavior stays in the CLI and is covered by the existing exit-code and JSON
  contracts;
- AAB identity removes Gradle parsing, fixes idempotent skip without a metadata sidecar, and
  makes `--changelog-dir` work with one less flag;
- tests for the scripts, parser and CLI run without Play credentials.

**Cons**

- composite actions cannot clean up after a cancelled job on self-hosted runners;
- a hand-written protobuf decoder must be maintained against the AOSP schema;
- AAB identity rejects `versionCodeMajor` and resource-referenced values, which a small number
  of projects may use;
- the first release with these changes needs a CHANGELOG entry for the optional
  `--version-code` and the added JSON fields.

**Follow-ups**

- action smoke tests on both supported runner architectures (S10.5);
- release of the CLI and actions, then the Gunclub workflow switch (S10.6);
- live publication through the action when S0 access exists (I3).

## References

- [GitHub composite actions and missing post steps](https://github.com/orgs/community/discussions/26743)
- [AOSP `Resources.proto` (`aapt.pb.XmlNode`)](https://android.googlesource.com/platform/frameworks/base/+/refs/heads/main/tools/aapt2/Resources.proto)
- [Arm64 hosted runners for public repositories](https://github.blog/changelog/2025-08-07-arm64-hosted-runners-for-public-repositories-are-now-generally-available/)
- [CLI contract](../03-cli-contract.md)
- [Architecture](../02-architecture.md)
