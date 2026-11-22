#!/usr/bin/env bash
# Verifies the GoReleaser output in a dist directory: archive names and
# contents, binary format and build settings per target, version info,
# SBOMs and checksums, and the GUI archives (members, loader, gui tag, the
# pinned SDL2.dll). Runs the linux CLI binaries (arm via qemu-user-static).
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
for tool in jq file unzip tar go sha256sum; do need "$tool"; done

# qemu user-mode emulators: "-static" suffix on Debian packages, plain names elsewhere.
find_qemu() {
  for c in "qemu-$1-static" "qemu-$1"; do
    if command -v "$c" >/dev/null 2>&1; then echo "$c"; return; fi
  done
  fail "missing tool: qemu-$1 (package qemu-user or qemu-user-static)"
}
qemu_aarch64="$(find_qemu aarch64)"
qemu_arm="$(find_qemu arm)"

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
  # exactly these members: nothing of the GUI (mothdex-gui, THIRD_PARTY/, SDL2.dll) may land in a mothdex_ archive
  case "$suffix" in
    *.zip) cli_members="$(unzip -Z1 "$archive" | grep -v '/$' | sort)" ;;
    *) cli_members="$(tar -tzf "$archive" | grep -v '/$' | sort)" ;;
  esac
  cli_want="$(printf '%s\n' "$bin" LICENSE README.md CHANGELOG.md | sort)"
  [ "$cli_members" = "$cli_want" ] || fail "$name: members differ from bin, LICENSE, README.md, CHANGELOG.md:
$(diff <(echo "$cli_want") <(echo "$cli_members") || true)"
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
    linux_arm64*) runner="$qemu_aarch64" ;;
    linux_armv6*|linux_armv7*) runner="$qemu_arm" ;;
    *) continue ;;
  esac
  out="$($runner "$dir/$bin" version)"
  want_out="mothdex ${version} (commit ${want_commit:0:7}, ${want_date:0:10})"
  [ "$out" = "$want_out" ] || fail "$name: output '$out', want '$want_out'"
done

# The GUI archives (gui.md section 6, ports.md): mothdex-gui_<version>_<os>_<arch>, members exactly what the installers
# extract by name, the binary built with the gui tag, dynamic on Linux with the loader the installer expects.
# target: archive suffix | file(1) pattern | ELF interpreter ("" on Windows)
gui_targets=(
  "linux_amd64.tar.gz|ELF 64-bit.*x86-64|/lib64/ld-linux-x86-64.so.2"
  "linux_arm64.tar.gz|ELF 64-bit.*ARM aarch64|/lib/ld-linux-aarch64.so.1"
  "linux_armv6.tar.gz|ELF 32-bit.*ARM, EABI5|/lib/ld-linux-armhf.so.3"
  "linux_armv7.tar.gz|ELF 32-bit.*ARM, EABI5|/lib/ld-linux-armhf.so.3"
  "windows_amd64.zip|PE32\+ executable.*x86-64|"
)
notices="$(cd THIRD_PARTY && for f in *; do [ -f "$f" ] && printf 'THIRD_PARTY/%s\n' "$f"; done)"
[ -n "$notices" ] || fail "THIRD_PARTY/ has no files"
sdl2_checked=0
for entry in "${gui_targets[@]}"; do
  IFS='|' read -r suffix pattern interp <<<"$entry"
  name="mothdex-gui_${version}_${suffix}"
  archive="$dist/$name"
  expected_files+=("$name" "$name.sbom.json")
  [ -f "$archive" ] || fail "missing GUI archive $name"
  [ -f "$archive.sbom.json" ] || fail "missing SBOM $name.sbom.json"
  jq -e . "$archive.sbom.json" >/dev/null || fail "$name.sbom.json is not valid JSON"
  grep -q 'github.com/ebitengine/purego' "$archive.sbom.json" || fail "$name.sbom.json does not list purego"

  dir="$tmp/gui-${suffix%%.*}"
  mkdir -p "$dir"
  case "$suffix" in
    *.zip)
      bin=mothdex-gui.exe
      members="$(unzip -Z1 "$archive" | grep -v '/$' | sort)"
      unzip -q "$archive" -d "$dir"
      want_members="$(printf '%s\n' "$bin" LICENSE README.md CHANGELOG.md SDL2.dll "$notices" | sort)"
      ;;
    *)
      bin=mothdex-gui
      members="$(tar -tzf "$archive" | grep -v '/$' | sort)"
      tar -xzf "$archive" -C "$dir"
      want_members="$(printf '%s\n' "$bin" LICENSE README.md CHANGELOG.md "$notices" | sort)"
      ;;
  esac
  [ "$members" = "$want_members" ] || fail "$name: members differ from what the installers extract:
