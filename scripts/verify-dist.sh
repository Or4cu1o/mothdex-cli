#!/usr/bin/env bash
# Verifies the GoReleaser output in a dist directory: archive names and
# contents, binary format and build settings per target, version info,
# SBOMs and checksums. Runs the linux binaries (arm via qemu-user-static).
#
# Usage: scripts/verify-dist.sh [dist-dir] [expected-version]
# Without an expected version the version must look like X.Y.Z-snapshot.
set -euo pipefail

dist="${1:-dist}"
want_version="${2:-}"
want_commit="$(git rev-parse HEAD)"
want_date="$(TZ=UTC git show -s --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ HEAD)"

fail() { echo "verify-dist: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "missing tool: $1"; }
for tool in jq file unzip tar go sha256sum qemu-aarch64-static qemu-arm-static; do need "$tool"; done

[ -f "$dist/metadata.json" ] || fail "$dist/metadata.json not found"
version="$(jq -r .version "$dist/metadata.json")"
commit="$(jq -r .commit "$dist/metadata.json")"
if [ -n "$want_version" ]; then
  [ "$version" = "$want_version" ] || fail "version is $version, want $want_version"
else
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-snapshot$ ]] || fail "version $version is not X.Y.Z-snapshot"
fi
[ "$commit" = "$want_commit" ] || fail "metadata commit $commit differs from HEAD $want_commit"

# target: archive suffix | file(1) pattern
targets=(
  "linux_amd64.tar.gz|ELF 64-bit.*x86-64"
  "linux_arm64.tar.gz|ELF 64-bit.*ARM aarch64"
  "linux_armv6.tar.gz|ELF 32-bit.*ARM, EABI5"
  "linux_armv7.tar.gz|ELF 32-bit.*ARM, EABI5"
  "linux_386.tar.gz|ELF 32-bit.*Intel (80386|i386)"
  "windows_amd64.zip|PE32\+ executable.*x86-64"
  "windows_arm64.zip|PE32\+ executable.*(Aarch64|ARM64)"
  "darwin_amd64.tar.gz|Mach-O 64-bit x86_64"
  "darwin_arm64.tar.gz|Mach-O 64-bit arm64"
)

expected_files=()
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

for entry in "${targets[@]}"; do
  suffix="${entry%%|*}"
  pattern="${entry#*|}"
  name="mothdex_${version}_${suffix}"
  archive="$dist/$name"
  sbom="$archive.sbom.json"
  expected_files+=("$name" "$name.sbom.json")
  [ -f "$archive" ] || fail "missing archive $name"
  [ -f "$sbom" ] || fail "missing SBOM $name.sbom.json"

  # SBOM: valid JSON, non-empty, lists the Go modules the binary embeds.
  jq -e . "$sbom" >/dev/null || fail "$name.sbom.json is not valid JSON"
  [ "$(wc -c <"$sbom")" -gt 1000 ] || fail "$name.sbom.json is suspiciously small"
  grep -q 'golang.org/x/crypto' "$sbom" || fail "$name.sbom.json does not list golang.org/x/crypto"

  # Archive contents.
  dir="$tmp/${suffix%%.*}"
  mkdir -p "$dir"
  case "$suffix" in
    *.zip) unzip -q "$archive" -d "$dir"; bin=mothdex.exe ;;
    *) tar -xzf "$archive" -C "$dir"; bin=mothdex ;;
  esac
  for f in "$bin" LICENSE README.md CHANGELOG.md; do
    [ -f "$dir/$f" ] || fail "$name lacks $f"
  done
  case "$suffix" in
    *.tar.gz) [ "$(stat -c %a "$dir/$bin")" = 755 ] || fail "$name: $bin is not mode 0755" ;;
  esac

  # Binary format and build settings.
  desc="$(file -b "$dir/$bin")"
  echo "$desc" | grep -Eq "$pattern" || fail "$name: unexpected format: $desc"
  case "$suffix" in
    linux_386*|linux_armv6*|linux_armv7*) echo "$desc" | grep -q "statically linked" || fail "$name: not statically linked: $desc" ;;
  esac
  info="$(go version -m "$dir/$bin")"
  echo "$info" | grep -q 'CGO_ENABLED=0' || fail "$name: not built with CGO_ENABLED=0"
  echo "$info" | grep -q -- '-trimpath=true' || fail "$name: not built with -trimpath"

  # Run what can run here and check the version line.
  runner=""
  case "$suffix" in
    linux_amd64*|linux_386*) runner="" ;;
    linux_arm64*) runner="qemu-aarch64-static" ;;
    linux_armv6*|linux_armv7*) runner="qemu-arm-static" ;;
    *) continue ;;
  esac
  out="$($runner "$dir/$bin")"
  want_out="mothdex ${version} (commit ${want_commit}, commit date ${want_date})"
  [ "$out" = "$want_out" ] || fail "$name: output '$out', want '$want_out'"
done

# checksums.txt lists exactly the expected archives and SBOMs and they match.
[ -s "$dist/checksums.txt" ] || fail "checksums.txt missing or empty"
listed="$(awk '{print $2}' "$dist/checksums.txt" | sort)"
want_list="$(printf '%s\n' "${expected_files[@]}" | sort)"
[ "$listed" = "$want_list" ] || fail "checksums.txt entries differ from the expected 18 files:
$listed"
(cd "$dist" && sha256sum --check --strict checksums.txt >/dev/null) || fail "checksums.txt does not match the files"

echo "verify-dist: ok (version $version, commit $commit, 9 targets)"
