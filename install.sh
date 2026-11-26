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
#   - installs the `mothdex` binary (0755) into the profile's bin_dir and, only when the GUI is installed and the system has
#     one, a launcher (a Port, which runs mothdex-gui and nothing else) into the profile's launcher dir;
#   - when the release lists a `mothdex-gui_` archive for this machine, also installs `mothdex-gui` after proving
#     that it starts (`mothdex-gui --probe`, run from bin_dir with a scrubbed environment), plus its licence notices.
# It never runs the downloaded archive's contents before verification and never pipes anything to a shell.
# docs/contracts/cli/ports.md is the contract; docs/security/T-70-threat-model-ports-installer.md (IC-1 to IC-13)
# and docs/security/T-110-threat-model-beta-adjustments.md (TM-1 to TM-15) are the coding rules.
#
# The whole script is one brace group (TM-1): a truncated download is a syntax error and runs nothing. main runs with
# stdin from /dev/null (TM-2): under a pipe stdin is this script, so no command may read it.

{
set -eu
umask 022

REPO="Or4cu1o/mothdex-cli"
BASE="https://github.com/$REPO/releases"
SIGNER_IDENTITY="https://github.com/$REPO/.github/workflows/release.yml@refs/tags/"
OIDC_ISSUER="https://token.actions.githubusercontent.com"
MARKER_V1="# mothdex-launcher v1"
MARKER_V2="# mothdex-launcher v2"
MARKER="# mothdex-launcher v3"
LAUNCHER_NAME="Mothdex.sh"
LICENSES_NAME="mothdex-licenses"
# A THIRD_PARTY member must have a plain name (IC-4) and be one of the notices this installer ships; only those names are
# ever removed from mothdex-licenses/ (IC-5), so a file of the user's in that folder is never deleted.
NAME_RE='^[A-Za-z0-9._-]+$'
NOTICE_NAMES="README.md Handjet-OFL-1.1.txt NotoSans-OFL-1.1.txt SDL_GameControllerDB-zlib.txt SDL2-zlib.txt purego-Apache-2.0.txt x-image-BSD-3-Clause.txt go-modules-mothdex.txt go-modules-mothdex-gui.txt"
# Scrubbed environment and time bound of the probe (IC-1, IC-10).
PROBE_PATH=/usr/sbin:/usr/bin:/sbin:/bin
PROBE_SECONDS=20
REFRESH_URL="http://127.0.0.1:1234/reloadgames"
STATE_NAME=".mothdex-install.json"
STATE_MAX=16384
RC_MARK="# mothdex"
ALIAS_DEFAULT=mdex
ALIAS_FALLBACK=mthd

VERSION=""
PROFILE=""
BIN_DIR_OPT=""
DRY_RUN=0
UNINSTALL=0
MODIFY_PATH=1
ALIAS_OPT="" # --alias <name>
NO_ALIAS=0
LAUNCHER_OPT=""   # "" = default (on when the GUI is installed), 1 = --launcher (the same), 0 = --no-launcher
GUI_OPT=""        # "" = default (on), 1 = --gui, 0 = --no-gui
FAILED=0          # set when a step reports an error after the CLI install completed: exit status 1 at the end
TMP=""

say() { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

NEWFILE=""
NEWGUI=""
NEWLIC=""
NEWSTATE=""
NEWRC=""
# shellcheck disable=SC2317,SC2329  # runs from the EXIT trap
cleanup() {
  if [ -n "$NEWFILE" ]; then rm -f "$NEWFILE"; fi
  if [ -n "$NEWGUI" ]; then rm -f "$NEWGUI"; fi
  if [ -n "$NEWLIC" ]; then remove_licenses "$NEWLIC" quiet; fi
  if [ -n "$NEWSTATE" ]; then rm -f "$NEWSTATE"; fi
  if [ -n "$NEWRC" ]; then rm -f "$NEWRC"; fi
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
  --launcher           create the Port/launcher (the default; it is written only when the GUI is installed, because a
                       Port has no terminal to fall back to the text interface)
  --no-launcher        do not create or rewrite the launcher
  --gui                install mothdex-gui when the release has one (the default)
  --no-gui             install the CLI only; an installed mothdex-gui is removed (upgrade)
  --alias <name>       shortcut to create next to mothdex (default: mdex, or mthd when mdex is taken)
  --no-alias           create no shortcut; one an earlier install created is removed
  --no-modify-path     do not add the install folder to your PATH; print the command to do it yourself
  --dry-run            download and verify, then print what would be installed; leaves nothing changed
                       (the GUI check runs in an existing bin_dir and removes its staging file)
  --uninstall          remove the binaries, the licence notices, the launcher, the shortcut and the PATH line created by this installer
  -h, --help           show this help

Paths must be absolute and contain only A-Z a-z 0-9 _ . / + -
EOF
}

# ^[a-z][a-z0-9-]{1,15}$ and not a name of ours or a Windows device name (TM-12; the state file is shared with Windows).
# The letters are spelled out: a range can match capitals in some locales.
valid_alias() {
  case $1 in
    '' | *[!abcdefghijklmnopqrstuvwxyz0123456789-]*) return 1 ;;
    [!abcdefghijklmnopqrstuvwxyz]*) return 1 ;;
    mothdex | mothdex-gui | con | prn | aux | nul | com[0123456789] | lpt[0123456789]) return 1 ;;
  esac
  [ "${#1}" -ge 2 ] && [ "${#1}" -le 16 ]
}

set_alias_opt() {
  [ "$NO_ALIAS" -eq 0 ] || die "--alias and --no-alias cannot be used together"
  valid_alias "$1" || die "invalid alias: use 2 to 16 characters, a-z 0-9 and -, starting with a letter (not mothdex)"
  ALIAS_OPT=$1
}

