#!/bin/sh
# Fetches the one SDL2.dll the Windows GUI archive ships (docs/contracts/cli/gui.md section 6, ports.md section 3): the
# official libsdl-org release named by scripts/sdl2-windows.pin, by exact URL, with its SHA-256 checked BEFORE anything
# is extracted. Only the entry SDL2.dll is read from the zip. Never cached, never fetched unpinned. Called by GoReleaser
# (`before.hooks` of .goreleaser.yaml), so the release job and the snapshot check use the same bytes.
#
#   scripts/fetch-sdl2-windows.sh <output-dir>      writes <output-dir>/SDL2.dll
set -eu
cd "$(dirname "$0")/.."

die() {
  printf 'fetch-sdl2-windows: %s\n' "$*" >&2
  exit 1
}

out=${1:-}
[ -n "$out" ] || die "usage: $0 <output-dir>"
pin=scripts/sdl2-windows.pin
[ -f "$pin" ] || die "$pin not found"

url=$(sed -n 's/^url=//p' "$pin")
want=$(sed -n 's/^sha256=//p' "$pin")
[ "$(printf '%s\n' "$url" | grep -c .)" -eq 1 ] || die "$pin needs exactly one url= line"
[ "$(printf '%s\n' "$want" | grep -c .)" -eq 1 ] || die "$pin needs exactly one sha256= line"
printf '%s\n' "$url" | grep -Eq '^https://github\.com/libsdl-org/SDL/releases/download/release-[0-9.]+/SDL2-[0-9.]+-win32-x64\.zip$' ||
  die "unexpected SDL2 URL: $url"
printf '%s\n' "$want" | grep -Eq '^[0-9a-f]{64}$' || die "the pinned SHA-256 is not 64 lower-case hex digits"

work=$(mktemp -d "${TMPDIR:-/tmp}/sdl2.XXXXXX")
trap 'rm -rf "$work"' EXIT

curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL --retry 2 --connect-timeout 20 --max-time 120 \
  -o "$work/sdl2.zip" "$url" || die "could not download $url"
have=$(sha256sum "$work/sdl2.zip" | cut -d' ' -f1)
[ "$have" = "$want" ] || die "SHA-256 mismatch for the SDL2 zip: got $have, pinned $want"

mkdir -p "$out"
unzip -p "$work/sdl2.zip" SDL2.dll >"$out/SDL2.dll" || die "SDL2.dll is not in the archive"
[ -s "$out/SDL2.dll" ] || die "SDL2.dll is empty"
chmod 0644 "$out/SDL2.dll"
printf 'fetch-sdl2-windows: SDL2.dll sha256 %s (zip %s verified)\n' "$(sha256sum "$out/SDL2.dll" | cut -d' ' -f1)" "$want"
