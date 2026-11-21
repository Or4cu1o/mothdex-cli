#!/bin/sh
# Installer tests. Run inside a throwaway, network-less container as root, plus a second pass as uid 1000 with
# NONROOT=1 (see run.sh and nonroot.sh): simulated Batocera, EmuELEC,
# Recalbox, Lakka and generic file systems, a local fake release, and shims for curl, wget, uname, cosign and gh.
# Covers T-16 cases 51 (launcher path injection) and 52 (launcher permissions).
set -u

REPO=${REPO:-/repo}
SHIMS=$REPO/tests/installer/shims
OPTIONAL=$REPO/tests/installer/optional
FAKE_RELEASE=${FAKE_RELEASE:-/fake-release}
export FAKE_RELEASE
export FAKE_LOG=/tmp/fake.log
BASE_PATH=$PATH
OUT=/tmp/install.out
PASS=0
FAIL=0
CASE=""
RC=0

check() { # description, then a command
  desc=$1
  shift
  if "$@"; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    printf 'FAIL: %s: %s\n' "$CASE" "$desc"
    sed 's/^/    | /' "$OUT" 2>/dev/null | tail -n 8
  fi
}

rc_is() { [ "$RC" -eq "$1" ]; }
rc_not0() { [ "$RC" -ne 0 ]; }
absent() { [ ! -e "$1" ] && [ ! -L "$1" ]; }
mode_is() { [ "$(stat -c %a "$1")" = "$2" ]; }
owned_by_me() { [ "$(stat -c %u "$1")" = "$(id -u)" ]; }
out_has() { grep -qF -- "$1" "$OUT"; }
file_has() { grep -qF -- "$2" "$1"; }
log_lacks() { ! grep -qF -- "$1" "$FAKE_LOG"; }
runs_ok() { "$@" >/dev/null 2>&1; }
no_downloads() { [ ! -s "$FAKE_LOG" ]; }
text_is() { [ "$1" = "$2" ]; }

# --- fake release -----------------------------------------------------------------------------------------------

TARGETS="linux_amd64 linux_386 linux_arm64 linux_armv7 linux_armv6 darwin_amd64 darwin_arm64"

clean_build() { rm -rf /tmp/build && mkdir -p /tmp/build; }