parse_args() {
while [ $# -gt 0 ]; do
  case $1 in
    --version | --profile | --bin-dir | --alias)
      [ $# -ge 2 ] || die "$1 needs a value"
      case $1 in
        --version) VERSION=$2 ;;
        --profile) PROFILE=$2 ;;
        --bin-dir) BIN_DIR_OPT=$2 ;;
        --alias) set_alias_opt "$2" ;;
      esac
      shift 2
      ;;
    --version=*) VERSION=${1#*=}; shift ;;
    --profile=*) PROFILE=${1#*=}; shift ;;
    --bin-dir=*) BIN_DIR_OPT=${1#*=}; shift ;;
    --alias=*) set_alias_opt "${1#*=}"; shift ;;
    --no-alias) [ -z "$ALIAS_OPT" ] || die "--alias and --no-alias cannot be used together"; NO_ALIAS=1; shift ;;
    --launcher) [ "$LAUNCHER_OPT" != 0 ] || die "--launcher and --no-launcher cannot be used together"; LAUNCHER_OPT=1; shift ;;
    --no-launcher) [ "$LAUNCHER_OPT" != 1 ] || die "--launcher and --no-launcher cannot be used together"; LAUNCHER_OPT=0; shift ;;
    --gui) [ "$GUI_OPT" != 0 ] || die "--gui and --no-gui cannot be used together"; GUI_OPT=1; shift ;;
    --no-gui) [ "$GUI_OPT" != 1 ] || die "--gui and --no-gui cannot be used together"; GUI_OPT=0; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    --no-modify-path) MODIFY_PATH=0; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done
}

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
    curl -q --proto '=https' --proto-redir '=https' --tlsv1.2 -fsSL --retry 2 --connect-timeout 20 --max-time 600 \
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

notice_known() {
  for known in $NOTICE_NAMES; do
    if [ "$1" = "$known" ]; then return 0; fi
  done
  return 1
}

# The archive $1 in $TMP must match its one line of the verified checksums.txt.
check_sha() {
  want=$(awk -v n="$1" '{ f = $2; sub(/^\*/, "", f); if (f == n) print tolower($1) }' "$TMP/checksums.txt")
  case $want in '' | *[!0-9a-f]*) die "checksums.txt has no single valid SHA-256 line for $1" ;; esac
  [ ${#want} -eq 64 ] || die "checksums.txt has no single valid SHA-256 line for $1"
  have=$(sha256_of "$TMP/$1")
  [ "$have" = "$want" ] || die "SHA-256 mismatch for $1: nothing was installed"
}

# How many lines of the verified checksums.txt name $1.
listed_count() {
  awk -v n="$1" '{ f = $2; sub(/^\*/, "", f); if (f == n) c++ } END { print c + 0 }' "$TMP/checksums.txt"
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

  check_sha "$ARCHIVE"

  mkdir "$TMP/x"
  tar -xzf "$TMP/$ARCHIVE" -C "$TMP/x" mothdex || die "$ARCHIVE does not contain the mothdex binary"
  if [ ! -f "$TMP/x/mothdex" ] || [ -L "$TMP/x/mothdex" ]; then
    die "$ARCHIVE: 'mothdex' is not a regular file"
  fi
  gui_prepare
}

# --- the GUI archive (ports.md section 5.2) ---------------------------------------------------------------------

GUI_ON=0     # that archive was downloaded, verified and extracted
GUI_ARCHIVE=""

# IC-3: whether a GUI archive exists comes only from the verified checksums.txt. Not listed: skip with a note. Listed but
# not downloadable, or failing verification: abort the whole install, before anything is written.
gui_prepare() {
  case $TARGET in linux_*) ;; *) return 0 ;; esac
  GUI_ARCHIVE="mothdex-gui_${VERSION}_${TARGET}.tar.gz"
  n=$(listed_count "$GUI_ARCHIVE")
  case $n in
    0)
      if [ "$GUI_OPT" != 0 ]; then say "note: release $TAG has no GUI archive for $TARGET: CLI only"; fi
      return 0
      ;;
    1) ;;
    *) die "checksums.txt lists $GUI_ARCHIVE more than once" ;;
  esac
  [ "$GUI_OPT" != 0 ] || return 0
  fetch "$URL/$GUI_ARCHIVE" "$TMP/$GUI_ARCHIVE" ||
    die "$GUI_ARCHIVE is listed in checksums.txt but could not be downloaded: nothing was installed"
  check_sha "$GUI_ARCHIVE"
  if [ "$SIGNATURE" = "GitHub attestation verified" ]; then
    if ! out=$(gh attestation verify "$TMP/$GUI_ARCHIVE" --repo "$REPO" \
      --signer-workflow "$REPO/.github/workflows/release.yml" --source-ref "refs/tags/$TAG" 2>&1); then
      die "gh attestation verification failed for $GUI_ARCHIVE: $out"
    fi
  fi
  extract_gui
  GUI_ON=1
}

