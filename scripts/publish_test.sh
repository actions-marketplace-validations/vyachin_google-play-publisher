#!/usr/bin/env bash
# shellcheck disable=SC2015 # ok()/not_ok() always return 0; the pattern is intentional
# Tests for scripts/publish.sh with a stub CLI. No Play credentials or network
# access are required.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PUBLISH="${ROOT}/scripts/publish.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/gpp-publish-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

passed=0
failed=0

ok() {
	passed=$((passed + 1))
	printf 'ok - %s\n' "$1"
}

not_ok() {
	failed=$((failed + 1))
	printf 'not ok - %s\n' "$1"
}

run_publish() {
	set +e
	env "$@" bash "$PUBLISH" >"${WORK}/out" 2>"${WORK}/err"
	status=$?
	set -e
}

expect_ok() {
	local label="$1"
	if [ "$status" -eq 0 ]; then
		ok "$label"
	else
		not_ok "$label (exit ${status}; stderr: $(tr '\n' ' ' <"${WORK}/err"))"
	fi
}

expect_status() {
	local want="$1" label="$2"
	if [ "$status" -eq "$want" ]; then
		ok "$label"
	else
		not_ok "$label (exit ${status}, want ${want}; stderr: $(tr '\n' ' ' <"${WORK}/err"))"
	fi
}

expect_err_contains() {
	local want="$1" label="$2"
	if grep -qF -- "$want" "${WORK}/err"; then
		ok "$label"
	else
		not_ok "$label (stderr: $(tr '\n' ' ' <"${WORK}/err"))"
	fi
}

output_value() {
	awk -F= -v key="$1" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "${WORK}/github_output"
}

mkdir -p "${WORK}/workspace/meta data" "${WORK}/workspace/secrets"

STUB="${WORK}/stub"
cat >"$STUB" <<'STUB_EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" >"${STUB_ARGS:?}"
prev=""
for arg in "$@"; do
	if [ "$prev" = "--credentials" ]; then
		printf '%s' "$arg" >"${STUB_CRED_PATH:?}"
		cp "$arg" "${STUB_CRED_COPY:?}"
		if stat -f %Lp "$arg" >/dev/null 2>&1; then
			stat -f %Lp "$arg" >"${STUB_CRED_MODE:?}"
		else
			stat -c %a "$arg" >"${STUB_CRED_MODE:?}"
		fi
	fi
	prev="$arg"
done
cat "${STUB_JSON:?}"
exit "${STUB_EXIT:-0}"
STUB_EOF
chmod 0755 "$STUB"

reset() {
	: >"${WORK}/github_output"
	: >"${WORK}/summary"
	: >"${WORK}/stub_args"
	rm -f "${WORK}/cred_path" "${WORK}/cred_copy" "${WORK}/cred_mode"
}

common_env() {
	COMMON=(
		GPP_REQUESTED_VERSION=0.0.1
		GPP_TEST_ONLY_BINARY="$STUB"
		GPP_PACKAGE=com.example.app
		GPP_AAB="app dir/app release.aab"
		GPP_TRACK=internal
		GITHUB_WORKSPACE="${WORK}/workspace"
		GITHUB_OUTPUT="${WORK}/github_output"
		GITHUB_STEP_SUMMARY="${WORK}/summary"
		STUB_ARGS="${WORK}/stub_args"
	)
}

success_json="${WORK}/success.json"
printf '%s\n' '{"ok":true,"command":"publish","package":"com.example.app","track":"internal","status":"completed","versionCode":42,"editId":"edit-1","review":"sent","managedPublishing":"unknown","durationMs":1}' >"$success_json"

common_env
reset
run_publish "${COMMON[@]}" \
	STUB_JSON="$success_json" \
	GPP_CHANGELOG_DIR="meta data" \
	GPP_VERSION_CODE=42 \
	GPP_RELEASE_NAME=1.0.42 \
	GPP_EXPECTED_TAG=v1.0.42 \
	GPP_IF_EXISTS=skip \
	GPP_TIMEOUT=5m \
	GPP_DEBUG=true
