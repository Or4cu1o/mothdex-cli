#!/bin/sh
# Mothdex installer for Linux and macOS: Batocera, EmuELEC, Recalbox, Lakka, EmuDeck, RetroDECK and generic Linux.
# Windows and RetroBat use install.ps1.
#
# Download this file, read it, then run it. Do not pipe it into a shell:
#   curl -fsSLO https://raw.githubusercontent.com/Or4cu1o/mothdex-cli/main/install.sh
#   less install.sh && sh install.sh --dry-run
#
# What it does (docs/contracts/cli/release-artifacts.md, docs/release.md):
#   - downloads only from https://github.com/Or4cu1o/mothdex-cli/releases (the base URL is fixed in this file);
#   - checks the archive SHA-256 against checksums.txt of the same release, always, before anything is executable;
#   - verifies the cosign signature of checksums.txt when `cosign` is installed (or `gh attestation verify` when `gh`
#     is installed and logged in);
#   - installs the `mothdex` binary (0755) into the profile's bin_dir and, where the system has one, a launcher
#     into the profile's launcher dir.
# It never runs the downloaded archive's contents before verification and never pipes anything to a shell.

set -eu
umask 022

REPO="Or4cu1o/mothdex-cli"
BASE="https://github.com/$REPO/releases"
SIGNER_IDENTITY="https://github.com/$REPO/.github/workflows/release.yml@refs/tags/"
OIDC_ISSUER="https://token.actions.githubusercontent.com"
MARKER="# mothdex-launcher v1"
LAUNCHER_NAME="Mothdex.sh"

VERSION=""
PROFILE=""
BIN_DIR_OPT=""
DRY_RUN=0
UNINSTALL=0
WITH_LAUNCHER=0
TMP=""

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

NEWFILE=""
cleanup() {
  if [ -n "$NEWFILE" ]; then rm -f "$NEWFILE"; fi
  if [ -n "$TMP" ] && [ -d "$TMP" ]; then
    rm -rf "$TMP"
  fi
}

usage() {
  cat <<'EOF'
Usage: sh install.sh [options]

  --version <vX.Y.Z>   install this release (default: the latest non-pre-release)
  --profile <id>       batocera | emuelec | recalbox | lakka | emudeck | retrodeck | generic
                       (default: detected from the system)
  --bin-dir <dir>      install the binary here instead of the profile's bin_dir
  --launcher           also create the Port/launcher (off by default until `mothdex tui` exists)
  --dry-run            download and verify, then print what would be installed; change nothing
  --uninstall          remove the binary and the launcher created by this installer
  -h, --help           show this help

Paths must be absolute and contain only A-Z a-z 0-9 _ . / + -
EOF
}

while [ $# -gt 0 ]; do
  case $1 in
    --version | --profile | --bin-dir)
      [ $# -ge 2 ] || die "$1 needs a value"
      case $1 in
        --version) VERSION=$2 ;;
        --profile) PROFILE=$2 ;;
        --bin-dir) BIN_DIR_OPT=$2 ;;
      esac
      shift 2
      ;;
    --version=*) VERSION=${1#*=}; shift ;;
    --profile=*) PROFILE=${1#*=}; shift ;;
    --bin-dir=*) BIN_DIR_OPT=${1#*=}; shift ;;
    --launcher) WITH_LAUNCHER=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

# --- platform ---------------------------------------------------------------------------------------------------

# Only the kernel architecture matters (static binary); `uname -m` reports it (release-artifacts.md section 3).
detect_target() {
  sys=$(uname -s)
  mach=$(uname -m)
  case "$sys/$mach" in
    Linux/x86_64 | Linux/amd64) TARGET=linux_amd64 ;;
    Linux/i386 | Linux/i486 | Linux/i586 | Linux/i686) TARGET=linux_386 ;;
    Linux/aarch64 | Linux/arm64) TARGET=linux_arm64 ;;
    Linux/armv6*) TARGET=linux_armv6 ;;
    Linux/armv7* | Linux/armv8l) TARGET=linux_armv7 ;;
    Darwin/x86_64) TARGET=darwin_amd64 ;;
    Darwin/arm64) TARGET=darwin_arm64 ;;
    *) die "unsupported system: $sys/$mach" ;;
  esac
}

# --- profiles (shell port of profiles/*.yaml probes; docs/contracts/cli/profiles.md) ----------------------------

os_release_has() {
  [ -r /etc/os-release ] || return 1
  grep -qx "$1=$2" /etc/os-release 2>/dev/null || grep -qx "$1=\"$2\"" /etc/os-release 2>/dev/null
}