# IC-4: only members with a known name are accepted and extracted, by exact name (never a directory extract);
# everything extracted must be a regular file.
extract_gui() {
  members=$(tar -tzf "$TMP/$GUI_ARCHIVE" 2>/dev/null) || die "$GUI_ARCHIVE cannot be read: nothing was installed"
  # by type, before anything is extracted: only regular files and directories (a link, a device or a pipe is refused)
  odd=$(tar -tvzf "$TMP/$GUI_ARCHIVE" 2>/dev/null | cut -c1 | grep -v '^[-d]' | head -n 1) || true
  [ -z "$odd" ] || die "$GUI_ARCHIVE has a member that is not a regular file: nothing was installed"
  list=""
  have_bin=0
  GUI_NOTICES=""
  while IFS= read -r m; do
    case $m in
      mothdex-gui) have_bin=1; list="$list mothdex-gui" ;;
      LICENSE | README.md | CHANGELOG.md | THIRD_PARTY | THIRD_PARTY/) ;;
      THIRD_PARTY/*)
        base=${m#THIRD_PARTY/}
        case $base in . | .. | '') die "$GUI_ARCHIVE has an unsafe member name: nothing was installed" ;; esac
        printf '%s\n' "$base" | grep -Eq "$NAME_RE" || die "$GUI_ARCHIVE has an unsafe member name: nothing was installed"
        notice_known "$base" || die "$GUI_ARCHIVE has a THIRD_PARTY member this installer does not know ($base): nothing was installed. This installer may be older than the release: fetch install.sh from the release tag (raw.githubusercontent.com/$REPO/<tag>/install.sh)"
        list="$list THIRD_PARTY/$base"
        GUI_NOTICES="$GUI_NOTICES $base"
        ;;
      *) die "$GUI_ARCHIVE has an unexpected member: nothing was installed" ;;
    esac
  done <<EOF
$members
EOF
  [ "$have_bin" -eq 1 ] || die "$GUI_ARCHIVE does not contain mothdex-gui"
  mkdir "$TMP/g"
  # shellcheck disable=SC2086  # $list holds only names that passed the checks above
  tar -xzf "$TMP/$GUI_ARCHIVE" -C "$TMP/g" $list || die "$GUI_ARCHIVE could not be extracted: nothing was installed"
  if [ ! -f "$TMP/g/mothdex-gui" ] || [ -L "$TMP/g/mothdex-gui" ]; then
    die "$GUI_ARCHIVE: 'mothdex-gui' is not a regular file"
  fi
  # busybox tar lists a hard link like a file: after extracting, every file must have exactly one link
  if [ -n "$(find "$TMP/g" -type f -links +1 2>/dev/null)" ]; then
    die "$GUI_ARCHIVE has a member that is not a regular file (a hard link): nothing was installed"
  fi
  if [ -e "$TMP/g/THIRD_PARTY" ] || [ -L "$TMP/g/THIRD_PARTY" ]; then
    if [ ! -d "$TMP/g/THIRD_PARTY" ] || [ -L "$TMP/g/THIRD_PARTY" ]; then die "$GUI_ARCHIVE: THIRD_PARTY is not a directory"; fi
  fi
  for base in $GUI_NOTICES; do
    if [ ! -f "$TMP/g/THIRD_PARTY/$base" ] || [ -L "$TMP/g/THIRD_PARTY/$base" ]; then
      die "$GUI_ARCHIVE: THIRD_PARTY/$base is not a regular file"
    fi
  done
}

# --- install, launcher, uninstall --------------------------------------------------------------------------------

install_binary() {
  new="$BIN_DIR/.mothdex.new.$$"
  NEWFILE=$new
  cp "$TMP/x/mothdex" "$new"
  chmod 0755 "$new"
  mv -f "$new" "$BIN_DIR/mothdex"
}

GUI_INSTALLED=0
PROBE_RC=0
PROBE_MSG=""
PROBE_BOUNDED=0

# IC-1, IC-2, IC-10: the staged binary is probed where it will live (bin_dir, already proven trusted and executable,
# never the temp dir), with a scrubbed environment (no LD_*, SDL_* or MOTHDEX_*) and a time bound. Leaves the staged file.
run_probe() {
  bound=""
  PROBE_BOUNDED=0
  if command -v timeout >/dev/null 2>&1; then bound="timeout $PROBE_SECONDS"; PROBE_BOUNDED=1; fi
  # shellcheck disable=SC2086  # $bound is "timeout N" or empty
  if out=$($bound env -i PATH="$PROBE_PATH" "$1" --probe 2>&1); then PROBE_RC=0; else PROBE_RC=$?; fi
  # first line only, control characters removed, at most 200 bytes
  PROBE_MSG=$(printf '%s\n' "$out" | head -n 1 | head -c 200 | tr -d '[:cntrl:]')
}

# Stages the GUI binary in bin_dir, probes it and sets GUI_VERDICT: eligible, not-eligible or error.
GUI_VERDICT=""
stage_and_probe() {
  stage="$BIN_DIR/.mothdex-gui.new.$$"
  NEWGUI=$stage
  cp "$TMP/g/mothdex-gui" "$stage"
  chmod 0755 "$stage"
  run_probe "$stage"
  case $PROBE_RC in
    0) GUI_VERDICT=eligible ;;
    8) GUI_VERDICT=not-eligible ;;
    1) # up to 0.1.x exit 1 meant "SDL missing"; from 0.2.0 on it is an internal error (8 is "not eligible")
      case $VERSION in
        0.0.* | 0.1.*) GUI_VERDICT=not-eligible ;;
        *) GUI_VERDICT=error ;;
      esac
      ;;
    124) GUI_VERDICT=not-eligible; PROBE_MSG="the probe did not finish within $PROBE_SECONDS seconds" ;;
    137 | 143) # killed by the bound (busybox timeout may report the signal instead of 124); otherwise a crash
      if [ "$PROBE_BOUNDED" -eq 1 ]; then
        GUI_VERDICT=not-eligible
        PROBE_MSG="the probe did not finish within $PROBE_SECONDS seconds"
      else
        GUI_VERDICT=error
      fi
      ;;
    *) GUI_VERDICT=error ;; # 126, 127 (cannot be executed), a crash, anything unexpected: reported, never "not eligible"
  esac
}

drop_stage() {
  rm -f "$NEWGUI"
  NEWGUI=""
}

install_gui() {
  [ "$GUI_ON" -eq 1 ] || return 0
  stage_and_probe
  case $GUI_VERDICT in
    eligible)
      mv -f "$NEWGUI" "$BIN_DIR/mothdex-gui"
      NEWGUI=""
      GUI_INSTALLED=1
      say "Installed $BIN_DIR/mothdex-gui ($PROBE_MSG)"
      ;;
    not-eligible)
      drop_stage
      warn "mothdex-gui was not installed: ${PROBE_MSG:-not eligible on this system}. The CLI works; no launcher is written, because a Port has no terminal for 'mothdex tui'."
      ;;
    *)
      drop_stage
      printf 'error: could not run mothdex-gui --probe (exit %s): %s\n' "$PROBE_RC" "${PROBE_MSG:-no output}" >&2
      printf 'error: mothdex-gui was not installed; the CLI install is complete.\n' >&2
      FAILED=1
      ;;
  esac
}

# Removes the regular files named like a notice this installer ships from a licences directory, then the directory itself if it
# is empty (IC-5): no recursive delete, no link followed, anything else is kept.
remove_licenses() {
  d=$1
  if [ -L "$d" ]; then
    [ "${2:-}" = quiet ] || warn "not touching $d: it is a symbolic link"
    return 0
  fi
  [ -d "$d" ] || return 0
  for f in "$d"/* "$d"/.[!.]*; do
    if [ ! -e "$f" ] && [ ! -L "$f" ]; then continue; fi
    n=${f##*/}
    if [ -f "$f" ] && [ ! -L "$f" ] && notice_known "$n"; then
      rm -f "$f"
    elif [ "${2:-}" != quiet ]; then
      warn "keeping $f: this installer did not write it"
    fi
  done
  rmdir "$d" 2>/dev/null || { [ "${2:-}" = quiet ] || warn "kept $d: it holds files this installer did not write"; }
}