expect_ok "publishes with the documented inputs"
[ "$(output_value version_code)" = "42" ] && ok "exposes version-code" || not_ok "exposes version-code"
[ "$(output_value edit_id)" = "edit-1" ] && ok "exposes edit-id" || not_ok "exposes edit-id"
[ "$(output_value review)" = "sent" ] && ok "exposes review" || not_ok "exposes review"
[ "$(output_value skipped)" = "false" ] && ok "marks a normal publish as not skipped" || not_ok "marks a normal publish as not skipped"
[ "$(output_value manual_action)" = "false" ] && ok "marks a normal publish as not manual" || not_ok "marks a normal publish as not manual"
[ -f "$(output_value result_file)" ] && ok "writes the JSON result file" || not_ok "writes the JSON result file"
grep -qF -- "--json" "${WORK}/stub_args" && ok "runs the CLI with --json" || not_ok "runs the CLI with --json"
grep -qF -- "${WORK}/workspace/app dir/app release.aab" "${WORK}/stub_args" && ok "resolves the AAB path against GITHUB_WORKSPACE" || not_ok "resolves the AAB path against GITHUB_WORKSPACE"
grep -qF -- "${WORK}/workspace/meta data" "${WORK}/stub_args" && ok "resolves the changelog path against GITHUB_WORKSPACE" || not_ok "resolves the changelog path against GITHUB_WORKSPACE"
grep -qF -- "--debug" "${WORK}/stub_args" && ok "forwards debug" || not_ok "forwards debug"
grep -qF -- "v1.0.42" "${WORK}/stub_args" && ok "forwards the expected tag" || not_ok "forwards the expected tag"
grep -qF "Google Play publish" "${WORK}/summary" && ok "writes the Job Summary" || not_ok "writes the Job Summary"

# Each invocation keeps its own result file.
first_result="$(output_value result_file)"
first_dir="$(dirname "$first_result")"

# Idempotent skip: no edit id.
skip_json="${WORK}/skip.json"
printf '%s\n' '{"ok":true,"command":"publish","package":"com.example.app","track":"internal","status":"completed","versionCode":42,"review":"skipped","managedPublishing":"unknown","durationMs":1}' >"$skip_json"
reset
run_publish "${COMMON[@]}" STUB_JSON="$skip_json"
expect_ok "handles an idempotent skip"
[ "$(output_value skipped)" = "true" ] && ok "marks a skip in outputs" || not_ok "marks a skip in outputs"
[ -z "$(output_value edit_id)" ] && ok "leaves edit-id empty on a skip" || not_ok "leaves edit-id empty on a skip"

second_result="$(output_value result_file)"
second_dir="$(dirname "$second_result")"
[ "$first_result" != "$second_result" ] && ok "keeps a distinct result file per invocation" || not_ok "keeps a distinct result file per invocation"
[ -f "$first_result" ] && grep -qF '"review":"sent"' "$first_result" && ok "preserves the earlier result payload" || not_ok "preserves the earlier result payload"
[ -f "$second_result" ] && grep -qF '"review":"skipped"' "$second_result" && ok "writes the current result payload" || not_ok "writes the current result payload"
rm -rf "$first_dir" "$second_dir"

# Manual review action: exit code 8 must be preserved.
manual_json="${WORK}/manual.json"
printf '%s\n' '{"ok":true,"command":"publish","package":"com.example.app","track":"internal","status":"completed","versionCode":42,"editId":"edit-2","review":"notSent","managedPublishing":"unknown","durationMs":1}' >"$manual_json"
reset
run_publish "${COMMON[@]}" STUB_JSON="$manual_json" STUB_EXIT=8
expect_status 8 "preserves exit code 8"
[ "$(output_value manual_action)" = "true" ] && ok "marks a manual action" || not_ok "marks a manual action"
grep -qF "Manual action required" "${WORK}/summary" && ok "explains the manual action in the summary" || not_ok "explains the manual action in the summary"

