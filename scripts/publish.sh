#!/usr/bin/env bash
# Publish a signed AAB through the google-play-publisher CLI.
#
# Used by action.yml. The script is deliberately thin: it prepares credentials,
# resolves paths and inputs, runs the CLI once with --json, exposes outputs and
# writes a Job Summary. All Play behavior stays in the CLI.
#
# Test hooks (not action inputs):
#   GPP_TEST_ONLY_BINARY   use this executable instead of downloading the CLI
set -euo pipefail

die() {
	printf 'google-play-publisher publish: %s\n' "$*" >&2
	exit 1
}

warn() {
	printf '::warning::google-play-publisher publish: %s\n' "$*" >&2
}

mask() {
	printf '::add-mask::%s\n' "$1"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

package="${GPP_PACKAGE:-}"
aab="${GPP_AAB:-}"
track="${GPP_TRACK:-internal}"
if_exists="${GPP_IF_EXISTS:-fail}"
timeout_value="${GPP_TIMEOUT:-}"
debug="${GPP_DEBUG:-false}"
metadata_dir="${GPP_METADATA_DIR:-}"
legacy_changelog_dir="${GPP_CHANGELOG_DIR:-}"
version_code="${GPP_VERSION_CODE:-}"
release_name="${GPP_RELEASE_NAME:-}"

[ -n "$package" ] || die "package input is required"
[ -n "$aab" ] || die "aab input is required"

version="${GPP_REQUESTED_VERSION:-}"
if [ -z "$version" ]; then
	ref="${GPP_ACTION_REF:-}"
	case "$ref" in
		v[0-9]*|[0-9]*) version="$ref" ;;
		*) die "version input is required when the action ref \"${ref}\" is not a release tag" ;;
	esac
fi

resolve_path() {
	case "$1" in
		/*) printf '%s' "$1" ;;
		*) printf '%s/%s' "${GITHUB_WORKSPACE:-$PWD}" "$1" ;;
	esac
}

tmp_root="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
# Every invocation owns its result file: a second publish action in the same
# job must not overwrite a result that downstream steps still reference.
result_dir="$(mktemp -d "${tmp_root%/}/google-play-publisher.XXXXXX")"
result_file="${result_dir}/result.json"

cred_path=""
cred_created=0
stderr_file=""

# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() {
	if [ "$cred_created" -eq 1 ] && [ -n "$cred_path" ]; then
		rm -f "$cred_path"
	fi
	if [ -n "$stderr_file" ]; then
		rm -f "$stderr_file"
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ -n "${GPP_TEST_ONLY_BINARY:-}" ]; then
	binary="$GPP_TEST_ONLY_BINARY"
	[ -x "$binary" ] || die "test binary ${binary} is not executable"
else
	install_dir="$(GITHUB_OUTPUT='' GPP_VERSION="$version" bash "${script_dir}/setup.sh")" ||
		die "failed to install google-play-publisher ${version}"
	binary="${install_dir}/google-play-publisher"
	export PATH="${install_dir}:${PATH}"
fi

base64_value="${GPP_CREDENTIALS_BASE64:-}"
credentials_file="${GPP_CREDENTIALS_FILE:-}"
if [ -n "$base64_value" ] && [ -n "$credentials_file" ]; then
	die "credentials-base64 and credentials-file are mutually exclusive"
fi

if [ -n "$base64_value" ]; then
	mask "$(printf '%s' "$base64_value" | tr -d '\r\n')"
	cred_path="${result_dir}/credentials-$$.json"
	cred_created=1
	if ! (umask 077; printf '%s' "$base64_value" | (base64 --decode 2>/dev/null || base64 -D) >"$cred_path"); then
		die "failed to decode credentials-base64"
	fi
	[ -s "$cred_path" ] || die "credentials-base64 is empty or invalid base64"
elif [ -n "$credentials_file" ]; then
	cred_path="$(resolve_path "$credentials_file")"
	[ -f "$cred_path" ] || die "credentials-file ${credentials_file} does not exist"
fi

if [ -n "$metadata_dir" ] && [ -n "$legacy_changelog_dir" ]; then
	die "metadata-dir and changelog-dir are mutually exclusive; changelog-dir is deprecated"
fi
if [ -z "$metadata_dir" ] && [ -n "$legacy_changelog_dir" ]; then
	warn "changelog-dir is deprecated; use metadata-dir"
	metadata_dir="$legacy_changelog_dir"
fi

args=(publish --json --package "$package" --aab "$(resolve_path "$aab")" --track "$track")
if [ -n "$metadata_dir" ]; then
	args+=(--metadata-dir "$(resolve_path "$metadata_dir")")
fi
if [ -n "$version_code" ]; then
	args+=(--version-code "$version_code")
fi
if [ -n "$release_name" ]; then
	args+=(--release-name "$release_name")
fi
if [ -n "$if_exists" ]; then
	args+=(--if-exists "$if_exists")
fi
if [ -n "$timeout_value" ]; then
	args+=(--timeout "$timeout_value")
fi
if [ "$debug" = "true" ]; then
	args+=(--debug)
fi
if [ -n "$cred_path" ]; then
	args+=(--credentials "$cred_path")
fi

stderr_file="$(mktemp "${tmp_root%/}/gpp-publish-stderr.XXXXXX")"
set +e
"$binary" "${args[@]}" >"$result_file" 2>"$stderr_file"
code=$?
set -e

cat "$stderr_file" >&2
rm -f "$stderr_file"
stderr_file=""

emit() {
	if [ -n "${GITHUB_OUTPUT:-}" ]; then
		printf '%s=%s\n' "$1" "$2" >>"$GITHUB_OUTPUT"
	fi
}

have_jq=0
if command -v jq >/dev/null 2>&1; then
	have_jq=1
else
	warn "jq is not installed: version-code, edit-id, review and error-code outputs are unavailable; the raw result file is kept"
fi

json_field() {
	if [ "$have_jq" -eq 1 ]; then
		jq -r "$1 // empty" "$result_file" 2>/dev/null || true
	fi
}

reported_version_code="$(json_field '.versionCode')"
edit_id="$(json_field '.editId')"
review="$(json_field '.review')"
error_code="$(json_field '.error.code')"

skipped="false"
if [ "$review" = "skipped" ]; then
	skipped="true"
fi

manual="false"
if [ "$code" -eq 8 ]; then
	manual="true"
fi

emit "version_code" "$reported_version_code"
emit "edit_id" "$edit_id"
emit "review" "$review"
emit "skipped" "$skipped"
emit "manual_action" "$manual"
emit "error_code" "$error_code"
emit "result_file" "$result_file"

summary="${GITHUB_STEP_SUMMARY:-}"
if [ -n "$summary" ]; then
	if ! {
		printf '### Google Play publish\n\n'
		printf '| Field | Value |\n'
		printf '| --- | --- |\n'
		printf '| Package | %s |\n' "$package"
		printf '| Track | %s |\n' "$track"
		printf '| Version code | %s |\n' "${reported_version_code:-unknown}"
		printf '| Edit | %s |\n' "${edit_id:-none}"
		printf '| Review | %s |\n' "${review:-unknown}"
		printf '| Exit code | %s |\n' "$code"
		if [ "$code" -eq 8 ]; then
			printf '\n> **Manual action required:** the release was saved without review. '
			printf 'Open Play Console -> Publishing overview and send the changes for review.\n'
		fi
	} >>"$summary"; then
		warn "failed to write the Job Summary"
	fi
fi

exit "$code"