# IC-5: temp directory, then rename; files 0644, directory 0755; never through a link.
install_licenses() {
  [ "$GUI_INSTALLED" -eq 1 ] || return 0
  [ -n "$GUI_NOTICES" ] || { warn "the GUI archive carries no THIRD_PARTY notices"; return 0; }
  dest="$BIN_DIR/$LICENSES_NAME"
  if [ -L "$dest" ] || { [ -e "$dest" ] && [ ! -d "$dest" ]; }; then
    warn "refusing $dest: it is a link or not a directory; licence notices not installed"
    return 0
  fi
  tmpd="$BIN_DIR/.$LICENSES_NAME.new.$$"
  mkdir "$tmpd" || { warn "cannot create $tmpd: licence notices not installed"; return 0; }
  NEWLIC=$tmpd
  chmod 0755 "$tmpd"
  for base in $GUI_NOTICES; do
    cp "$TMP/g/THIRD_PARTY/$base" "$tmpd/$base"
    chmod 0644 "$tmpd/$base"
  done
  if [ -d "$dest" ]; then remove_licenses "$dest" quiet; fi
  if [ -e "$dest" ]; then
    warn "kept $dest: it holds files this installer did not write; licence notices not updated"
    return 0
  fi
  mv "$tmpd" "$dest"
  NEWLIC=""
}

# Whenever this release's GUI is not installed (--no-gui, not listed, not eligible, probe error) the GUI files of an earlier
# install go too: the launcher must not start an older GUI next to this CLI, or one the probe just showed cannot start.
remove_gui() {
  why=$1
  gui="$BIN_DIR/mothdex-gui"
  if [ -f "$gui" ] || [ -L "$gui" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $gui ($why)"; else rm -f "$gui"; say "Removed $gui ($why)"; fi
  fi
  if [ -d "$BIN_DIR/$LICENSES_NAME" ] || [ -L "$BIN_DIR/$LICENSES_NAME" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $BIN_DIR/$LICENSES_NAME ($why)"; else remove_licenses "$BIN_DIR/$LICENSES_NAME"; fi
  fi
}

has_marker() {
  [ ! -L "$1" ] && { grep -qxF "$MARKER" "$1" 2>/dev/null || grep -qxF "$MARKER_V2" "$1" 2>/dev/null || grep -qxF "$MARKER_V1" "$1" 2>/dev/null; }
}

# D32 (PRT-G8): a Port has no terminal, so the launcher is written only when the GUI is installed and runs mothdex-gui and
# nothing else. D22 reads "launcher on when the GUI is installed". Without the GUI (not eligible, --no-gui, no GUI archive)
# no launcher is written and an existing launcher that carries one of our markers is removed. --no-launcher stops the
# installer from writing or rewriting it.
LAUNCHER_ON=0
LAUNCHER_WRITTEN=0
LAUNCHER_REMOVED=0
decide_launcher() { # after the GUI outcome is known
  LAUNCHER_ON=0
  if [ "$GUI_INSTALLED" -ne 1 ]; then
    if [ "$LAUNCHER_OPT" = 1 ]; then say "note: no launcher: it is written only when the GUI is installed (a Port has no terminal for 'mothdex tui')"; fi
    return 0
  fi
  [ "$LAUNCHER_OPT" != 0 ] || return 0
  if [ -z "$LAUNCHER_DIR" ]; then
    if [ "$LAUNCHER_OPT" = 1 ]; then say "note: the $PROFILE profile has no launcher folder: launcher skipped"; fi
    return 0
  fi
  LAUNCHER_ON=1
}

# Without the GUI, a launcher of ours would start nothing: it is removed, never anything without a marker (has_marker refuses
# a symbolic link too). $1 says why.
remove_launcher() {
  [ -n "$LAUNCHER_DIR" ] || return 0
  target="$LAUNCHER_DIR/$LAUNCHER_NAME"
  if [ ! -e "$target" ] && [ ! -L "$target" ]; then return 0; fi
  has_marker "$target" || return 0
  # LAUNCHER_DIR is a constant of the profile (resolve_profile_dirs), never user input, and rm quotes the path: no path check here
  if [ "$DRY_RUN" -eq 1 ]; then
    say "Would remove $target ($1)"
  else
    rm -f "$target" || { warn "cannot remove $target: left in place"; FAILED=1; return 0; }
    LAUNCHER_REMOVED=1
    say "Removed $target ($1)"
  fi
}

# Fixed template (marker v3); the only variable part is the validated bin_dir. No argument forwarding (IC-6) and no
# environment of its own (ports.md 2.3). Atomic; never overwrites a file without a marker.
install_launcher() {
  [ "$LAUNCHER_ON" -eq 1 ] || return 0
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
  printf '#!/bin/sh\n%s\nexec "%s/mothdex-gui"\n' "$MARKER" "$BIN_DIR" >"$tmpl" || {
    rm -f "$tmpl"
    warn "cannot write the launcher: skipped"
    return 0
  }
  chmod 0755 "$tmpl"
  mv -f "$tmpl" "$target"
  LAUNCHER_WRITTEN=1
  say "Launcher: $target (opt out with --no-launcher; --uninstall removes it)"
}

# IC-11 (TH-22): ask the frontend to reload its game list, only where the profile has a verified http_get refresh
# (profiles/*.yaml). Literal loopback URL, no proxy, no redirect, 3 seconds, curl only, never fatal.
refresh_frontend() {
  if [ "$LAUNCHER_WRITTEN" -ne 1 ] && [ "$LAUNCHER_REMOVED" -ne 1 ]; then return 0; fi
  case $PROFILE in batocera | emuelec) ;; *) return 0 ;; esac
  if command -v curl >/dev/null 2>&1 &&
    curl -q --noproxy '*' --max-redirs 0 --max-time 3 -fs -o /dev/null "$REFRESH_URL" 2>/dev/null; then
    say "Asked the frontend to reload its game list."
  else
    say "note: could not ask the frontend to reload its game list; the Port shows after the frontend reloads or restarts."
  fi
}