rom_ok() { [ -d "$1" ] && [ -w "$1" ]; }

detect_profile() {
  if [ -f /usr/share/batocera/batocera.version ] && rom_ok /userdata/roms; then
    PROFILE=batocera
  elif os_release_has ID emuelec && rom_ok /storage/roms; then
    PROFILE=emuelec
  elif { os_release_has NAME Lakka || os_release_has ID lakka; } && rom_ok /storage/roms; then
    PROFILE=lakka
  elif rom_ok /recalbox/share/roms; then
    PROFILE=recalbox
  else
    emudeck=0
    retrodeck=0
    if [ -n "${HOME:-}" ] && rom_ok "$HOME/Emulation/roms"; then emudeck=1; fi
    for d in /run/media/*/Emulation/roms; do
      if [ -d "$d" ]; then emudeck=1; fi
    done
    if [ -n "${HOME:-}" ] && rom_ok "$HOME/retrodeck/roms"; then retrodeck=1; fi
    if [ $((emudeck + retrodeck)) -eq 1 ] && [ $emudeck -eq 1 ]; then
      PROFILE=emudeck
    elif [ $((emudeck + retrodeck)) -eq 1 ]; then
      PROFILE=retrodeck
    else
      if [ $((emudeck + retrodeck)) -eq 2 ]; then
        warn "both EmuDeck and RetroDECK found: using generic (pass --profile to choose)"
      fi
      PROFILE=generic
    fi
  fi
}

# Sets BIN_DIR and LAUNCHER_DIR ("" = no launcher) for PROFILE.
resolve_profile_dirs() {
  home_bin=""
  if [ -n "${HOME:-}" ]; then home_bin="$HOME/.local/bin"; fi
  LAUNCHER_DIR=""
  case $PROFILE in
    batocera) BIN_DIR=/userdata/system/bin; LAUNCHER_DIR=/userdata/roms/ports ;;
    emuelec) BIN_DIR=/storage/.local/bin; LAUNCHER_DIR=/storage/roms/ports_scripts ;;
    recalbox) BIN_DIR=/recalbox/share/system/bin ;;
    lakka) BIN_DIR=/storage/.local/bin ;;
    emudeck | retrodeck | generic) BIN_DIR=$home_bin ;;
    retrobat) die "RetroBat is a Windows profile: use install.ps1" ;;
    *) die "unknown profile: $PROFILE" ;;
  esac
  if [ -n "$BIN_DIR_OPT" ]; then
    BIN_DIR=$BIN_DIR_OPT
  else
    say "note: bin_dir for the $PROFILE profile is not verified on real hardware yet; override with --bin-dir if needed."
  fi
  [ -n "$BIN_DIR" ] || die "HOME is not set: pass --bin-dir"
  if [ "$WITH_LAUNCHER" -ne 1 ] && [ "$UNINSTALL" -ne 1 ]; then LAUNCHER_DIR=""; fi
}

# --- path and directory safety (TH-30, TH-31) -------------------------------------------------------------------

# Absolute, only [A-Za-z0-9_./+-], no ".." component. The launcher is a fixed template and this check is what makes
# the quoted path safe: no spaces, quotes, backticks, `$` or newlines can get in.
valid_path() {
  case $1 in /*) ;; *) return 1 ;; esac
  case $1 in *[!A-Za-z0-9_./+-]*) return 1 ;; esac
  case $1 in */../* | */..) return 1 ;; esac
  return 0
}

# Directory not world-writable and owned by the installing user or root; and no ancestor (of the physical path) that
# other users can write to, unless it has the sticky bit (like /tmp): they could rename the directory away.
dir_safe() {
  if [ -n "$(find "$1" -maxdepth 0 -perm -0002 2>/dev/null)" ]; then return 1; fi
  uid=$(id -u)
  [ -n "$(find "$1" -maxdepth 0 \( -user "$uid" -o -user 0 \) 2>/dev/null)" ] || return 1
  real=$(cd -P -- "$1" 2>/dev/null && pwd -P) || return 1
  anc=$real
  while [ "$anc" != / ]; do
    anc=$(dirname "$anc")
    if [ -n "$(find "$anc" -maxdepth 0 -perm -0002 ! -perm -1000 2>/dev/null)" ]; then return 1; fi
  done
  return 0
}

# Filesystems without Unix permission bits: every directory looks world-writable there.
fat_fs() {
  case $(stat -f -c %T -- "$1" 2>/dev/null) in
    exfat | vfat | msdos | ntfs | ntfs3 | fuseblk) return 0 ;;
  esac
  return 1
}