# make_tag <tag>: archives whose fake `mothdex` prints its version, target and arguments, plus checksums.txt.
make_tag() {
  tag=$1
  ver=${tag#v}
  d=$FAKE_RELEASE/$tag
  mkdir -p "$d"
  : >"$d/checksums.txt"
  for t in $TARGETS; do
    clean_build
    printf '#!/bin/sh\necho "mothdex %s %s $*"\n' "$ver" "$t" >/tmp/build/mothdex
    chmod 0755 /tmp/build/mothdex
    echo license >/tmp/build/LICENSE
    name=mothdex_${ver}_$t.tar.gz
    (cd /tmp/build && tar -czf "$d/$name" mothdex LICENSE)
    printf '%s  %s\n' "$(sha256sum "$d/$name" | cut -d' ' -f1)" "$name" >>"$d/checksums.txt"
  done
  echo '{"fake":"bundle"}' >"$d/checksums.txt.sigstore.json"
}

# replace_amd64 <tag>: re-pack the linux_amd64 archive of <tag> from /tmp/build and fix its checksums.txt line.
replace_amd64() {
  tag_for_pack=$1
  name=mothdex_${1#v}_linux_amd64.tar.gz
  (cd /tmp/build && set -- * && tar -czf "$FAKE_RELEASE/${tag_for_pack}/$name" "$@")
  sum=$(sha256sum "$FAKE_RELEASE/$1/$name" | cut -d' ' -f1)
  sed "s/.*$name/$sum  $name/" "$FAKE_RELEASE/$1/checksums.txt" >/tmp/c
  mv /tmp/c "$FAKE_RELEASE/$1/checksums.txt"
}

setup_release() {
  rm -rf "$FAKE_RELEASE"
  for tag in v1.2.3 v1.1.0 v1.0.9 v1.0.8 v1.0.7 v2.0.0-rc.1; do make_tag "$tag"; done
  mkdir -p "$FAKE_RELEASE/latest"
  cp "$FAKE_RELEASE"/v1.2.3/* "$FAKE_RELEASE/latest/"
  # v1.1.0: archive tampered after checksums.txt was written
  echo tamper >>"$FAKE_RELEASE/v1.1.0/mothdex_1.1.0_linux_amd64.tar.gz"
  # v1.0.9: checksums.txt has no line for linux_amd64
  grep -v linux_amd64 "$FAKE_RELEASE/v1.0.9/checksums.txt" >/tmp/c && mv /tmp/c "$FAKE_RELEASE/v1.0.9/checksums.txt"
  # v1.0.8: valid checksum, but `mothdex` is a symlink to an executable (only the regular-file guard can stop it)
  clean_build
  ln -s /bin/sh /tmp/build/mothdex
  replace_amd64 v1.0.8
  # v1.0.7: valid checksum, but no `mothdex` member
  clean_build
  echo x >/tmp/build/other
  replace_amd64 v1.0.7
}

# --- simulated systems ------------------------------------------------------------------------------------------

reset() {
  CASE=$1
  rm -rf /userdata /storage /recalbox /usr/share/batocera /etc/os-release /home/u "/home/a b" /opt/* /run/media \
    /tmp/pwn /tmp/victim /tmp/nocurl /tmp/ghonly "$FAKE_LOG" "$FAKE_LOG.cosign" "$FAKE_LOG.gh" 2>/dev/null
  : >"$FAKE_LOG"
  mkdir -p /home/u /opt
  HOME=/home/u
  FAKE_UNAME_S=Linux
  FAKE_UNAME_M=x86_64
  PATH=$SHIMS:$BASE_PATH
  export HOME FAKE_UNAME_S FAKE_UNAME_M PATH
  unset COSIGN_RC GH_RC GH_AUTH_RC FAKE_FSTYPE
}

fs_batocera() {
  mkdir -p /usr/share/batocera /userdata/roms/ports /userdata/system
  echo 1 >/usr/share/batocera/batocera.version
  HOME=/userdata/system
  export HOME
}

fs_emuelec() {
  mkdir -p /storage/roms/ports_scripts
  printf 'NAME="EmuELEC"\nID=emuelec\n' >/etc/os-release
  HOME=/storage
  export HOME
}

fs_recalbox() {
  mkdir -p /recalbox/share/roms /recalbox/share/system
  HOME=/recalbox/share/system
  export HOME
}

fs_lakka() {
  mkdir -p /storage/roms
  printf 'NAME="Lakka"\nID=lakka\n' >/etc/os-release
  HOME=/storage
  export HOME
}

# The launcher is opt-in (D22): tests ask for it unless INST_LAUNCHER=0.
INST_LAUNCHER=1
inst() {
  if [ "$INST_LAUNCHER" -eq 1 ]; then
    sh "$REPO/install.sh" --launcher "$@" >"$OUT" 2>&1
  else
    sh "$REPO/install.sh" "$@" >"$OUT" 2>&1
  fi
  RC=$?
}

# --- cases ------------------------------------------------------------------------------------------------------

setup_release

if [ "${NONROOT:-0}" = 1 ]; then
  . "$REPO/tests/installer/nonroot.sh"
  printf '%s passed, %s failed (non-root)\n' "$PASS" "$FAIL"
  [ "$FAIL" -eq 0 ]
  exit
fi

reset batocera-install
fs_batocera
inst --version v1.2.3
check "exit 0" rc_is 0
check "binary is 0755" mode_is /userdata/system/bin/mothdex 755
check "bin_dir is 0755" mode_is /userdata/system/bin 755
check "binary runs" runs_ok /userdata/system/bin/mothdex --version
check "launcher exists" test -f /userdata/roms/ports/Mothdex.sh
check "launcher is 0755" mode_is /userdata/roms/ports/Mothdex.sh 755
check "launcher owned by installer" owned_by_me /userdata/roms/ports/Mothdex.sh
check "launcher marker" file_has /userdata/roms/ports/Mothdex.sh '# mothdex-launcher v1'
check "launcher template" file_has /userdata/roms/ports/Mothdex.sh 'exec "/userdata/system/bin/mothdex" tui "$@"'
check "launcher passes sh -n" sh -n /userdata/roms/ports/Mothdex.sh
check "launcher forwards arguments" text_is "$(/userdata/roms/ports/Mothdex.sh 'a b')" "mothdex 1.2.3 linux_amd64 tui a b"
check "only GitHub release URLs used" log_lacks BLOCKED
check "no leftovers in temp dir" test -z "$(ls -d /tmp/mothdex-install.* 2>/dev/null)"
check "missing signature tools are reported" out_has "SHA-256 only"

reset emuelec-install
fs_emuelec
inst --version v1.2.3
check "exit 0" rc_is 0
check "profile emuelec" out_has "profile: emuelec"
check "binary in /storage/.local/bin" mode_is /storage/.local/bin/mothdex 755
check "launcher in ports_scripts" test -f /storage/roms/ports_scripts/Mothdex.sh
check "launcher not in roms/ports" absent /storage/roms/ports/Mothdex.sh

reset recalbox-no-launcher
fs_recalbox
inst --version v1.2.3
check "recalbox profile" out_has "profile: recalbox"
check "recalbox binary" test -x /recalbox/share/system/bin/mothdex
check "recalbox has no launcher" test -z "$(find /recalbox -name Mothdex.sh)"
reset lakka-no-launcher
fs_lakka
inst --version v1.2.3
check "lakka profile" out_has "profile: lakka"
check "lakka binary" test -x /storage/.local/bin/mothdex
check "lakka has no launcher" test -z "$(find /storage -name Mothdex.sh)"

reset generic-install
inst --version v1.2.3
check "generic profile" out_has "profile: generic"
check "binary in HOME/.local/bin" mode_is /home/u/.local/bin/mothdex 755
check "PATH hint printed" out_has "Add /home/u/.local/bin to PATH"

reset detection-falls-back-to-generic
mkdir -p /usr/share/batocera
echo 1 >/usr/share/batocera/batocera.version
inst --dry-run --version v1.2.3
check "batocera marker without roms dir is generic" out_has "profile: generic"
reset emudeck-and-retrodeck-are-ambiguous
mkdir -p /home/u/Emulation/roms /home/u/retrodeck/roms
inst --dry-run --version v1.2.3
check "ambiguous falls back to generic" out_has "profile: generic"
check "ambiguity warned" out_has "both EmuDeck and RetroDECK"
reset emudeck-alone
mkdir -p /home/u/Emulation/roms
inst --dry-run --version v1.2.3
check "emudeck detected" out_has "profile: emudeck"

reset latest
inst
check "latest resolves from the latest release" out_has "mothdex_1.2.3_linux_amd64.tar.gz"
check "latest installs 1.2.3" text_is "$(/home/u/.local/bin/mothdex)" "mothdex 1.2.3 linux_amd64 "
reset pin-prerelease
inst --version 2.0.0-rc.1
check "pre-release only by pin" text_is "$(/home/u/.local/bin/mothdex)" "mothdex 2.0.0-rc.1 linux_amd64 "
# shellcheck disable=SC2016  # the payloads are literal on purpose
for bad in '1.2.3; touch /tmp/pwn' '../../x' 'v1.2.3/../v2' '1.2' '$(touch /tmp/pwn)' 'v1.2.3 '; do
  reset bad-version
  inst --version "$bad"
  check "refuses version '$bad'" rc_not0
  check "no command ran for '$bad'" absent /tmp/pwn
  check "nothing installed for '$bad'" absent /home/u/.local/bin/mothdex
done

reset checksum-mismatch
inst --version v1.1.0
check "mismatch aborts" rc_not0
check "mismatch message" out_has "SHA-256 mismatch"
check "nothing installed" absent /home/u/.local/bin/mothdex
reset checksum-line-missing
inst --version v1.0.9
check "missing line aborts" rc_not0
check "nothing installed" absent /home/u/.local/bin/mothdex
reset archive-symlink-member
inst --version v1.0.8
check "symlink member refused" rc_not0
check "refused by the regular-file guard" out_has "not a regular file"
check "nothing installed" absent /home/u/.local/bin/mothdex
reset archive-without-binary
inst --version v1.0.7
check "archive without binary refused" rc_not0
check "nothing installed" absent /home/u/.local/bin/mothdex
reset unknown-release
inst --version v9.9.9
check "unknown release aborts" rc_not0

# uname mapping (release-artifacts.md section 3)
for pair in 'Linux x86_64 linux_amd64' 'Linux i686 linux_386' 'Linux i386 linux_386' 'Linux aarch64 linux_arm64' \
  'Linux armv7l linux_armv7' 'Linux armv8l linux_armv7' 'Linux armv6l linux_armv6' 'Darwin x86_64 darwin_amd64' \
  'Darwin arm64 darwin_arm64'; do
  # shellcheck disable=SC2086  # intentional word splitting of the "os machine target" triple
  set -- $pair
  reset "uname-$1-$2"
  FAKE_UNAME_S=$1 FAKE_UNAME_M=$2 inst --dry-run --version v1.2.3
  check "$1/$2 -> $3" out_has "Verified mothdex_1.2.3_$3.tar.gz"
done
for pair in 'FreeBSD amd64' 'Linux riscv64' 'Linux ppc64le' 'Darwin i386'; do
  # shellcheck disable=SC2086  # intentional word splitting of the "os machine target" triple
  set -- $pair
  reset "uname-unsupported"
  FAKE_UNAME_S=$1 FAKE_UNAME_M=$2 inst --dry-run --version v1.2.3
  check "$1/$2 unsupported" out_has "unsupported system: $1/$2"
  check "$1/$2 downloads nothing" no_downloads
done

# T-16 case 51: launcher path injection
# shellcheck disable=SC2016  # the payloads are literal on purpose
for bad in '/opt/a b' '/opt/$(touch /tmp/pwn)' '/opt/`touch /tmp/pwn`' '/opt/x;touch /tmp/pwn' 'relative/dir' '/opt/../etc' \
  '/opt/x"y' "/opt/x'y"; do
  reset case51
  fs_batocera
  inst --bin-dir "$bad" --version v1.2.3
  check "refuses bin_dir '$bad'" rc_not0
  check "no command ran for '$bad'" absent /tmp/pwn
  check "no launcher for '$bad'" absent /userdata/roms/ports/Mothdex.sh
  check "no download for '$bad'" no_downloads
done
reset case51-home-with-space
mkdir -p "/home/a b"
HOME="/home/a b" inst --version v1.2.3
check "HOME with a space refused" rc_not0
reset case51-safe-odd-path
fs_batocera
inst --bin-dir /opt/mo+th-dex.v1/bin --version v1.2.3
check "safe unusual chars accepted" rc_is 0
check "launcher passes sh -n" sh -n /userdata/roms/ports/Mothdex.sh
check "launcher runs the literal path" text_is "$(/userdata/roms/ports/Mothdex.sh x)" "mothdex 1.2.3 linux_amd64 tui x"

# T-16 case 52: launcher permissions and re-install
reset case52-world-writable-bin-dir
fs_batocera
mkdir -p /userdata/system/bin
chmod 0777 /userdata/system/bin
inst --version v1.2.3
check "world-writable bin_dir refused" rc_not0
check "no binary" absent /userdata/system/bin/mothdex
reset case52-sticky-world-writable-bin-dir
fs_batocera
mkdir -p /userdata/system/bin
chmod 1777 /userdata/system/bin
inst --version v1.2.3
check "mode 1777 bin_dir refused" rc_not0
check "no binary (1777)" absent /userdata/system/bin/mothdex
reset case52-foreign-owner-bin-dir
fs_batocera
mkdir -p /userdata/system/bin
chown 1234 /userdata/system/bin
inst --version v1.2.3
check "bin_dir owned by another user refused" rc_not0
check "no binary (foreign owner)" absent /userdata/system/bin/mothdex
reset case52-mode-0702-bin-dir
fs_batocera
mkdir -p /userdata/system/bin
chmod 0702 /userdata/system/bin
inst --version v1.2.3
check "mode 0702 (other write only) refused" rc_not0
reset case52-mode-0775-bin-dir
fs_batocera
mkdir -p /userdata/system/bin
chmod 0775 /userdata/system/bin
inst --version v1.2.3
check "mode 0775 (group write) accepted" rc_is 0
reset case52-symlink-to-marked-file
fs_batocera
printf '#!/bin/sh\n# mothdex-launcher v1\necho precious\n' >/tmp/victim
ln -s /tmp/victim /userdata/roms/ports/Mothdex.sh
inst --version v1.2.3
check "symlink to a marked file is not replaced" test -L /userdata/roms/ports/Mothdex.sh
check "marked target untouched" file_has /tmp/victim "echo precious"
reset fat-world-writable-batocera-root
fs_batocera
mkdir -p /userdata/system/bin
chmod 0777 /userdata/system/bin /userdata/roms/ports
FAKE_FSTYPE=exfat inst --version v1.2.3
check "exFAT bin_dir as root on batocera installs" rc_is 0
check "binary installed" test -x /userdata/system/bin/mothdex
check "warns about missing file permissions" out_has "has no file permissions"
check "launcher dir covered by the same rule" test -x /userdata/roms/ports/Mothdex.sh
for fst in vfat msdos ntfs ntfs3 fuseblk; do
  reset "fat-$fst"
  fs_batocera
  mkdir -p /userdata/system/bin
  chmod 0777 /userdata/system/bin
  FAKE_FSTYPE=$fst inst --version v1.2.3
  check "$fst accepted" rc_is 0
done
reset fat-recalbox
fs_recalbox
mkdir -p /recalbox/share/system/bin
chmod 0777 /recalbox/share/system/bin
FAKE_FSTYPE=exfat inst --version v1.2.3
check "recalbox on exFAT installs" rc_is 0
reset fat-generic-refused
fs_batocera
mkdir -p /userdata/system/bin
chmod 0777 /userdata/system/bin
FAKE_FSTYPE=exfat inst --profile generic --bin-dir /userdata/system/bin --version v1.2.3
check "same dir on generic is refused" rc_not0
check "no binary (generic)" absent /userdata/system/bin/mothdex
reset fat-other-bin-dir-refused
fs_batocera
mkdir -p /opt/bin
chmod 0777 /opt/bin
FAKE_FSTYPE=exfat inst --bin-dir /opt/bin --version v1.2.3
check "a bin_dir outside the profile tree is refused" rc_not0
reset fat-unknown-fstype
fs_batocera
mkdir -p /userdata/system/bin
chmod 0777 /userdata/system/bin
FAKE_FSTYPE=ext2/ext3 inst --version v1.2.3
check "unknown filesystem is refused" rc_not0
FAKE_FSTYPE=FAIL inst --version v1.2.3
check "failing detection is refused" rc_not0
check "no binary (unknown fs)" absent /userdata/system/bin/mothdex
reset fat-foreign-owner
fs_batocera
mkdir -p /userdata/system/bin
chmod 0777 /userdata/system/bin
chown 1234 /userdata/system/bin
FAKE_FSTYPE=exfat inst --version v1.2.3
check "FAT exception still requires owner root" rc_not0
reset case52-world-writable-launcher-dir
fs_batocera
chmod 0777 /userdata/roms/ports
inst --version v1.2.3
check "binary still installed" test -x /userdata/system/bin/mothdex
check "launcher skipped" absent /userdata/roms/ports/Mothdex.sh
check "skip is explained" out_has "launcher skipped"
reset case52-keeps-user-launcher
fs_batocera
printf '#!/bin/sh\necho mine\n' >/userdata/roms/ports/Mothdex.sh
inst --version v1.2.3
check "user launcher kept" text_is "$(cat /userdata/roms/ports/Mothdex.sh)" "$(printf '#!/bin/sh\necho mine')"
check "kept is reported" out_has "keeping existing"
reset case52-overwrites-own-launcher
fs_batocera
inst --version v1.2.3
printf '#!/bin/sh\n# mothdex-launcher v1\necho stale\n' >/userdata/roms/ports/Mothdex.sh
inst --version v1.2.3
check "marked launcher refreshed" file_has /userdata/roms/ports/Mothdex.sh 'exec "/userdata/system/bin/mothdex" tui'
check "still 0755" mode_is /userdata/roms/ports/Mothdex.sh 755
reset case52-symlink-launcher
fs_batocera
echo precious >/tmp/victim
ln -s /tmp/victim /userdata/roms/ports/Mothdex.sh
inst --version v1.2.3
check "symlink target untouched" text_is "$(cat /tmp/victim)" precious
check "symlink left alone" test -L /userdata/roms/ports/Mothdex.sh

reset dry-run
fs_batocera
inst --dry-run --version v1.2.3
check "exit 0" rc_is 0
check "says dry run" out_has "Dry run: nothing was installed"
check "no bin_dir" absent /userdata/system/bin
check "no launcher" absent /userdata/roms/ports/Mothdex.sh

reset uninstall
fs_batocera
inst --version v1.2.3
: >"$FAKE_LOG"
inst --uninstall
check "uninstall ok" rc_is 0
check "binary removed" absent /userdata/system/bin/mothdex
check "launcher removed" absent /userdata/roms/ports/Mothdex.sh
check "uninstall needs no network" no_downloads
reset uninstall-keeps-foreign-launcher
fs_batocera
printf '#!/bin/sh\necho mine\n' >/userdata/roms/ports/Mothdex.sh
inst --uninstall
check "uninstall ok" rc_is 0
check "foreign launcher kept" test -f /userdata/roms/ports/Mothdex.sh

# optional verification tools
reset cosign-present
PATH=$SHIMS:$OPTIONAL:$BASE_PATH inst --version v1.2.3
check "install ok" rc_is 0
check "cosign verify-blob used" file_has "$FAKE_LOG.cosign" "verify-blob"
check "cosign identity pinned to the tag" file_has "$FAKE_LOG.cosign" \
  "--certificate-identity https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/v1.2.3"
check "cosign issuer pinned" file_has "$FAKE_LOG.cosign" \
  "--certificate-oidc-issuer https://token.actions.githubusercontent.com"
check "bundle downloaded" file_has "$FAKE_LOG" "checksums.txt.sigstore.json"
check "reported" out_has "cosign signature verified"
reset cosign-fails
COSIGN_RC=1 PATH=$SHIMS:$OPTIONAL:$BASE_PATH inst --version v1.2.3
check "failed signature aborts" rc_not0
check "nothing installed" absent /home/u/.local/bin/mothdex

reset gh-only
mkdir -p /tmp/ghonly
cp "$OPTIONAL/gh" /tmp/ghonly/gh
PATH=$SHIMS:/tmp/ghonly:$BASE_PATH inst --version v1.2.3
check "gh attestation pinned to repo, workflow and tag" file_has "$FAKE_LOG.gh" \
  "--repo Or4cu1o/mothdex-cli --signer-workflow Or4cu1o/mothdex-cli/.github/workflows/release.yml --source-ref refs/tags/v1.2.3"
reset gh-fails
mkdir -p /tmp/ghonly
cp "$OPTIONAL/gh" /tmp/ghonly/gh
GH_RC=1 PATH=$SHIMS:/tmp/ghonly:$BASE_PATH inst --version v1.2.3
check "failed attestation aborts" rc_not0
check "nothing installed" absent /home/u/.local/bin/mothdex
reset gh-not-logged-in
mkdir -p /tmp/ghonly
cp "$OPTIONAL/gh" /tmp/ghonly/gh
GH_AUTH_RC=1 PATH=$SHIMS:/tmp/ghonly:$BASE_PATH inst --version v1.2.3
check "unauthenticated gh is skipped, checksum still enforced" rc_is 0
check "attestation not attempted" absent "$FAKE_LOG.gh"

reset wget-fallback
mkdir -p /tmp/nocurl
cp "$SHIMS/wget" "$SHIMS/fake-fetch" "$SHIMS/uname" /tmp/nocurl/
cp "$OPTIONAL/cosign" /tmp/nocurl/cosign
PATH=/tmp/nocurl:$BASE_PATH inst --version v1.2.3
check "wget fallback works with a signature tool" rc_is 0
check "installed through wget" test -x /home/u/.local/bin/mothdex
check "signature was verified" test -s "$FAKE_LOG.cosign"
reset wget-only-without-signature-tool
mkdir -p /tmp/nocurl
cp "$SHIMS/wget" "$SHIMS/fake-fetch" "$SHIMS/uname" /tmp/nocurl/
PATH=/tmp/nocurl:$BASE_PATH inst --version v1.2.3
check "wget alone is refused (it may follow https to http)" rc_not0
check "explains the redirect risk" out_has "https to http redirect"
check "nothing installed (wget alone)" absent /home/u/.local/bin/mothdex
check "nothing downloaded (wget alone)" no_downloads
reset wget-with-failing-signature
mkdir -p /tmp/nocurl
cp "$SHIMS/wget" "$SHIMS/fake-fetch" "$SHIMS/uname" /tmp/nocurl/
cp "$OPTIONAL/cosign" /tmp/nocurl/cosign
COSIGN_RC=1 PATH=/tmp/nocurl:$BASE_PATH inst --version v1.2.3
check "failing signature aborts the wget path" rc_not0
check "nothing installed (wget, bad signature)" absent /home/u/.local/bin/mothdex

reset wget-busybox-refused
mkdir -p /tmp/nocurl2
cp "$OPTIONAL/wget-busybox" /tmp/nocurl2/wget
cp "$SHIMS/uname" /tmp/nocurl2/
PATH=/tmp/nocurl2:$BASE_PATH inst --version v1.2.3
check "busybox wget refused" rc_not0
check "explains why" out_has "not GNU Wget"
check "nothing installed (busybox wget)" absent /home/u/.local/bin/mothdex
check "busybox wget never downloaded" absent "$FAKE_LOG.busybox"

reset symlink-bin-dir
mkdir -p /tmp/realbin
ln -s /tmp/realbin /tmp/linkbin
inst --version v1.2.3 --bin-dir /tmp/linkbin
check "symlink bin_dir refused" rc_not0
check "says symbolic link" out_has "symbolic link"

reset hyphen-in-prerelease
make_tag v2.1.0-rc-1
inst --version 2.1.0-rc-1
check "hyphen inside a pre-release identifier is accepted" rc_is 0

reset launcher-opt-in
fs_batocera
INST_LAUNCHER=0
inst --version v1.2.3
check "binary installed" test -x /userdata/system/bin/mothdex
check "no launcher without --launcher" absent /userdata/roms/ports/Mothdex.sh
inst --launcher --version v1.2.3
check "launcher with --launcher" test -x /userdata/roms/ports/Mothdex.sh
inst --uninstall
check "uninstall removes the opt-in launcher" absent /userdata/roms/ports/Mothdex.sh
INST_LAUNCHER=1

reset help-and-bad-options
inst --help
check "help exits 0" rc_is 0
inst --frobnicate
check "unknown option fails" rc_not0
inst --profile retrobat
check "retrobat points to install.ps1" out_has "install.ps1"
inst --profile nonsense
check "unknown profile fails" rc_not0

printf '%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