# --- install state (docs/contracts/cli/install-state.md) and PATH (T-110 TM-7 to TM-11, TM-14) -------------------

ST_PRESENT=0 # a valid state file of this folder was read
ST_INSTALLED_AT=""
ST_ADDED=false
ST_KIND=""
ST_FILE=""
ST_VALUE="" # raw JSON text of path_value; only ever compared, never evaluated
ST_ALIAS=""

# The raw text of one string field, or "" ; fails unless the key appears on exactly one line in the canonical layout.
state_string() {
  n=$(grep -c "^  \"$2\": " "$1" 2>/dev/null) || return 1
  [ "$n" -eq 1 ] || return 1
  grep -Eq "^  \"$2\": \"([^\"\\\\]|\\\\[\"\\\\])*\",?\$" "$1" || return 1
  sed -n "s/^  \"$2\": \"\\(.*\\)\",\\{0,1\\}\$/\\1/p" "$1"
}

# The line this installer adds to the rc file of kind $1 for BIN_DIR (TM-9). BIN_DIR passed valid_path.
rc_line_for() {
  # shellcheck disable=SC2016  # $PATH must reach the rc file literally
  case $1 in
    fish) printf "fish_add_path -g '%s' %s" "$BIN_DIR" "$RC_MARK" ;;
    *) printf 'export PATH="%s:$PATH" %s' "$BIN_DIR" "$RC_MARK" ;;
  esac
}

json_escape() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }

known_rc() {
  [ -n "${HOME:-}" ] || return 1
  case $1 in
    "$HOME/.bashrc" | "$HOME/.zshrc" | "$HOME/.profile" | "$HOME/.config/fish/config.fish") return 0 ;;
  esac
  return 1
}

# Every value is checked again on read (TM-14). An invalid file counts as absent: warned about, nothing it names touched.
read_state() {
  ST_PRESENT=0
  f="$BIN_DIR/$STATE_NAME"
  if [ ! -e "$f" ] && [ ! -L "$f" ]; then return 0; fi
  if state_valid "$f"; then
    ST_PRESENT=1
  else
    ST_ADDED=false; ST_KIND=""; ST_FILE=""; ST_VALUE=""; ST_INSTALLED_AT=""; ST_ALIAS=""
    warn "ignoring $f: it is not a valid install state of this folder"
  fi
}

state_valid() {
  [ -f "$1" ] && [ ! -L "$1" ] || return 1
  [ "$(wc -c <"$1")" -le "$STATE_MAX" ] || return 1
  grep -qx '  "schema_version": 1,' "$1" || return 1
  [ "$(grep -c '"schema_version"' "$1")" -eq 1 ] || return 1
  v=$(state_string "$1" source) && [ "$v" = installer ] || return 1
  v=$(state_string "$1" dir) && [ "$v" = "$BIN_DIR" ] || return 1
  v=$(state_string "$1" version) && valid_version "$v" || return 1
  ST_INSTALLED_AT=$(state_string "$1" installed_at) || return 1
  printf '%s\n' "$ST_INSTALLED_AT" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+(Z|[+-][0-9:]+)$' || return 1
  ST_ALIAS=$(state_string "$1" alias) || return 1
  v=$(state_string "$1" alias_kind) || return 1
  if [ -z "$ST_ALIAS" ]; then [ -z "$v" ] || return 1
  else valid_alias "$ST_ALIAS" && [ "$v" = symlink ] || return 1; fi
  ST_KIND=$(state_string "$1" path_kind) || return 1
  ST_FILE=$(state_string "$1" path_file) || return 1
  ST_VALUE=$(state_string "$1" path_value) || return 1
  if grep -qx '  "path_added": true,' "$1"; then ST_ADDED=true
  elif grep -qx '  "path_added": false,' "$1"; then ST_ADDED=false
  else return 1; fi
  [ "$(grep -c '"path_added"' "$1")" -eq 1 ] || return 1
  case $ST_KIND in
    '') [ "$ST_ADDED" = false ] && [ -z "$ST_FILE" ] && [ -z "$ST_VALUE" ] ;;
    rc_line | fish) known_rc "$ST_FILE" && [ "$ST_VALUE" = "$(json_escape "$(rc_line_for "$ST_KIND")")" ] ;;
    *) return 1 ;;
  esac
}

# Atomic (temporary file in the same folder, then rename), 0644, never through a link.
write_state() {
  f="$BIN_DIR/$STATE_NAME"
  if [ -L "$f" ] || { [ -e "$f" ] && [ ! -f "$f" ]; }; then
    warn "$f is not a regular file: left alone; mothdex upgrade will not be available for this install"
    return 0
  fi
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  gui=false
  if [ "$GUI_INSTALLED" -eq 1 ]; then gui=true; fi
  launcher=""
  if [ -n "$LAUNCHER_DIR" ] && has_marker "$LAUNCHER_DIR/$LAUNCHER_NAME"; then launcher="$LAUNCHER_DIR/$LAUNCHER_NAME"; fi
  alias_kind=""
  if [ -n "$ALIAS" ]; then alias_kind=symlink; fi
  tmps=$(mktemp "$BIN_DIR/$STATE_NAME.new.XXXXXX" 2>/dev/null) || {
    warn "cannot write $f: mothdex upgrade will not be available for this install"
    return 0
  }
  NEWSTATE=$tmps
  if printf '{
  "schema_version": 1,
  "source": "installer",
  "installer": "install.sh",
  "dir": "%s",
  "version": "%s",
  "target": "%s",
  "profile": "%s",
  "gui": %s,
  "alias": "%s",
  "alias_kind": "%s",
  "path_added": %s,
  "path_kind": "%s",
  "path_file": "%s",
  "path_value": "%s",
  "launcher": "%s",
  "installed_at": "%s",
  "updated_at": "%s",
  "updated_by": "installer"
}\n' "$BIN_DIR" "$VERSION" "$TARGET" "$PROFILE" "$gui" "$ALIAS" "$alias_kind" "$ST_ADDED" "$ST_KIND" "$ST_FILE" "$ST_VALUE" "$launcher" \
    "${ST_INSTALLED_AT:-$now}" "$now" >"$tmps" && chmod 0644 "$tmps" && mv -f "$tmps" "$f"; then
    NEWSTATE=""
  else
    rm -f "$tmps"
    NEWSTATE=""
    warn "cannot write $f: mothdex upgrade will not be available for this install"
  fi
}

