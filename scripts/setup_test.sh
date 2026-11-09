#!/usr/bin/env bash
# Tests for scripts/setup.sh. They use a local file:// release fixture so no
# network access and no published release are required.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SETUP="${ROOT}/scripts/setup.sh"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/gpp-setup-test.XXXXXX")"
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

hash_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

build_release() {
	local version="$1" arch="${2:-amd64}"
	local dir="${WORK}/releases/v${version}"
	local asset="google-play-publisher_${version}_linux_${arch}.tar.gz"
	local build="${WORK}/build"

	mkdir -p "$dir" "$build"
	printf '#!/usr/bin/env bash\necho "fake google-play-publisher %s"\n' "$version" >"${build}/google-play-publisher"
	chmod 0755 "${build}/google-play-publisher"
	tar -czf "${dir}/${asset}" -C "$build" google-play-publisher
	printf '%s  %s\n' "$(hash_file "${dir}/${asset}")" "$asset" >"${dir}/checksums.txt"
}

# run_setup ENV... — runs the installer and stores exit status, stdout, stderr.
run_setup() {
	set +e
	env "$@" bash "$SETUP" >"${WORK}/out" 2>"${WORK}/err"
	status=$?
	set -e
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

expect_ok() {
	local label="$1"
	if [ "$status" -eq 0 ]; then
		ok "$label"
	else
		not_ok "$label (exit ${status}; stderr: $(tr '\n' ' ' <"${WORK}/err"))"
	fi
}

build_release "0.0.1"

# Successful install, version normalization and GITHUB_PATH/GITHUB_OUTPUT.
install_dir="${WORK}/install dir/0.0.1"
: >"${WORK}/github_path"
: >"${WORK}/github_output"
run_setup \
	GPP_VERSION=v0.0.1 \
	GPP_TEST_ONLY_OS=Linux \
	GPP_TEST_ONLY_ARCH=x86_64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/releases" \
	GPP_INSTALL_DIR="$install_dir" \
	GITHUB_PATH="${WORK}/github_path" \
	GITHUB_OUTPUT="${WORK}/github_output"
expect_ok "installs a released archive"
if [ -x "${install_dir}/google-play-publisher" ]; then ok "binary is executable"; else not_ok "binary is executable"; fi
if [ "$(cat "${WORK}/out")" = "$install_dir" ]; then ok "prints the install directory"; else not_ok "prints the install directory"; fi
if grep -qxF "$install_dir" "${WORK}/github_path"; then ok "appends the directory to GITHUB_PATH"; else not_ok "appends the directory to GITHUB_PATH"; fi
if grep -qxF "version=0.0.1" "${WORK}/github_output"; then ok "writes the normalized version output"; else not_ok "writes the normalized version output"; fi
if grep -qxF "path=${install_dir}" "${WORK}/github_output"; then ok "writes the path output"; else not_ok "writes the path output"; fi

# arm64 archive selection.
build_release "0.0.1" "arm64"
run_setup \
	GPP_VERSION=0.0.1 \
	GPP_TEST_ONLY_OS=Linux \
	GPP_TEST_ONLY_ARCH=aarch64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/releases" \
	GPP_INSTALL_DIR="${WORK}/install-arm64"
expect_ok "selects the arm64 archive for aarch64"

# Version validation.
for bad in latest 1.2 v1 "1.2.3/../../x" ""; do
	run_setup GPP_VERSION="$bad" GPP_TEST_ONLY_OS=Linux GPP_TEST_ONLY_ARCH=x86_64 \
		GPP_RELEASES_BASE_URL="file://${WORK}/releases" GPP_INSTALL_DIR="${WORK}/install-bad"
	expect_status 1 "rejects version \"$bad\""
done

# Platform validation.
run_setup GPP_VERSION=0.0.1 GPP_TEST_ONLY_OS=Darwin GPP_TEST_ONLY_ARCH=x86_64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/releases" GPP_INSTALL_DIR="${WORK}/install-darwin"
expect_status 1 "rejects a non-Linux platform"
expect_err_contains "unsupported operating system" "explains the unsupported platform"

run_setup GPP_VERSION=0.0.1 GPP_TEST_ONLY_OS=Linux GPP_TEST_ONLY_ARCH=riscv64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/releases" GPP_INSTALL_DIR="${WORK}/install-riscv"
expect_status 1 "rejects an unsupported architecture"
expect_err_contains "unsupported architecture" "explains the unsupported architecture"

# Missing release and checksum file.
run_setup GPP_VERSION=9.9.9 GPP_TEST_ONLY_OS=Linux GPP_TEST_ONLY_ARCH=x86_64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/releases" GPP_INSTALL_DIR="${WORK}/install-missing"
expect_status 1 "fails for a missing release"
expect_err_contains "failed to download" "reports the download failure"

mkdir -p "${WORK}/nochecksums/v0.0.1"
cp "${WORK}/releases/v0.0.1/google-play-publisher_0.0.1_linux_amd64.tar.gz" \
	"${WORK}/nochecksums/v0.0.1/"
run_setup GPP_VERSION=0.0.1 GPP_TEST_ONLY_OS=Linux GPP_TEST_ONLY_ARCH=x86_64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/nochecksums" GPP_INSTALL_DIR="${WORK}/install-nochecksums"
expect_status 1 "fails when checksums.txt is missing"
expect_err_contains "checksums.txt" "names the missing checksums file"

# Checksum mismatch.
mkdir -p "${WORK}/badsum/v0.0.1"
cp "${WORK}/releases/v0.0.1/google-play-publisher_0.0.1_linux_amd64.tar.gz" \
	"${WORK}/badsum/v0.0.1/"
printf '%064d  %s\n' 0 "google-play-publisher_0.0.1_linux_amd64.tar.gz" >"${WORK}/badsum/v0.0.1/checksums.txt"
run_setup GPP_VERSION=0.0.1 GPP_TEST_ONLY_OS=Linux GPP_TEST_ONLY_ARCH=x86_64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/badsum" GPP_INSTALL_DIR="${WORK}/install-badsum"
expect_status 1 "fails on a checksum mismatch"
expect_err_contains "sha256 mismatch" "reports the checksum mismatch"

# checksums.txt without the requested asset.
printf '%s  other.tar.gz\n' "$(hash_file "${WORK}/releases/v0.0.1/google-play-publisher_0.0.1_linux_amd64.tar.gz")" \
	>"${WORK}/releases/v0.0.1/checksums.txt"
run_setup GPP_VERSION=0.0.1 GPP_TEST_ONLY_OS=Linux GPP_TEST_ONLY_ARCH=x86_64 \
	GPP_RELEASES_BASE_URL="file://${WORK}/releases" GPP_INSTALL_DIR="${WORK}/install-noentry"
expect_status 1 "fails when the asset is absent from checksums.txt"
expect_err_contains "does not contain" "names the missing asset"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