# The only exception to the world-writable refusal (TH-31): an appliance (Batocera or Recalbox) share partition,
# as root, inside the profile's own tree, on a filesystem proven (not guessed from the mode) to have no permission
# bits. Owner must still be root; symlinks are refused; unknown or failing detection keeps the refusal.
fat_exception_ok() {
  [ "$(id -u)" -eq 0 ] || return 1
  case $PROFILE in
    batocera) root=/userdata ;;
    recalbox) root=/recalbox/share ;;
    *) return 1 ;;
  esac
  [ ! -L "$1" ] || return 1
  # the physical path (symlinked parents resolved) must be inside the profile tree
  real=$(cd -P -- "$1" 2>/dev/null && pwd -P) || return 1
  rootreal=$(cd -P -- "$root" 2>/dev/null && pwd -P) || return 1
  case "$real/" in "$rootreal"/*) ;; *) return 1 ;; esac
  [ -n "$(find "$real" -maxdepth 0 -user 0 2>/dev/null)" ] || return 1
  fat_fs "$real"
}

# dir_safe, or the exception above with a warning.
dir_trusted() {
  dir_safe "$1" && return 0
  fat_exception_ok "$1" || return 1
  fstype=$(stat -f -c %T -- "$1" 2>/dev/null)
  warn "$1 is on $fstype, which has no file permissions; any process or SMB client that can write $1 can replace mothdex"
  return 0
}

# A bin_dir on NTFS/exFAT or a noexec mount cannot run the binary: prove it with a throwaway script.
can_exec_in() {
  probe="$1/.mothdex-exec-test.$$"
  if ! printf '#!/bin/sh\nexit 0\n' >"$probe" 2>/dev/null; then return 1; fi
  chmod 0755 "$probe" 2>/dev/null || { rm -f "$probe"; return 1; }
  if "$probe" >/dev/null 2>&1; then rc=0; else rc=1; fi
  rm -f "$probe"
  return $rc
}

# Returns 0 when dir ($1) is usable as bin_dir, after creating it unless this is a dry run.
bin_dir_usable() {
  valid_path "$1" || die "bin_dir '$1' is not allowed: use an absolute path of only A-Z a-z 0-9 _ . / + - (no spaces, no '..')"
  if [ ! -d "$1" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then return 0; fi
    mkdir -p "$1" || return 1
  fi
  if [ -L "$1" ]; then die "refusing bin_dir '$1': it is a symbolic link"; fi
  dir_trusted "$1" || die "refusing bin_dir '$1': it is world-writable or owned by another user"
  if [ "$DRY_RUN" -eq 1 ]; then return 0; fi
  can_exec_in "$1"
}

# --- download and verification ---------------------------------------------------------------------------------

# Only URLs built from $BASE are ever passed in. curl is forced to https, including on redirects (--proto-redir).
# wget cannot do that: GNU Wget follows an https to http redirect in a plain download (--https-only is recursive-only),
# and busybox wget does not validate TLS certificates. So wget is accepted only when it is GNU Wget AND a signature tool
# that verify_signature will run (cosign, or a logged-in gh) is present: the signature check does not depend on how
# the files travelled.
gnu_wget() {
  wget --version 2>/dev/null | head -n 1 | grep -q 'GNU Wget'
}

signature_tool_present() {
  if command -v cosign >/dev/null 2>&1; then return 0; fi
  command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1
}

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL --retry 2 --connect-timeout 20 --max-time 600 \
      -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    gnu_wget || die "this wget is not GNU Wget (busybox wget does not verify TLS certificates): install curl and run again"
    signature_tool_present ||
      die "only wget is available, and wget can follow an https to http redirect: install curl, or cosign (or a logged-in gh) so the signature is verified, and run again"
    wget -q --secure-protocol=TLSv1_2 --max-redirect=5 --timeout=60 -O "$2" "$1"
  else
    die "curl or GNU wget is required"
  fi
}

valid_version() {
  case $1 in
    '' | *[!0-9A-Za-z.-]*) return 1 ;;
  esac
  printf '%s\n' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$'
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}'
  else
    die "no SHA-256 tool found (sha256sum, shasum or openssl)"
  fi
}

# The latest non-pre-release version, read from the latest release's checksums.txt (names carry the version).
resolve_latest() {
  # wget cannot refuse an https to http redirect, so a redirect could roll "latest" back to an older, signed release
  command -v curl >/dev/null 2>&1 || die "without curl the latest version cannot be trusted: pass --version vX.Y.Z (or install curl)"
  fetch "$BASE/latest/download/checksums.txt" "$TMP/latest-checksums.txt" || die "could not find the latest release"
  found=$(sed -n "s/^[0-9a-fA-F]*[ *]*mothdex_\\(.*\\)_${TARGET}\\.tar\\.gz\$/\\1/p" "$TMP/latest-checksums.txt")
  [ -n "$found" ] || die "the latest release has no archive for $TARGET"
  [ "$(printf '%s\n' "$found" | grep -c .)" -eq 1 ] || die "the latest release lists several archives for $TARGET"
  valid_version "$found" || die "the latest release has an unexpected version: refusing"
  VERSION=$found
}

verify_signature() {
  if command -v cosign >/dev/null 2>&1; then
    fetch "$URL/checksums.txt.sigstore.json" "$TMP/checksums.txt.sigstore.json" ||
      die "cosign is installed but the signature bundle could not be downloaded"
    if ! out=$(cosign verify-blob --bundle "$TMP/checksums.txt.sigstore.json" \
      --certificate-identity "$SIGNER_IDENTITY$TAG" --certificate-oidc-issuer "$OIDC_ISSUER" \
      "$TMP/checksums.txt" 2>&1); then
      die "cosign signature verification failed: $out"
    fi
    SIGNATURE="cosign signature verified"
  elif command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    if ! out=$(gh attestation verify "$TMP/$ARCHIVE" --repo "$REPO" \
      --signer-workflow "$REPO/.github/workflows/release.yml" --source-ref "refs/tags/$TAG" 2>&1); then
      die "gh attestation verification failed: $out"
    fi
    SIGNATURE="GitHub attestation verified"
  else
    SIGNATURE="signature not checked (cosign or a logged-in gh not found); SHA-256 only"
  fi
}

download_and_verify() {
  TMP=$(mktemp -d "${TMPDIR:-/tmp}/mothdex-install.XXXXXX") || die "cannot create a temporary directory"
  if [ -z "$VERSION" ]; then
    resolve_latest
  fi
  VERSION=${VERSION#v}
  valid_version "$VERSION" || die "invalid version '$VERSION' (expected X.Y.Z or X.Y.Z-rc.1)"
  TAG="v$VERSION"
  URL="$BASE/download/$TAG"
  ARCHIVE="mothdex_${VERSION}_${TARGET}.tar.gz"

  say "Downloading $ARCHIVE from $URL"
  fetch "$URL/checksums.txt" "$TMP/checksums.txt" || die "could not download checksums.txt for $TAG"
  fetch "$URL/$ARCHIVE" "$TMP/$ARCHIVE" || die "could not download $ARCHIVE (is $TAG released for $TARGET?)"

  verify_signature

  want=$(awk -v n="$ARCHIVE" '{ f = $2; sub(/^\*/, "", f); if (f == n) print tolower($1) }' "$TMP/checksums.txt")
  case $want in '' | *[!0-9a-f]*) die "checksums.txt has no single valid SHA-256 line for $ARCHIVE" ;; esac
  [ ${#want} -eq 64 ] || die "checksums.txt has no single valid SHA-256 line for $ARCHIVE"
  have=$(sha256_of "$TMP/$ARCHIVE")
  [ "$have" = "$want" ] || die "SHA-256 mismatch for $ARCHIVE: nothing was installed"

  mkdir "$TMP/x"
  tar -xzf "$TMP/$ARCHIVE" -C "$TMP/x" mothdex || die "$ARCHIVE does not contain the mothdex binary"
  if [ ! -f "$TMP/x/mothdex" ] || [ -L "$TMP/x/mothdex" ]; then
    die "$ARCHIVE: 'mothdex' is not a regular file"
  fi
}

# --- install, launcher, uninstall --------------------------------------------------------------------------------

install_binary() {
  new="$BIN_DIR/.mothdex.new.$$"
  NEWFILE=$new
  cp "$TMP/x/mothdex" "$new"
  chmod 0755 "$new"
  mv -f "$new" "$BIN_DIR/mothdex"
}

has_marker() { [ ! -L "$1" ] && grep -qxF "$MARKER" "$1" 2>/dev/null; }

# Fixed template; the only variable part is the validated bin_dir. Atomic; never overwrites a file without the marker.
install_launcher() {
  [ -n "$LAUNCHER_DIR" ] || return 0
  if ! valid_path "$LAUNCHER_DIR"; then
    warn "launcher dir '$LAUNCHER_DIR' has unsafe characters: launcher skipped"
    return 0
  fi
  target="$LAUNCHER_DIR/$LAUNCHER_NAME"
  if [ -e "$target" ] || [ -L "$target" ]; then
    if ! has_marker "$target"; then
      warn "keeping existing $target: it was not created by this installer"
      return 0
    fi
  fi
  if [ ! -d "$LAUNCHER_DIR" ]; then
    mkdir -p "$LAUNCHER_DIR" || { warn "cannot create $LAUNCHER_DIR: launcher skipped"; return 0; }
  fi
  if ! dir_trusted "$LAUNCHER_DIR"; then
    warn "$LAUNCHER_DIR is world-writable or owned by another user: launcher skipped (chmod 755 it to enable)"
    return 0
  fi
  tmpl="$LAUNCHER_DIR/.$LAUNCHER_NAME.$$"
  # shellcheck disable=SC2016  # "$@" must reach the launcher literally
  printf '#!/bin/sh\n%s\nexec "%s/mothdex" tui "$@"\n' "$MARKER" "$BIN_DIR" >"$tmpl" || {
    rm -f "$tmpl"
    warn "cannot write the launcher: skipped"
    return 0
  }
  chmod 0755 "$tmpl"
  mv -f "$tmpl" "$target"
  say "Launcher: $target"
}

do_uninstall() {
  bin="$BIN_DIR/mothdex"
  if [ -f "$bin" ] || [ -L "$bin" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $bin"; else rm -f "$bin"; say "Removed $bin"; fi
  else
    say "No binary at $bin"
    if [ -z "$BIN_DIR_OPT" ] && [ -n "${HOME:-}" ] && [ -f "$HOME/.local/bin/mothdex" ] && [ "$HOME/.local/bin" != "$BIN_DIR" ]; then
      warn "found $HOME/.local/bin/mothdex (the fallback used when $BIN_DIR cannot run programs); remove it with: sh install.sh --uninstall --bin-dir $HOME/.local/bin"
    fi
  fi
  if [ -n "$LAUNCHER_DIR" ] && [ -e "$LAUNCHER_DIR/$LAUNCHER_NAME" ]; then
    if has_marker "$LAUNCHER_DIR/$LAUNCHER_NAME"; then
      if [ "$DRY_RUN" -eq 1 ]; then
        say "Would remove $LAUNCHER_DIR/$LAUNCHER_NAME"
      else
        rm -f "$LAUNCHER_DIR/$LAUNCHER_NAME"
        say "Removed $LAUNCHER_DIR/$LAUNCHER_NAME"
      fi
    else
      warn "keeping $LAUNCHER_DIR/$LAUNCHER_NAME: it was not created by this installer"
    fi
  fi
  say "Your configuration, cache and ROMs were not touched."
}

# --- main -----------------------------------------------------------------------------------------------------------

trap cleanup EXIT
trap 'exit 1' INT TERM HUP

detect_target
if [ -z "$PROFILE" ]; then detect_profile; fi
resolve_profile_dirs
say "System: $TARGET, profile: $PROFILE"

if [ "$UNINSTALL" -eq 1 ]; then
  valid_path "$BIN_DIR" || die "bin_dir '$BIN_DIR' is not allowed"
  do_uninstall
  exit 0
fi

if ! bin_dir_usable "$BIN_DIR"; then
  fallback=""
  if [ -z "$BIN_DIR_OPT" ] && [ -n "${HOME:-}" ]; then fallback="$HOME/.local/bin"; fi
  if [ -z "$fallback" ] || [ "$fallback" = "$BIN_DIR" ]; then
    die "cannot install into $BIN_DIR (not writable or cannot run programs); pass --bin-dir"
  fi
  warn "$BIN_DIR cannot run programs (noexec or no permission bits): using $fallback"
  BIN_DIR=$fallback
  bin_dir_usable "$BIN_DIR" || die "cannot install into $BIN_DIR either; pass --bin-dir"
fi

download_and_verify

if [ "$DRY_RUN" -eq 1 ]; then
  say "Dry run: nothing was installed. Verified $ARCHIVE ($SIGNATURE)."
  say "Would install: $BIN_DIR/mothdex (0755)"
  if [ -n "$LAUNCHER_DIR" ]; then say "Would write: $LAUNCHER_DIR/$LAUNCHER_NAME"; fi
  exit 0
fi

install_binary
say "Installed $BIN_DIR/mothdex $VERSION ($SIGNATURE)"
install_launcher
"$BIN_DIR/mothdex" --version || warn "the installed binary did not run; check that this is the right architecture"
case ":${PATH:-}:" in
  *":$BIN_DIR:"*) ;;
  *) say "Add $BIN_DIR to PATH to run 'mothdex' directly." ;;
esac