# --- the shortcut (T-110 TM-12, TM-13) ------------------------------------------------------------------------------

ALIAS=""      # the shortcut of this install, "" for none
ALIAS_BUSY="" # what holds the last name that was refused

# Our link is the relative symbolic link to mothdex, so an upgrade needs nothing.
alias_is_ours() { [ -L "$BIN_DIR/$1" ] && [ "$(readlink "$BIN_DIR/$1")" = mothdex ]; }

# A name is free when nothing in bin_dir holds it and the shell resolves it to nothing; the link the state records as
# ours does not count.
alias_free() {
  p="$BIN_DIR/$1"
  recorded=0
  if [ "$1" = "$ST_ALIAS" ] && alias_is_ours "$1"; then recorded=1; fi
  if { [ -e "$p" ] || [ -L "$p" ]; } && [ "$recorded" -eq 0 ]; then ALIAS_BUSY=$p; return 1; fi
  found=$(command -v "$1" 2>/dev/null) || return 0
  if [ "$recorded" -eq 1 ] && [ "$found" -ef "$p" ]; then return 0; fi
  ALIAS_BUSY=$found
  return 1
}

# Sets ALIAS. A name the user chose has no fallback; otherwise the recorded one, then mdex, then mthd.
choose_alias() {
  ALIAS=""
  [ "$NO_ALIAS" -eq 0 ] || return 0
  if [ -n "$ALIAS_OPT" ]; then names=$ALIAS_OPT; else names="$ST_ALIAS $ALIAS_DEFAULT $ALIAS_FALLBACK"; fi
  tried=" "
  for name in $names; do
    case $tried in *" $name "*) continue ;; esac
    tried="$tried$name "
    if alias_free "$name"; then ALIAS=$name; return 0; fi
    warn "$name is already used ($ALIAS_BUSY): left alone"
  done
}

remove_alias() { # name; only our link
  if alias_is_ours "$1"; then
    rm -f "$BIN_DIR/$1" && say "Removed $BIN_DIR/$1"
  elif [ -e "$BIN_DIR/$1" ] || [ -L "$BIN_DIR/$1" ]; then
    warn "keeping $BIN_DIR/$1: it is not the shortcut this installer created"
  fi
}

update_alias() {
  choose_alias
  if [ -n "$ALIAS" ] && ! alias_is_ours "$ALIAS"; then
    # ln -s never replaces an existing name
    if ! ln -s mothdex "$BIN_DIR/$ALIAS" 2>/dev/null; then
      warn "cannot create the shortcut $BIN_DIR/$ALIAS (the file system may not support links)"
      ALIAS=""
    fi
  fi
  if [ -n "$ST_ALIAS" ] && [ "$ST_ALIAS" != "$ALIAS" ]; then remove_alias "$ST_ALIAS"; fi
}

# bash, zsh, fish, sh (anything POSIX, which reads ~/.profile) or other.
detect_shell() {
  case ${SHELL:-} in
    */bash) USER_SHELL=bash ;;
    */zsh) USER_SHELL=zsh ;;
    */fish) USER_SHELL=fish ;;
    '' | */sh | */ash | */dash | */ksh) USER_SHELL='sh' ;;
    *) USER_SHELL=other ;;
  esac
}

