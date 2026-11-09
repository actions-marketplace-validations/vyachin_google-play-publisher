#!/usr/bin/env bash
# Install a released google-play-publisher binary for Linux.
#
# Used by setup/action.yml and by the publish action. The script accepts only
# released Linux amd64/arm64 archives, verifies the exact asset against
# checksums.txt, and exposes the install directory to the caller.
#
# Test hooks (not action inputs):
#   GPP_TEST_ONLY_OS, GPP_TEST_ONLY_ARCH  pretend to run on another platform
#   GPP_INSTALL_DIR                      override the install directory
#   GPP_RELEASES_BASE_URL                override the release download base URL
set -euo pipefail

readonly DEFAULT_REPOSITORY="vyachin/google-play-publisher"

die() {
	printf 'google-play-publisher setup: %s\n' "$*" >&2
	exit 1
}

log() {
	printf 'google-play-publisher setup: %s\n' "$*" >&2
}

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		die "no SHA-256 tool found (need sha256sum, shasum or openssl)"
	fi
}

raw_version="${GPP_VERSION:-}"
[ -n "$raw_version" ] || die "version input is required (X.Y.Z or vX.Y.Z)"

version="${raw_version#v}"
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
	die "invalid version \"$raw_version\": expected X.Y.Z or vX.Y.Z"
fi

os="${GPP_TEST_ONLY_OS:-$(uname -s)}"
arch="${GPP_TEST_ONLY_ARCH:-$(uname -m)}"

case "$os" in
	Linux) ;;
	*) die "unsupported operating system \"$os\": the action supports Linux only" ;;
esac

case "$arch" in
	x86_64|amd64) arch="amd64" ;;
	aarch64|arm64) arch="arm64" ;;
	*) die "unsupported architecture \"$arch\": supported are x86_64 and aarch64" ;;
esac

asset="google-play-publisher_${version}_linux_${arch}.tar.gz"
repository="${GPP_REPOSITORY:-$DEFAULT_REPOSITORY}"
base_url="${GPP_RELEASES_BASE_URL:-https://github.com/${repository}/releases/download}"
base_url="${base_url%/}/v${version}"

tmp_root="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
install_dir="${GPP_INSTALL_DIR:-${tmp_root}/google-play-publisher/${version}}"

work_dir="$(mktemp -d "${tmp_root%/}/gpp-setup.XXXXXX")"
cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

mkdir -p "$install_dir"

log "installing google-play-publisher ${version} for linux/${arch}"

download() {
	curl --fail --silent --show-error --location --retry 3 --retry-delay 1 \
		--output "$2" "$1" || die "failed to download $1"
}

download "${base_url}/${asset}" "${work_dir}/${asset}"
download "${base_url}/checksums.txt" "${work_dir}/checksums.txt"

expected="$(awk -v name="$asset" '$2 == name { print $1; exit }' "${work_dir}/checksums.txt")"
[ -n "$expected" ] || die "checksums.txt does not contain ${asset}"
case "$expected" in
	*[!0-9a-fA-F]*) die "invalid checksum for ${asset} in checksums.txt" ;;
esac
[ "${#expected}" -eq 64 ] || die "invalid checksum for ${asset} in checksums.txt"

actual="$(sha256_file "${work_dir}/${asset}")"
if [ "$actual" != "$expected" ]; then
	die "sha256 mismatch for ${asset}: got ${actual}, want ${expected}"
fi

tar -xzf "${work_dir}/${asset}" -C "$install_dir"
binary="${install_dir}/google-play-publisher"
[ -f "$binary" ] || die "archive does not contain google-play-publisher"
chmod 0755 "$binary"

if [ -n "${GITHUB_PATH:-}" ]; then
	printf '%s\n' "$install_dir" >>"$GITHUB_PATH"
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
	{
		printf 'version=%s\n' "$version"
		printf 'path=%s\n' "$install_dir"
	} >>"$GITHUB_OUTPUT"
fi

printf '%s\n' "$install_dir"