$(diff <(echo "$want_members") <(echo "$members") || true)"
  # by type, like the installers: only regular files and directories (a link, device or pipe fails at build time)
  case "$suffix" in
    *.zip) odd="$(unzip -Z "$archive" | grep -E '^[lbcp]' || true)" ;;
    *) odd="$(tar -tvzf "$archive" | cut -c1 | grep -v '^[-d]' || true)" ;;
  esac
  [ -z "$odd" ] || fail "$name: has a member that is not a regular file"
  case "$suffix" in
    *.tar.gz) [ "$(stat -c %a "$dir/$bin")" = 755 ] || fail "$name: $bin is not mode 0755" ;;
  esac

  desc="$(file -b "$dir/$bin")"
  echo "$desc" | grep -Eq "$pattern" || fail "$name: unexpected format: $desc"
  if [ -n "$interp" ]; then
    echo "$desc" | grep -q "dynamically linked" || fail "$name: the GUI must be dynamic on Linux: $desc"
    echo "$desc" | grep -q "interpreter $interp" || fail "$name: interpreter is not $interp: $desc"
  fi
  info="$(go version -m "$dir/$bin")"
  echo "$info" | grep -q 'CGO_ENABLED=0' || fail "$name: not built with CGO_ENABLED=0"
  echo "$info" | grep -q -- '-trimpath=true' || fail "$name: not built with -trimpath"
  case "$suffix" in
    linux_armv6*) echo "$info" | grep -q 'GOARM=6' || fail "$name: not built with GOARM=6" ;;
    linux_armv7*) echo "$info" | grep -q 'GOARM=7' || fail "$name: not built with GOARM=7" ;;
  esac
  echo "$info" | grep -Eq -- '-tags=.*gui' || fail "$name: not built with the gui tag"
  # go version -m does not print the linker flags; the full commit stamped by -X is in the binary's strings
  grep -aqF -- "$want_commit" "$dir/$bin" || fail "$name: the commit is not stamped into the binary"

  if [ "$suffix" = windows_amd64.zip ]; then
    # SDL2.dll is the pinned release: refetch it (exact URL, SHA-256 checked) and compare
    scripts/fetch-sdl2-windows.sh "$tmp/sdl2" >/dev/null || fail "could not refetch the pinned SDL2.dll"
    cmp -s "$tmp/sdl2/SDL2.dll" "$dir/SDL2.dll" || fail "$name: SDL2.dll is not the pinned one"
    sdl2_checked=1
  fi
done
[ "$sdl2_checked" -eq 1 ] || fail "the Windows GUI archive was not checked"

# checksums.txt lists exactly the expected archives and SBOMs and they match.
[ -s "$dist/checksums.txt" ] || fail "checksums.txt missing or empty"
listed="$(awk '{print $2}' "$dist/checksums.txt" | sort)"
want_list="$(printf '%s\n' "${expected_files[@]}" | sort)"
[ "$listed" = "$want_list" ] || fail "checksums.txt entries differ from the expected ${#expected_files[@]} files:
$listed"
(cd "$dist" && sha256sum --check --strict checksums.txt >/dev/null) || fail "checksums.txt does not match the files"

echo "verify-dist: ok (version $version, commit $commit, 9 targets, ${#gui_targets[@]} GUI archives)"