# Ordinary failure: error JSON and exit code.
error_json="${WORK}/error.json"
printf '%s\n' '{"ok":false,"command":"publish","package":"com.example.app","error":{"code":"conflict","exitCode":5,"message":"version code 42 has already been used"}}' >"$error_json"
reset
run_publish "${COMMON[@]}" STUB_JSON="$error_json" STUB_EXIT=5
expect_status 5 "preserves a failure exit code"
[ "$(output_value error_code)" = "conflict" ] && ok "exposes the error code" || not_ok "exposes the error code"

# Base64 credentials: decoded, restricted and removed.
b64="$(printf '%s' '{"type":"service_account","private_key":"secret"}' | base64 | tr -d '\n')"
reset
run_publish "${COMMON[@]}" STUB_JSON="$success_json" \
	GPP_CREDENTIALS_BASE64="$b64" \
	STUB_CRED_PATH="${WORK}/cred_path" \
	STUB_CRED_COPY="${WORK}/cred_copy" \
	STUB_CRED_MODE="${WORK}/cred_mode"
expect_ok "publishes with base64 credentials"
[ -f "${WORK}/cred_copy" ] && ok "passes a credentials file to the CLI" || not_ok "passes a credentials file to the CLI"
[ "$(cat "${WORK}/cred_copy" 2>/dev/null)" = '{"type":"service_account","private_key":"secret"}' ] && ok "decodes credentials-base64" || not_ok "decodes credentials-base64"
[ "$(cat "${WORK}/cred_mode" 2>/dev/null)" = "600" ] && ok "creates the credentials file as 0600" || not_ok "creates the credentials file as 0600"
cred_file="$(cat "${WORK}/cred_path" 2>/dev/null || true)"
if [ -n "$cred_file" ] && [ -f "$cred_file" ]; then
	not_ok "removes the temporary credentials file"
else
	ok "removes the temporary credentials file"
fi
if grep -qF 'secret' "${WORK}/err" || grep -qF 'service_account' "${WORK}/err"; then not_ok "does not log credentials"; else ok "does not log credentials"; fi

# Caller-owned credentials file: used in place and never deleted.
printf '%s' '{"caller":true}' >"${WORK}/workspace/secrets/sa.json"
reset
run_publish "${COMMON[@]}" STUB_JSON="$success_json" \
	GPP_CREDENTIALS_FILE="secrets/sa.json" \
	STUB_CRED_PATH="${WORK}/cred_path" \
	STUB_CRED_COPY="${WORK}/cred_copy" \
	STUB_CRED_MODE="${WORK}/cred_mode"
expect_ok "publishes with a credentials file"
[ "$(cat "${WORK}/cred_copy" 2>/dev/null)" = '{"caller":true}' ] && ok "reads the caller credentials file" || not_ok "reads the caller credentials file"
[ -f "${WORK}/workspace/secrets/sa.json" ] && ok "keeps the caller credentials file" || not_ok "keeps the caller credentials file"

# Mutually exclusive credentials.
reset
run_publish "${COMMON[@]}" STUB_JSON="$success_json" \
	GPP_CREDENTIALS_BASE64="$b64" \
	GPP_CREDENTIALS_FILE="secrets/sa.json"
expect_status 1 "rejects both credential inputs"
expect_err_contains "mutually exclusive" "explains the credential conflict"

# Version resolution from the action ref.
reset
run_publish "${COMMON[@]}" STUB_JSON="$success_json" GPP_REQUESTED_VERSION= GPP_ACTION_REF=v0.0.1
expect_ok "resolves the CLI version from the action ref"
reset
run_publish "${COMMON[@]}" STUB_JSON="$success_json" GPP_REQUESTED_VERSION= GPP_ACTION_REF=main
expect_status 1 "rejects a branch action ref without a version"
expect_err_contains "version input is required" "asks for an explicit version"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