# Follows a chain of symbolic links (dotfile managers) and prints the final path.
resolve_link() {
  p=$1
  i=0
  while [ -L "$p" ]; do
    i=$((i + 1))
    [ "$i" -le 20 ] || return 1
    t=$(readlink "$p") || return 1
    case $t in
      /*) p=$t ;;
      *) p="$(dirname "$p")/$t" ;;
    esac
  done
  printf '%s\n' "$p"
}

# fish_add_path exists from fish 3.2 on; on an older one the line would fail at every shell start. An unknown version is
# taken as new enough (fish is only asked for its version).
fish_too_old() {
  command -v fish >/dev/null 2>&1 || return 1
  v=$(fish --version </dev/null 2>/dev/null | sed -n 's/.*version \([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p')
  [ -n "$v" ] || return 1
  major=${v% *}
  minor=${v#* }
  [ "$major" -lt 3 ] || { [ "$major" -eq 3 ] && [ "$minor" -lt 2 ]; }
}

# TM-9: sets RC_REAL to the file to write for rc path $1, or RC_WHY to why it is left alone.
RC_REAL=""
RC_WHY=""
rc_writable() {
  uid=$(id -u)
  [ -d "$HOME" ] || { RC_WHY="HOME ($HOME) is not a folder"; return 1; }
  if [ -z "$(find "$HOME/." -maxdepth 0 -user "$uid" 2>/dev/null)" ]; then
    RC_WHY="HOME ($HOME) is not owned by the installing user (sudo?)"
    return 1
  fi
  RC_REAL=$(resolve_link "$1") || { RC_WHY="$1: too many links"; return 1; }
  if [ ! -e "$RC_REAL" ]; then
    [ ! -L "$1" ] || { RC_WHY="$1 is a dangling link"; return 1; }
    return 0
  fi
  [ -f "$RC_REAL" ] || { RC_WHY="$1 is not a regular file"; return 1; }
  [ -n "$(find "$RC_REAL" -maxdepth 0 -user "$uid" 2>/dev/null)" ] || { RC_WHY="$1 is owned by another user"; return 1; }
  if [ -n "$(find "$RC_REAL" -maxdepth 0 \( -perm -0020 -o -perm -0002 \) 2>/dev/null)" ]; then
    RC_WHY="$1 can be written by other users"
    return 1
  fi
  return 0
}

rc_has_line() { [ -f "$1" ] && grep -qxF -- "$2" "$1" 2>/dev/null; }

rc_append() { # file, line
  mkdir -p "$(dirname "$1")" 2>/dev/null || return 1
  if [ -s "$1" ] && [ -n "$(tail -c 1 "$1")" ]; then printf '\n' 2>/dev/null >>"$1" || return 1; fi
  printf '%s\n' "$2" 2>/dev/null >>"$1"
}

# The command the user can copy, for the detected shell (spec section 1). BIN_DIR passed valid_path.
print_path_block() {
  say ""
  say "To add mothdex to your PATH, copy and run:"
  say ""
  case $USER_SHELL in
    fish) say "  fish_add_path $BIN_DIR" ;;
    bash) say "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.bashrc" ;;
    zsh) say "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc" ;;
    sh) say "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.profile" ;;
    *) say "  export PATH=\"$BIN_DIR:\$PATH\"    (put it in the startup file of your shell)" ;;
  esac
  say ""
}

PATH_RESULT="" # added, present or manual
update_path() {
  detect_shell
  on_path=0
  case ":${PATH:-}:" in *":$BIN_DIR:"* | *":$BIN_DIR/:"*) on_path=1 ;; esac
  rc=""
  kind=rc_line
  if [ -n "${HOME:-}" ]; then
    case $USER_SHELL in
      bash) rc="$HOME/.bashrc" ;;
      zsh) if [ "${ZDOTDIR:-$HOME}" = "$HOME" ]; then rc="$HOME/.zshrc"; fi ;;
      fish) rc="$HOME/.config/fish/config.fish"; kind=fish ;;
      sh) rc="$HOME/.profile" ;;
    esac
  fi
  if [ "$MODIFY_PATH" -ne 1 ]; then
    PATH_RESULT=manual
    print_path_block
    return 0
  fi
  line=$(rc_line_for "$kind")
  if [ -n "$rc" ] && rc_has_line "$rc" "$line"; then
    # our own line (it carries the marker) from an earlier install
    ST_ADDED=true; ST_KIND=$kind; ST_FILE=$rc; ST_VALUE=$(json_escape "$line")
    if [ "$on_path" -eq 1 ]; then PATH_RESULT=present; else PATH_RESULT=added; fi
    return 0
  fi
  if [ "$on_path" -eq 1 ]; then PATH_RESULT=present; return 0; fi
  PATH_RESULT=manual
  if [ "$ST_ADDED" = true ] && rc_has_line "$ST_FILE" "$(rc_line_for "$ST_KIND")"; then
    say "note: an earlier install already added mothdex to $ST_FILE; only one file is managed."
  elif [ -z "$rc" ]; then
    say "note: no startup file is managed for this shell."
  elif ! valid_path "$rc"; then
    warn "$rc has characters this installer does not write to: PATH not changed"
  elif [ "$kind" = fish ] && fish_too_old; then
    warn "fish older than 3.2 has no fish_add_path: PATH not changed; add $BIN_DIR to fish_user_paths yourself"
  elif ! dir_safe "$BIN_DIR"; then
    warn "$BIN_DIR can be written by other users or processes: it is not added to PATH automatically"
  elif ! rc_writable "$rc"; then
    warn "PATH not changed: $RC_WHY"
  elif ! rc_append "$RC_REAL" "$line"; then
    warn "cannot write $rc: PATH not changed"
  else
    ST_ADDED=true; ST_KIND=$kind; ST_FILE=$rc; ST_VALUE=$(json_escape "$line")
    PATH_RESULT=added
    return 0
  fi
  print_path_block
}

# TM-10: removes only the lines equal to the recorded one (read_state proved it is our template for BIN_DIR, in one of
# the known files under HOME); the file is rewritten atomically with its mode and owner.
remove_path_line() {
  [ "$ST_ADDED" = true ] || return 0
  line=$(rc_line_for "$ST_KIND")
  rc_has_line "$ST_FILE" "$line" || return 0
  if [ "$MODIFY_PATH" -ne 1 ]; then say "Keeping the PATH line in $ST_FILE (--no-modify-path)"; return 0; fi
  if [ "$DRY_RUN" -eq 1 ]; then say "Would remove the mothdex PATH line from $ST_FILE"; return 0; fi
  if ! rc_writable "$ST_FILE"; then
    warn "PATH line left in place ($RC_WHY): remove this line yourself: $line"
    return 0
  fi
  tmprc=$(mktemp "$RC_REAL.mothdex.XXXXXX" 2>/dev/null) || { warn "cannot rewrite $ST_FILE: remove this line yourself: $line"; return 0; }
  NEWRC=$tmprc
  if cp -p "$RC_REAL" "$tmprc" && MOTHDEX_LINE=$line awk '$0 != ENVIRON["MOTHDEX_LINE"]' "$RC_REAL" >"$tmprc" &&
    mv -f "$tmprc" "$RC_REAL"; then
    say "Removed the mothdex PATH line from $ST_FILE"
  else
    rm -f "$tmprc"
    warn "cannot rewrite $ST_FILE: remove this line yourself: $line"
  fi
  NEWRC=""
}

# The closing message, in Portuguese when the system language is Portuguese (and the locale is UTF-8), else English.
final_message() {
  lc=${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}
  ok="*"
  lang=en
  case $lc in
    *[Uu][Tt][Ff]8* | *[Uu][Tt][Ff]-8*) ok="✓"; case $lc in pt*) lang=pt ;; esac ;;
  esac
  say ""
  if [ "$lang" = pt ]; then
    say "mothdex $VERSION instalado em $BIN_DIR"
    case $PATH_RESULT in
      added) say "$ok PATH do usuário atualizado. Abra um terminal novo, ou rode neste: export PATH=\"$BIN_DIR:\$PATH\"" ;;
      present) say "$ok A pasta já está no PATH." ;;
      *) say "O PATH não foi alterado. Use o comando acima, ou rode $BIN_DIR/mothdex." ;;
    esac
    if [ -n "$ALIAS" ]; then say "Atalho: $ALIAS"
    elif [ "$NO_ALIAS" -eq 0 ]; then say "Sem atalho: os nomes já estão em uso ou o link não pôde ser criado (veja acima)."; fi
    say "Teste: ${ALIAS:-mothdex} --version"
  else
    say "mothdex $VERSION installed in $BIN_DIR"
    case $PATH_RESULT in
      added) say "$ok User PATH updated. Open a new terminal, or run in this one: export PATH=\"$BIN_DIR:\$PATH\"" ;;
      present) say "$ok The folder is already on your PATH." ;;
      *) say "PATH was not changed. Use the command above, or run $BIN_DIR/mothdex." ;;
    esac
    if [ -n "$ALIAS" ]; then say "Shortcut: $ALIAS"
    elif [ "$NO_ALIAS" -eq 0 ]; then say "No shortcut: the names are taken or the link could not be created (see above)."; fi
    say "Test: ${ALIAS:-mothdex} --version"
  fi
}

do_uninstall() {
  read_state
  for name in mothdex mothdex-gui; do
    bin="$BIN_DIR/$name"
    if [ -f "$bin" ] || [ -L "$bin" ]; then
      if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $bin"; else rm -f "$bin"; say "Removed $bin"; fi
    elif [ "$name" = mothdex ]; then
      say "No binary at $bin"
      if [ -z "$BIN_DIR_OPT" ] && [ -n "${HOME:-}" ] && [ -f "$HOME/.local/bin/mothdex" ] && [ "$HOME/.local/bin" != "$BIN_DIR" ]; then
        warn "found $HOME/.local/bin/mothdex (the fallback used when $BIN_DIR cannot run programs); remove it with: sh install.sh --uninstall --bin-dir $HOME/.local/bin"
      fi
    fi
  done
  lic="$BIN_DIR/$LICENSES_NAME"
  if [ -d "$lic" ] || [ -L "$lic" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $lic"; else
      remove_licenses "$lic"
      if [ ! -e "$lic" ] && [ ! -L "$lic" ]; then say "Removed $lic"; fi
    fi
  fi
  launcher="$LAUNCHER_DIR/$LAUNCHER_NAME"
  if [ -n "$LAUNCHER_DIR" ] && { [ -e "$launcher" ] || [ -L "$launcher" ]; }; then
    if has_marker "$launcher"; then
      if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $launcher"; else rm -f "$launcher"; say "Removed $launcher"; fi
    else
      warn "keeping $launcher: it was not created by this installer"
    fi
  fi
  if [ "$ST_PRESENT" -eq 1 ]; then
    if [ -n "$ST_ALIAS" ]; then
      if [ "$DRY_RUN" -eq 0 ]; then remove_alias "$ST_ALIAS"
      elif alias_is_ours "$ST_ALIAS"; then say "Would remove $BIN_DIR/$ST_ALIAS"; fi
    fi
    remove_path_line
    state="$BIN_DIR/$STATE_NAME"
    if [ "$DRY_RUN" -eq 1 ]; then say "Would remove $state"
    elif [ "$MODIFY_PATH" -eq 1 ]; then rm -f "$state"; say "Removed $state"; fi
  else
    say "note: no install state in $BIN_DIR: a shortcut or PATH entry for it was not created by this installer, or must be removed by hand."
  fi
  say "Your configuration, data, history, cache and ROMs were not touched."
}

# --- main -----------------------------------------------------------------------------------------------------------

main() {
parse_args "$@"
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
  dry_gui=none # eligible, none (the GUI would not be installed) or unknown (eligibility not checked)
  if [ "$GUI_ON" -eq 1 ]; then
    say "GUI archive: $GUI_ARCHIVE (verified)"
    if [ -d "$BIN_DIR" ]; then
      stage_and_probe
      drop_stage
      case $GUI_VERDICT in
        eligible) dry_gui=eligible; say "GUI eligibility: eligible ($PROBE_MSG); would install $BIN_DIR/mothdex-gui and $BIN_DIR/$LICENSES_NAME" ;;
        not-eligible) say "GUI eligibility: not eligible: ${PROBE_MSG:-no reason given}; the GUI would not be installed"; remove_gui "not eligible" ;;
        *) say "GUI eligibility: the probe could not run (exit $PROBE_RC): ${PROBE_MSG:-no output}"; remove_gui "probe error" ;;
      esac
    else
      dry_gui=unknown
      say "GUI eligibility: not checked ($BIN_DIR does not exist yet)"
    fi
  elif [ "$GUI_OPT" = 0 ]; then
    remove_gui "--no-gui"
  else
    remove_gui "this release has no GUI archive"
  fi
  case $dry_gui in
    eligible) if [ "$LAUNCHER_OPT" != 0 ] && [ -n "$LAUNCHER_DIR" ]; then say "Would write: $LAUNCHER_DIR/$LAUNCHER_NAME"; fi ;;
    unknown) if [ "$LAUNCHER_OPT" != 0 ] && [ -n "$LAUNCHER_DIR" ]; then say "Launcher: would be written only if the GUI turns out to be eligible"; fi ;;
    *) remove_launcher "the GUI would not be installed" ;;
  esac
  read_state
  choose_alias
  if [ -n "$ALIAS" ]; then say "Would create the shortcut $BIN_DIR/$ALIAS"; fi
  exit 0
fi

install_binary
say "Installed $BIN_DIR/mothdex $VERSION ($SIGNATURE)"
install_gui
if [ "$GUI_INSTALLED" -ne 1 ]; then
  if [ "$GUI_OPT" = 0 ]; then why="--no-gui"; elif [ "$GUI_ON" -eq 1 ]; then why="not installed: see above"; else why="this release has no GUI archive"; fi
  remove_gui "$why"
fi
install_licenses
decide_launcher
if [ "$GUI_INSTALLED" -eq 1 ]; then install_launcher; else remove_launcher "the GUI is not installed: a Port needs it"; fi
refresh_frontend
# TM-11: PATH and the install state change only after the binaries are in place and --version passed.
if ! "$BIN_DIR/mothdex" --version; then
  warn "the installed binary did not run; check that this is the right architecture. PATH was not changed."
elif [ "$FAILED" -ne 0 ]; then
  detect_shell
  warn "a step failed (see above): PATH was not changed"
  print_path_block
else
  read_state
  update_alias
  update_path
  write_state
  final_message
fi
exit "$FAILED"
}

main "$@" </dev/null
}
