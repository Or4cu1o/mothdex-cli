#!/bin/bash
# Runs the installers' own member rules against the GUI archives of a GoReleaser output (docs/contracts/cli/ports.md
# 5.2, T-70 IC-3 and IC-4), so a layout change of the archives cannot silently break every GUI install.
#
#   scripts/check-gui-archives.sh [dist-dir]      default: dist (a snapshot or a release build)
#
# - install.sh is run for real against a fake release made of the dist files (the test shims of tests/installer serve
#   them; no network). linux_amd64 is installed for real: the probe needs a system libsdl2 (CI installs it), and the
#   result must be mothdex, mothdex-gui and mothdex-licenses holding exactly the files of THIRD_PARTY/. The arm
#   archives are checked with --dry-run (their binaries cannot run here, but extraction and the member rules do).
# - install.ps1's Expand-GuiArchive (the function itself, read from the file) is run on the Windows zip by
#   scripts/check-gui-zip.ps1 with PowerShell 7.
# A missing tool is an error, never a skip.
set -euo pipefail
cd "$(dirname "$0")/.."

dist=${1:-dist}
fail() { echo "check-gui-archives: $*" >&2; exit 1; }
for tool in jq sha256sum tar; do command -v "$tool" >/dev/null 2>&1 || fail "missing tool: $tool"; done
[ -f "$dist/metadata.json" ] || fail "$dist/metadata.json not found"
version=$(jq -r .version "$dist/metadata.json")
tag="v$version"

work=$(mktemp -d "${TMPDIR:-/tmp}/gui-archives.XXXXXX")
trap 'rm -rf "$work"' EXIT
release="$work/release/$tag"
mkdir -p "$release"
cp "$dist/checksums.txt" "$release/"
for f in "$dist"/mothdex_*.tar.gz "$dist"/mothdex-gui_*.tar.gz; do cp "$f" "$release/"; done

shims="$PWD/tests/installer/shims"
notices=$(cd THIRD_PARTY && for f in *; do [ -f "$f" ] && printf '%s\n' "$f"; done | sort)

run_installer() { # <uname -m> <args...>: install.sh against the fake release, with only the shims and the system tools
  machine=$1
  shift
  : >"$work/fake.log"
  env -i HOME="$work/home" PATH="$shims:/usr/bin:/bin" FAKE_RELEASE="$work/release" FAKE_LOG="$work/fake.log" \
    FAKE_UNAME_S=Linux FAKE_UNAME_M="$machine" sh install.sh "$@" 2>&1
}
mkdir -p "$work/home"

# arm64, armv7 and armv6: dry run, so the archive is downloaded, verified and extracted by the installer's rules
for pair in "aarch64 arm64" "armv7l armv7" "armv6l armv6"; do
  # shellcheck disable=SC2086  # intentional word splitting of the "uname -m target" pair
  set -- $pair
  out=$(run_installer "$1" --profile generic --bin-dir "$work/bin-$2" --version "$version" --dry-run) || {
    echo "$out" >&2
    fail "install.sh --dry-run failed for linux_$2"
  }
  echo "$out" | grep -q "GUI archive: mothdex-gui_${version}_linux_$2.tar.gz (verified)" || {
    echo "$out" >&2
    fail "install.sh did not accept the linux_$2 GUI archive"
  }
  echo "install.sh accepts mothdex-gui_${version}_linux_$2.tar.gz"
done

# amd64: a real install; the probe runs the real mothdex-gui against the system libsdl2
# the output goes into a variable: under pipefail, grep -q closing the pipe early could make ldconfig fail with SIGPIPE
libs=$(ldconfig -p 2>/dev/null || true)
case $libs in *libSDL2-2.0.so.0*) ;; *) fail "libsdl2 is not installed: the amd64 probe cannot run" ;; esac
mkdir -p "$work/bin-amd64"
chmod 0755 "$work/bin-amd64"
out=$(run_installer x86_64 --profile generic --bin-dir "$work/bin-amd64" --version "$version" --no-launcher) || {
  echo "$out" >&2
  fail "install.sh failed for linux_amd64"
}
echo "$out" | grep -q "Installed $work/bin-amd64/mothdex-gui" || {
  echo "$out" >&2
  fail "the amd64 GUI was not installed (probe or extraction refused it)"
}
[ -x "$work/bin-amd64/mothdex" ] || fail "mothdex was not installed"
got=$(cd "$work/bin-amd64/mothdex-licenses" && for f in *; do printf '%s\n' "$f"; done | sort)
[ "$got" = "$notices" ] || fail "mothdex-licenses differs from THIRD_PARTY/:
$(diff <(echo "$notices") <(echo "$got") || true)"
echo "install.sh installed linux_amd64: mothdex, mothdex-gui and $(echo "$notices" | grep -c .) notices"

# Windows: the installer's Expand-GuiArchive on the zip
command -v pwsh >/dev/null 2>&1 || fail "missing tool: pwsh (PowerShell 7)"
pwsh -NoProfile -File scripts/check-gui-zip.ps1 -Installer install.ps1 -Zip "$dist/mothdex-gui_${version}_windows_amd64.zip" -Notices "$(echo "$notices" | paste -sd, -)" ||
  fail "install.ps1 does not accept the Windows GUI archive"

echo "check-gui-archives: ok (version $version)"
