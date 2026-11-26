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
only_dash_q() { [ -s "$1" ] && ! grep -vqx -- '-q' "$1"; }
log_lacks() { ! grep -qF -- "$1" "$FAKE_LOG"; }
runs_ok() { "$@" >/dev/null 2>&1; }
no_downloads() { [ ! -s "$FAKE_LOG" ]; }
text_is() { [ "$1" = "$2" ]; }
file_lacks() { ! grep -qF -- "$2" "$1"; }
out_lacks() { ! grep -qF -- "$1" "$OUT"; }
absent_all() { for f in "$@"; do absent "$f" || return 1; done; }
no_stage() { [ -z "$(find "$1" -maxdepth 1 -name '.mothdex*' ! -name .mothdex-install.json 2>/dev/null)" ]; }
env_scrubbed() { [ -s /tmp/probe.env ] && ! grep -Eq '^(LD_|SDL_|MOTHDEX_|HOME=)' /tmp/probe.env; }
probe_ran_in_bin() { case $(cat /tmp/probe.where 2>/dev/null) in /userdata/system/bin/.mothdex-gui.new.*) return 0 ;; esac; return 1; }
out_has_no_esc() { ! grep -q "$(printf '')" "$OUT"; }
out_longest_zero_run_le() { ! grep -Eq "0{$(( $1 + 1 ))}" "$OUT"; }

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

# --- fake GUI archives (ports.md section 5.2) ------------------------------------------------------------------

# shellcheck disable=SC2016  # the stub scripts are written literally
# gui_stub <variant>: the fake mothdex-gui. Its --probe records the environment it sees and the path it ran from.
gui_stub() {
  case $1 in
    notelig)
      printf '#!/bin/sh\nif [ "${1:-}" = "--probe" ]; then echo "libSDL2-2.0.so.0 not found" >&2; exit 8; fi\necho "gui $*"\n'
      ;;
    exit1) # "SDL missing" up to 0.1.x, an internal error from 0.2.0 on
      printf '#!/bin/sh\nif [ "${1:-}" = "--probe" ]; then echo "libSDL2-2.0.so.0 not found" >&2; exit 1; fi\necho "gui $*"\n'
      ;;
    execfail)
      printf '#!/nonexistent/interpreter\n'
      ;;
    crash)
      printf '#!/bin/sh\nif [ "${1:-}" = "--probe" ]; then echo "panic: boom" >&2; exit 2; fi\n'
      ;;
    longmsg)
      printf '#!/bin/sh\nif [ "${1:-}" = "--probe" ]; then printf "\\033[31m%%0300d\\n" 0 >&2; exit 8; fi\n'
      ;;
    *)
      printf '#!/bin/sh\nif [ "${1:-}" = "--probe" ]; then\n  env >/tmp/probe.env\n  echo "$0" >/tmp/probe.where\n  echo "sdl 2.32.10"\n  exit 0\nfi\necho "gui $*"\n'
      ;;
  esac
}

# make_gui_tag <tag> <variant>: a release like make_tag plus mothdex-gui_ archives for linux amd64 and armv7.
# Variants: ok notelig execfail crash longmsg (probe behaviour), unknownnotice (a safe name that is not a shipped notice), symlink (mothdex-gui is a link), extra (an unexpected
# member), noticelink (a THIRD_PARTY member is a link), nomember (no mothdex-gui), missing (listed, not downloadable),
# tamper (checksum mismatch), dotdot and absolute (GNU tar only: names a tar writer should never produce).
make_gui_tag() {
  tag=$1
  variant=$2
  ver=${tag#v}
  make_tag "$tag"
  d=$FAKE_RELEASE/$tag
  for t in linux_amd64 linux_armv7; do
    clean_build
    mkdir -p /tmp/build/THIRD_PARTY
    gui_stub "$variant" >/tmp/build/mothdex-gui
    chmod 0755 /tmp/build/mothdex-gui
    echo license >/tmp/build/LICENSE
    echo noto >/tmp/build/THIRD_PARTY/NotoSans-OFL-1.1.txt
    echo handjet >/tmp/build/THIRD_PARTY/Handjet-OFL-1.1.txt
    echo zlib >/tmp/build/THIRD_PARTY/SDL_GameControllerDB-zlib.txt
    echo readme >/tmp/build/THIRD_PARTY/README.md
    echo gomods >/tmp/build/THIRD_PARTY/go-modules-mothdex.txt
    echo gomodsgui >/tmp/build/THIRD_PARTY/go-modules-mothdex-gui.txt
    members="mothdex-gui LICENSE THIRD_PARTY"
    name=mothdex-gui_${ver}_$t.tar.gz
    case $variant in
      symlink)
        rm -f /tmp/build/mothdex-gui
        ln -s /bin/sh /tmp/build/mothdex-gui
        ;;
      extra)
        echo x >/tmp/build/evil
        members="$members evil"
        ;;
      noticelink)
        ln -s /etc/passwd /tmp/build/THIRD_PARTY/passwd
        ;;
      nomember)
        members="LICENSE THIRD_PARTY"
        ;;
      hardlink)
        # a notice name that is a hard link to another member (busybox tar lists it like a file)
        rm -f /tmp/build/THIRD_PARTY/SDL_GameControllerDB-zlib.txt
        ln /tmp/build/THIRD_PARTY/NotoSans-OFL-1.1.txt /tmp/build/THIRD_PARTY/SDL_GameControllerDB-zlib.txt
        ;;
      unknownnotice) echo x >/tmp/build/THIRD_PARTY/extra.txt ;;
      badspace) echo x >"/tmp/build/THIRD_PARTY/a b" ;;
      badstar) echo x >"/tmp/build/THIRD_PARTY/a*b" ;;
    esac
    case $variant in
      dotdot) (cd /tmp/build && tar -czPf "$d/$name" mothdex-gui --transform 's,^mothdex-gui$,../mothdex-gui,') ;;
      absolute) (cd /tmp/build && tar -czPf "$d/$name" mothdex-gui --transform 's,^mothdex-gui$,/tmp/pwn-abs,') ;;
      *)
        # shellcheck disable=SC2086  # $members is a list of plain names
        (cd /tmp/build && tar -czf "$d/$name" $members)
        ;;
    esac
    printf '%s  %s\n' "$(sha256sum "$d/$name" | cut -d' ' -f1)" "$name" >>"$d/checksums.txt"
    case $variant in
      missing) rm -f "$d/$name" ;;
      tamper) echo tamper >>"$d/$name" ;;
    esac
  done
}

gnu_tar() { tar --version 2>/dev/null | grep -q 'GNU tar'; }

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
  # releases that ship mothdex-gui_ archives (Ports)
  make_gui_tag v1.3.0 ok
  make_gui_tag v1.3.1 notelig
  make_gui_tag v1.3.2 execfail
  make_gui_tag v1.3.3 crash
  make_gui_tag v1.3.4 symlink
  make_gui_tag v1.3.5 extra
  make_gui_tag v1.3.6 missing
  make_gui_tag v1.3.7 tamper
  make_gui_tag v1.3.8 noticelink
  make_gui_tag v1.3.9 nomember
  make_gui_tag v1.4.0 longmsg
  if gnu_tar; then
    make_gui_tag v1.4.1 dotdot
    make_gui_tag v1.4.2 absolute
  fi
  make_gui_tag v1.4.3 hardlink
  make_gui_tag v1.4.4 badspace
  make_gui_tag v1.4.5 badstar
  make_gui_tag v1.4.6 unknownnotice
  # the probe's exit 1: not eligible for releases up to 0.1.x, a probe error from 0.2.0 on
  make_gui_tag v0.1.5 exit1
  make_gui_tag v1.4.7 exit1
  # v1.0.6: valid release whose binary reads its stdin; v1.0.5: whose binary fails --version
  make_tag v1.0.6
  clean_build
  printf '#!/bin/sh\ncat >>/tmp/stdin.seen\necho "mothdex 1.0.6"\n' >/tmp/build/mothdex
  chmod 0755 /tmp/build/mothdex
  replace_amd64 v1.0.6
  make_tag v1.0.5
  clean_build
  printf '#!/bin/sh\nexit 1\n' >/tmp/build/mothdex
  chmod 0755 /tmp/build/mothdex
  replace_amd64 v1.0.5
}

# --- simulated systems ------------------------------------------------------------------------------------------

reset() {
  CASE=$1
  rm -rf /userdata /storage /recalbox /usr/share/batocera /etc/os-release /home/u "/home/a b" /opt/* /run/media \
    /tmp/pwn /tmp/pwn-abs /tmp/victim /tmp/victimdir /tmp/nocurl /tmp/nocurl2 /tmp/ghonly /tmp/probe.env /tmp/probe.where "$FAKE_LOG.refresh" "$FAKE_LOG.timeout" "$FAKE_LOG.curlfirst" "$FAKE_LOG" "$FAKE_LOG.cosign" "$FAKE_LOG.gh" 2>/dev/null
  : >"$FAKE_LOG"
  mkdir -p /home/u /opt
  rm -rf /tmp/stdin.seen /tmp/readers /tmp/fishbin /tmp/cut.sh
  unset SHELL ZDOTDIR LANG LC_ALL LC_MESSAGES
  HOME=/home/u
  FAKE_UNAME_S=Linux
  FAKE_UNAME_M=x86_64
  PATH=$SHIMS:$BASE_PATH
  export HOME FAKE_UNAME_S FAKE_UNAME_M PATH
  unset COSIGN_RC GH_RC GH_AUTH_RC FAKE_FSTYPE TIMEOUT_RC REFRESH_RC http_proxy
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

# D32: the launcher is written when the GUI is installed (--launcher is the same thing, kept for the old tests); INST_LAUNCHER=0 omits the flag.
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
inst --version v1.3.0
check "exit 0" rc_is 0
check "binary is 0755" mode_is /userdata/system/bin/mothdex 755
check "bin_dir is 0755" mode_is /userdata/system/bin 755
check "binary runs" runs_ok /userdata/system/bin/mothdex --version
check "launcher exists" test -f /userdata/roms/ports/Mothdex.sh
check "launcher is 0755" mode_is /userdata/roms/ports/Mothdex.sh 755
check "launcher owned by installer" owned_by_me /userdata/roms/ports/Mothdex.sh
check "launcher marker" file_has /userdata/roms/ports/Mothdex.sh '# mothdex-launcher v3'
check "launcher template runs the GUI only" file_has /userdata/roms/ports/Mothdex.sh 'exec "/userdata/system/bin/mothdex-gui"'
check "launcher passes sh -n" sh -n /userdata/roms/ports/Mothdex.sh
check "launcher forwards no arguments (IC-6)" text_is "$(/userdata/roms/ports/Mothdex.sh 'a b')" "gui "
check "only GitHub release URLs used" log_lacks BLOCKED
check "no leftovers in temp dir" test -z "$(ls -d /tmp/mothdex-install.* 2>/dev/null)"
check "missing signature tools are reported" out_has "SHA-256 only"

reset emuelec-install
fs_emuelec
inst --version v1.3.0
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

LINE="export PATH=\"/home/u/.local/bin:\$PATH\" # mothdex"
reset generic-install
inst --version v1.2.3
check "generic profile" out_has "profile: generic"
check "binary in HOME/.local/bin" mode_is /home/u/.local/bin/mothdex 755
check "PATH line added to ~/.profile when SHELL is unset" text_is "$(cat /home/u/.profile)" "$LINE"

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
inst --bin-dir /opt/mo+th-dex.v1/bin --version v1.3.0
check "safe unusual chars accepted" rc_is 0
check "launcher passes sh -n" sh -n /userdata/roms/ports/Mothdex.sh
check "launcher runs the literal path" text_is "$(/userdata/roms/ports/Mothdex.sh x)" "gui "

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
inst --version v1.3.0
check "symlink to a marked file is not replaced" test -L /userdata/roms/ports/Mothdex.sh
check "marked target untouched" file_has /tmp/victim "echo precious"
reset fat-world-writable-batocera-root
fs_batocera
mkdir -p /userdata/system/bin
chmod 0777 /userdata/system/bin /userdata/roms/ports
FAKE_FSTYPE=exfat inst --version v1.3.0
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
inst --version v1.3.0
check "binary still installed" test -x /userdata/system/bin/mothdex
check "launcher skipped" absent /userdata/roms/ports/Mothdex.sh
check "skip is explained" out_has "launcher skipped"
reset case52-keeps-user-launcher
fs_batocera
printf '#!/bin/sh\necho mine\n' >/userdata/roms/ports/Mothdex.sh
inst --version v1.3.0
check "user launcher kept" text_is "$(cat /userdata/roms/ports/Mothdex.sh)" "$(printf '#!/bin/sh\necho mine')"
check "kept is reported" out_has "keeping existing"
reset case52-overwrites-own-launcher
fs_batocera
inst --version v1.3.0
printf '#!/bin/sh\n# mothdex-launcher v1\necho stale\n' >/userdata/roms/ports/Mothdex.sh
inst --version v1.3.0
check "marked launcher refreshed" file_has /userdata/roms/ports/Mothdex.sh 'exec "/userdata/system/bin/mothdex-gui"'
check "still 0755" mode_is /userdata/roms/ports/Mothdex.sh 755
reset case52-symlink-launcher
fs_batocera
echo precious >/tmp/victim
ln -s /tmp/victim /userdata/roms/ports/Mothdex.sh
inst --version v1.3.0
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
inst --version v1.3.0
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

reset launcher-needs-the-gui
fs_batocera
INST_LAUNCHER=0
inst --version v1.2.3
check "a release without a GUI archive: binary installed" test -x /userdata/system/bin/mothdex
check "no launcher without the GUI" absent /userdata/roms/ports/Mothdex.sh
inst --launcher --version v1.2.3
check "--launcher without the GUI still writes nothing" absent /userdata/roms/ports/Mothdex.sh
check "and says why" out_has "written only when the GUI is installed"
inst --version v1.3.0
check "with the GUI the launcher is written" test -x /userdata/roms/ports/Mothdex.sh
inst --uninstall
check "uninstall removes the launcher" absent /userdata/roms/ports/Mothdex.sh
INST_LAUNCHER=1

reset wget-latest-refused
mkdir -p /tmp/nocurl
cp "$SHIMS/wget" "$SHIMS/fake-fetch" "$SHIMS/uname" /tmp/nocurl/
cp "$OPTIONAL/cosign" /tmp/nocurl/cosign
PATH=/tmp/nocurl:$BASE_PATH inst
check "wget without --version is refused (no rollback through a redirect)" rc_not0
check "asks for --version" out_has "--version"
check "nothing installed (wget latest)" absent /home/u/.local/bin/mothdex
reset parent-world-writable
fs_batocera
mkdir -p /opt/shared
chmod 0777 /opt/shared
inst --bin-dir /opt/shared/bin --version v1.2.3
check "bin_dir under a world-writable parent is refused" rc_not0
check "no binary (writable parent)" absent /opt/shared/bin/mothdex
reset parent-sticky-world-writable
fs_batocera
mkdir -p /opt/sticky
chmod 1777 /opt/sticky
inst --bin-dir /opt/sticky/bin --version v1.2.3
check "a sticky world-writable parent (like /tmp) is accepted" rc_is 0
reset parent-symlinked-in-profile-tree
fs_batocera
mkdir -p /opt/elsewhere/bin
chmod 0777 /opt/elsewhere/bin
ln -s /opt/elsewhere /userdata/link
FAKE_FSTYPE=exfat inst --bin-dir /userdata/link/bin --version v1.2.3
check "FAT exception does not follow a symlinked parent out of the profile tree" rc_not0
check "no binary (symlinked parent)" absent /opt/elsewhere/bin/mothdex

reset help-and-bad-options
inst --help
check "help exits 0" rc_is 0
inst --frobnicate
check "unknown option fails" rc_not0
inst --profile retrobat
check "retrobat points to install.ps1" out_has "install.ps1"
inst --profile nonsense
check "unknown profile fails" rc_not0

# --- Ports: the notice lists of both installers equal the files of THIRD_PARTY/ (a new notice must not break installs) --
notice_list_sh() { sed -n 's/^NOTICE_NAMES="\(.*\)"$/\1/p' "$REPO/install.sh" | tr ' ' '\n' | sort; }
# shellcheck disable=SC2016  # the PowerShell variable name is matched literally
notice_list_ps1() { grep '^\$NoticeNames = ' "$REPO/install.ps1" | grep -o "'[^']*'" | tr -d "'" | sort; }
notice_files() { (cd "$REPO/THIRD_PARTY" && for f in *; do [ -f "$f" ] && printf '%s\n' "$f"; done) | sort; }
CASE=notice-lists
check "install.sh notice list equals THIRD_PARTY/" text_is "$(notice_list_sh)" "$(notice_files)"
check "install.ps1 notice list equals THIRD_PARTY/" text_is "$(notice_list_ps1)" "$(notice_files)"

# --- Ports: GUI archive, eligibility, launcher v3 (GUI only, D32), default (ports.md sections 2 and 5; T-70 IC-1 to IC-12) --- (ports.md sections 2 and 5; T-70 IC-1 to IC-12) ---------

BATOCERA_BIN=/userdata/system/bin
PORT=/userdata/roms/ports/Mothdex.sh
V3_TEMPLATE=$(printf '#!/bin/sh\n# mothdex-launcher v3\nexec "%s/mothdex-gui"' "$BATOCERA_BIN")
V2_FILE=$(printf '#!/bin/sh\n# mothdex-launcher v2\nif [ -x "%s/mothdex-gui" ]; then exec "%s/mothdex-gui"; fi\nexec "%s/mothdex" tui' \
  "$BATOCERA_BIN" "$BATOCERA_BIN" "$BATOCERA_BIN")

# D32: the launcher is on whenever the GUI is installed, without --launcher.
INST_LAUNCHER=0
reset gui-flip-default
fs_batocera
http_proxy=http://127.0.0.1:9 inst --version v1.3.0
check "exit 0" rc_is 0
check "GUI installed 0755" mode_is $BATOCERA_BIN/mothdex-gui 755
check "launcher is on by default when the GUI is installed" test -x $PORT
check "launcher content equals the v3 template" text_is "$(cat $PORT)" "$V3_TEMPLATE"
check "the v3 launcher has no tui fallback" file_lacks $PORT 'tui'
# shellcheck disable=SC2016  # the strings are looked up literally
check "launcher has no argument forwarding (IC-6)" file_lacks $PORT '"$@"'
check "launcher has no percent-star (IC-6)" file_lacks $PORT '%*'
check "launcher sets no environment (2.3)" file_lacks $PORT 'LD_LIBRARY_PATH'
check "launcher runs the GUI" text_is "$($PORT)" "gui "
check "licence directory is 0755" mode_is $BATOCERA_BIN/mothdex-licenses 755
check "licence file is 0644" mode_is $BATOCERA_BIN/mothdex-licenses/NotoSans-OFL-1.1.txt 644
check "both notices installed" test -f $BATOCERA_BIN/mothdex-licenses/SDL_GameControllerDB-zlib.txt
check "the Go module notices installed" test -f $BATOCERA_BIN/mothdex-licenses/go-modules-mothdex-gui.txt
check "no staging or temp leftovers in bin_dir" no_stage $BATOCERA_BIN
check "path and opt-out printed (IC-12)" out_has "opt out with --no-launcher"
check "GUI archive comes from the same release" file_has "$FAKE_LOG" "download/v1.3.0/mothdex-gui_1.3.0_linux_amd64.tar.gz"
check "refresh asked on batocera" test -s "$FAKE_LOG.refresh"
check "refresh: literal loopback URL, no proxy, no redirect, bounded (IC-11)" file_has "$FAKE_LOG.refresh" \
  "--noproxy * --max-redirs 0 --max-time 3 -fs -o /dev/null http://127.0.0.1:1234/reloadgames"
check "only GitHub release URLs downloaded" log_lacks BLOCKED
check "every curl call starts with -q, so a curlrc cannot change it" only_dash_q "$FAKE_LOG.curlfirst"

reset refresh-failure-is-not-fatal
fs_batocera
REFRESH_RC=7 inst --version v1.3.0
check "a failing refresh does not fail the install" rc_is 0
check "failure is a note" out_has "could not ask the frontend"
reset refresh-emuelec
fs_emuelec
inst --version v1.3.0
check "emuelec refreshes too" test -s "$FAKE_LOG.refresh"
check "emuelec launcher in ports_scripts" test -x /storage/roms/ports_scripts/Mothdex.sh
reset recalbox-gui-no-launcher
fs_recalbox
inst --version v1.3.0
check "recalbox gets the GUI" test -x /recalbox/share/system/bin/mothdex-gui
check "recalbox has no launcher" test -z "$(find /recalbox -name Mothdex.sh)"
check "recalbox does not refresh" absent "$FAKE_LOG.refresh"
reset refresh-wget-only
mkdir -p /tmp/nocurl
cp "$SHIMS/wget" "$SHIMS/fake-fetch" "$SHIMS/uname" "$SHIMS/timeout" /tmp/nocurl/
cp "$OPTIONAL/cosign" /tmp/nocurl/cosign
fs_batocera
PATH=/tmp/nocurl:$BASE_PATH inst --version v1.3.0
check "wget never does the refresh (IC-11)" absent "$FAKE_LOG.refresh"
check "install still works" rc_is 0
check "told the user" out_has "could not ask the frontend"

# IC-1: the probe sees a scrubbed environment. IC-2: it runs from bin_dir, not from the temp dir. IC-10: bounded.
reset probe-environment
fs_batocera
LD_PRELOAD='' LD_LIBRARY_PATH=/tmp/ldpath SDL_DYNAMIC_API=/tmp/none SDL_VIDEODRIVER=dummy MOTHDEX_CONFIG=/tmp/c inst --version v1.3.0
check "GUI installed" test -x $BATOCERA_BIN/mothdex-gui
check "probe saw no LD_, SDL_, MOTHDEX_ or HOME variable (IC-1)" env_scrubbed
check "probe PATH is the fixed system path" file_has /tmp/probe.env "PATH=/usr/sbin:/usr/bin:/sbin:/bin"
check "probe ran from the staging name in bin_dir (IC-2)" probe_ran_in_bin
check "probe bounded by timeout 20 (IC-10)" file_has "$FAKE_LOG.timeout" "20 env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin"
reset probe-timeout
fs_batocera
TIMEOUT_RC=124 inst --version v1.3.0
check "a probe that does not finish means not eligible, exit 0" rc_is 0
check "says so" out_has "did not finish within 20 seconds"
check "GUI not installed" absent $BATOCERA_BIN/mothdex-gui
reset probe-message-sanitized
fs_batocera
inst --version v1.4.0
check "not eligible, exit 0" rc_is 0
check "control characters stripped (IC-10)" out_has_no_esc
check "message cut to 200 bytes (IC-10)" out_longest_zero_run_le 200

# not eligible, and the two ways the probe itself can fail
reset gui-not-eligible
fs_batocera
inst --version v1.3.1
check "exit 0" rc_is 0
check "CLI installed" test -x $BATOCERA_BIN/mothdex
check "GUI not installed" absent $BATOCERA_BIN/mothdex-gui
check "the probe's reason is shown" out_has "libSDL2-2.0.so.0 not found"
check "no licence notices without the GUI" absent $BATOCERA_BIN/mothdex-licenses
check "staging file removed" no_stage $BATOCERA_BIN
check "no launcher: a Port has no terminal for tui" absent $PORT
check "and says so" out_has "no launcher is written"
for pair in 'v1.3.2 exec-failure' 'v1.3.3 crash'; do
  # shellcheck disable=SC2086  # intentional word splitting of the "tag name" pair
  set -- $pair
  reset "gui-probe-$2"
  fs_batocera
  inst --version "$1"
  check "$2 is an installer error (exit 1)" rc_is 1
  check "$2 is reported as an error, not as not eligible (IC-2)" out_has "could not run mothdex-gui --probe"
  check "$2: the CLI install is complete" test -x $BATOCERA_BIN/mothdex
  check "$2: GUI not installed" absent $BATOCERA_BIN/mothdex-gui
  check "$2: staging file removed" no_stage $BATOCERA_BIN
done

# IC-3: whether a GUI archive exists comes from the verified checksums.txt only
reset gui-not-listed
fs_batocera
inst --version v1.2.3
check "exit 0" rc_is 0
check "skipped with a note" out_has "no GUI archive"
check "CLI installed" test -x $BATOCERA_BIN/mothdex
check "no launcher without a GUI archive" absent $PORT
check "no GUI download attempted" log_lacks mothdex-gui_
check "no refresh without a launcher" absent "$FAKE_LOG.refresh"
reset gui-flag-before-the-gui-release
fs_batocera
inst --gui --version v1.2.3
check "--gui before the GUI release does nothing, exit 0" rc_is 0
check "no GUI" absent $BATOCERA_BIN/mothdex-gui
INST_LAUNCHER=1
reset launcher-flag-without-the-gui
fs_batocera
inst --version v1.2.3
check "--launcher does not write a launcher without the GUI" absent $PORT
INST_LAUNCHER=0
reset gui-listed-not-downloadable
fs_batocera
inst --version v1.3.6
check "listed but not downloadable aborts" rc_not0
check "explains" out_has "could not be downloaded"
check "nothing installed, not even the CLI" absent $BATOCERA_BIN/mothdex
check "no launcher" absent $PORT
reset gui-checksum-mismatch
fs_batocera
inst --version v1.3.7
check "a GUI archive failing its checksum aborts the whole install" rc_not0
check "mismatch reported" out_has "SHA-256 mismatch for mothdex-gui_"
check "nothing installed (CLI neither)" absent $BATOCERA_BIN/mothdex

# IC-4: exact member names only, regular files only
for pair in 'v1.3.4 symlink-binary:not a regular file' 'v1.3.5 extra-member:unexpected member' \
  'v1.3.8 notice-link:not a regular file' 'v1.3.9 no-binary:does not contain mothdex-gui'; do
  tag=${pair%% *}
  rest=${pair#* }
  name=${rest%%:*}
  msg=${rest#*:}
  reset "gui-archive-$name"
  fs_batocera
  inst --version "$tag"
  check "$name aborts" rc_not0
  check "$name: $msg" out_has "$msg"
  check "$name: nothing installed" absent $BATOCERA_BIN/mothdex
  check "$name: no GUI" absent $BATOCERA_BIN/mothdex-gui
done
if gnu_tar; then
  reset gui-archive-dotdot
  fs_batocera
  inst --version v1.4.1
  check "a ../ member aborts" rc_not0
  check "nothing written outside" absent /userdata/system/mothdex-gui
  reset gui-archive-absolute
  fs_batocera
  inst --version v1.4.2
  check "an absolute member aborts" rc_not0
  check "nothing written to the absolute path" absent /tmp/pwn-abs
fi

# IC-5: mothdex-licenses is never written or deleted through a link
reset licenses-symlink
fs_batocera
mkdir -p $BATOCERA_BIN /tmp/victimdir
echo keep >/tmp/victimdir/keep.txt
ln -s /tmp/victimdir $BATOCERA_BIN/mothdex-licenses
inst --version v1.3.0
check "install still completes" rc_is 0
check "symlinked licence dir refused" out_has "refusing $BATOCERA_BIN/mothdex-licenses"
check "nothing written through the link" absent /tmp/victimdir/NotoSans-OFL-1.1.txt
check "victim file untouched" file_has /tmp/victimdir/keep.txt keep
inst --uninstall
check "uninstall does not follow the link" file_has /tmp/victimdir/keep.txt keep
check "the link is left alone" test -L $BATOCERA_BIN/mothdex-licenses
reset licenses-uninstall-keeps-foreign
fs_batocera
inst --version v1.3.0
echo mine >"$BATOCERA_BIN/mothdex-licenses/notes.txt"
inst --uninstall
check "notice files removed" absent $BATOCERA_BIN/mothdex-licenses/NotoSans-OFL-1.1.txt
check "a foreign file with a plain name is kept" file_has "$BATOCERA_BIN/mothdex-licenses/notes.txt" mine
check "so is the directory, with a note" out_has "kept $BATOCERA_BIN/mothdex-licenses"

# upgrade and --no-gui (5.5), uninstall (5.6)
reset upgrade-v1-launcher
fs_batocera
# shellcheck disable=SC2016  # the old template is literal on purpose
printf '#!/bin/sh\n# mothdex-launcher v1\nexec "/userdata/system/bin/mothdex" tui "$@"\n' >$PORT
inst --version v1.3.0
check "v1 launcher becomes v3" text_is "$(cat $PORT)" "$V3_TEMPLATE"
reset upgrade-keeps-unmarked
fs_batocera
printf '#!/bin/sh\necho mine\n' >$PORT
inst --version v1.3.0
check "a file without a marker is kept" text_is "$(cat $PORT)" "$(printf '#!/bin/sh\necho mine')"
check "and reported" out_has "keeping existing"
reset upgrade-recreates-deleted-launcher
fs_batocera
inst --version v1.3.0
rm -f $PORT
inst --version v1.3.0
check "a deleted launcher comes back by default" test -x $PORT
inst --no-launcher --version v1.3.0
check "--no-launcher leaves it alone" test -x $PORT
reset no-launcher-flag
fs_batocera
inst --no-launcher --version v1.3.0
check "GUI installed" test -x $BATOCERA_BIN/mothdex-gui
check "no launcher with --no-launcher" absent $PORT
check "no refresh" absent "$FAKE_LOG.refresh"
reset no-gui-upgrade
fs_batocera
inst --version v1.3.0
: >"$FAKE_LOG"
inst --no-gui --version v1.3.0
check "exit 0" rc_is 0
check "--no-gui removes the GUI" absent $BATOCERA_BIN/mothdex-gui
check "--no-gui removes the notices" absent $BATOCERA_BIN/mothdex-licenses
check "--no-gui: the launcher of ours is removed (a Port has no terminal for tui)" absent $PORT
check "and says so" out_has "Removed $PORT"
check "the GUI archive is not downloaded for --no-gui" log_lacks mothdex-gui_
reset no-gui-first-install
fs_batocera
inst --no-gui --version v1.3.0
check "CLI only" absent $BATOCERA_BIN/mothdex-gui
check "--no-gui: no launcher is written" absent $PORT
reset flag-conflicts
inst --gui --no-gui --version v1.3.0
check "--gui with --no-gui is refused" rc_not0
check "says why" out_has "cannot be used together"
inst --launcher --no-launcher --version v1.3.0
check "--launcher with --no-launcher is refused" rc_not0
reset gui-uninstall
fs_batocera
inst --version v1.3.0
mkdir -p /userdata/system/.config/mothdex /userdata/roms/snes
echo cfg >/userdata/system/.config/mothdex/config.toml
echo rom >/userdata/roms/snes/a.sfc
: >"$FAKE_LOG"
inst --uninstall --dry-run
check "dry run lists the GUI" out_has "Would remove $BATOCERA_BIN/mothdex-gui"
check "dry run lists the notices" out_has "Would remove $BATOCERA_BIN/mothdex-licenses"
check "dry run lists the launcher" out_has "Would remove $PORT"
check "dry run removes nothing" test -x $BATOCERA_BIN/mothdex-gui
inst --uninstall
check "uninstall ok" rc_is 0
check "binary, GUI, notices and launcher removed" absent_all $BATOCERA_BIN/mothdex $BATOCERA_BIN/mothdex-gui $BATOCERA_BIN/mothdex-licenses $PORT
check "config kept" test -f /userdata/system/.config/mothdex/config.toml
check "ROM kept" test -f /userdata/roms/snes/a.sfc
check "uninstall needs no network" no_downloads

# --dry-run (5.1)
reset gui-dry-run
fs_batocera
mkdir -p $BATOCERA_BIN
inst --dry-run --version v1.3.0
check "exit 0" rc_is 0
check "names the GUI archive" out_has "GUI archive: mothdex-gui_1.3.0_linux_amd64.tar.gz"
check "prints the eligibility" out_has "GUI eligibility: eligible"
check "prints the launcher path" out_has "Would write: $PORT"
check "changes nothing" absent_all $BATOCERA_BIN/mothdex $BATOCERA_BIN/mothdex-gui $BATOCERA_BIN/mothdex-licenses $PORT
check "no staging file left" no_stage $BATOCERA_BIN
reset gui-dry-run-no-bin-dir
fs_batocera
inst --dry-run --version v1.3.0
check "eligibility not checked when bin_dir does not exist" out_has "not checked"
check "bin_dir not created" absent $BATOCERA_BIN

# A GUI of an earlier install never outlives a release whose GUI is not installed (Alicerce, PR-88): the launcher must not
# start an older GUI next to a newer CLI, or one the probe just showed cannot start.
for pair in 'v1.3.1 not-eligible 0' 'v1.3.2 probe-error 1' 'v1.2.3 not-listed 0'; do
  # shellcheck disable=SC2086  # intentional word splitting of the "tag name exit" triple
  set -- $pair
  reset "upgrade-drops-gui-$2"
  fs_batocera
  inst --version v1.3.0
  check "$2: the first install has the GUI" test -x $BATOCERA_BIN/mothdex-gui
  inst --version "$1"
  check "$2: exit status $3" rc_is "$3"
  check "$2: the older GUI is gone" absent $BATOCERA_BIN/mothdex-gui
  check "$2: its notices are gone" absent $BATOCERA_BIN/mothdex-licenses
  check "$2: said so" out_has "Removed $BATOCERA_BIN/mothdex-gui"
  check "$2: the launcher of the older install is removed" absent $PORT
done
reset probe-killed-by-the-bound
fs_batocera
TIMEOUT_RC=143 inst --version v1.3.0
check "a probe killed by the bound (143) is not eligible, exit 0" rc_is 0
check "says it did not finish" out_has "did not finish within 20 seconds"
reset dry-run-prints-the-removal
fs_batocera
inst --version v1.3.0
inst --dry-run --version v1.3.1
check "dry run: not eligible prints the removal" out_has "Would remove $BATOCERA_BIN/mothdex-gui (not eligible)"
inst --dry-run --version v1.2.3
check "dry run: not listed prints the removal" out_has "Would remove $BATOCERA_BIN/mothdex-gui (this release has no GUI archive)"
check "dry run removes nothing" test -x $BATOCERA_BIN/mothdex-gui
reset gui-archive-hardlink
fs_batocera
inst --version v1.4.3
check "a hard link member is refused (by type on GNU tar, by link count on busybox)" rc_not0
check "refusal says so" out_has "not a regular file"
check "nothing installed" absent $BATOCERA_BIN/mothdex
reset gui-archive-unknown-notice
fs_batocera
inst --version v1.4.6
check "a safe but unknown THIRD_PARTY name aborts (the allowlist, IC-4)" rc_not0
check "says the member is unknown and the installer may be older than the release" out_has "installer may be older than the release"
check "points to the installer of the release tag" out_has "fetch install.sh from the release tag"
check "nothing installed" absent $BATOCERA_BIN/mothdex
for pair in 'v1.4.4 a-space' 'v1.4.5 a-star'; do
  # shellcheck disable=SC2086  # intentional word splitting of the "tag name" pair
  set -- $pair
  reset "gui-archive-badname-$2"
  fs_batocera
  inst --version "$1"
  check "$2: a member name outside A-Za-z0-9._- aborts as unsafe (IC-4)" rc_not0
  check "$2: says unsafe member name" out_has "unsafe member name"
  check "$2: nothing installed" absent $BATOCERA_BIN/mothdex
done

# D32 (PRT-G8): without the GUI no launcher is written and a launcher of ours is removed; a file without our marker, or a link,
# is never touched. The four ways of not installing the GUI: not eligible, a probe error, no GUI archive, --no-gui.
for scen in 'v1.3.1|' 'v1.3.2|' 'v1.2.3|' 'v1.3.0|--no-gui'; do
  tag=${scen%%|*}
  flag=${scen#*|}
  for marker in v1 v2 v3; do
    reset "d32-$tag-$marker"
    fs_batocera
    printf '#!/bin/sh\n# mothdex-launcher %s\necho old\n' "$marker" >$PORT
    # shellcheck disable=SC2086  # $flag is empty or one option
    inst --version "$tag" $flag
    check "$tag $flag, a $marker launcher: removed" absent $PORT
    check "$tag $flag, a $marker launcher: the removal is reported" out_has "Removed $PORT"
    check "$tag $flag, a $marker launcher: the frontend is asked to reload (batocera)" test -s "$FAKE_LOG.refresh"
    check "$tag $flag, a $marker launcher: the CLI is installed" test -x $BATOCERA_BIN/mothdex
  done
  reset "d32-user-$tag"
  fs_batocera
  printf '#!/bin/sh\necho mine\n' >$PORT
  # shellcheck disable=SC2086
  inst --version "$tag" $flag
  check "$tag $flag: a launcher without our marker is never touched" text_is "$(cat $PORT)" "$(printf '#!/bin/sh\necho mine')"
  check "$tag $flag: nothing was removed, so no refresh" absent "$FAKE_LOG.refresh"
  reset "d32-link-$tag"
  fs_batocera
  printf '#!/bin/sh\n# mothdex-launcher v2\necho precious\n' >/tmp/victim
  ln -s /tmp/victim $PORT
  # shellcheck disable=SC2086
  inst --version "$tag" $flag
  check "$tag $flag: a link is left alone" test -L $PORT
  check "$tag $flag: the target of the link is untouched" file_has /tmp/victim "echo precious"
  reset "d32-fresh-$tag"
  fs_batocera
  # shellcheck disable=SC2086
  inst --version "$tag" $flag
  check "$tag $flag: a fresh install writes no launcher" absent $PORT
done
reset d32-upgrade-v2
fs_batocera
printf '%s\n' "$V2_FILE" >$PORT
inst --version v1.3.0
check "a v2 launcher (with the tui fallback) becomes v3 when the GUI is installed" text_is "$(cat $PORT)" "$V3_TEMPLATE"
reset d32-no-launcher-keeps-v2
fs_batocera
printf '%s\n' "$V2_FILE" >$PORT
inst --no-launcher --version v1.3.0
check "--no-launcher with the GUI: an existing launcher is left as it is" text_is "$(cat $PORT)" "$V2_FILE"
reset d32-no-launcher-no-gui
fs_batocera
printf '%s\n' "$V2_FILE" >$PORT
inst --no-launcher --no-gui --version v1.3.0
check "--no-launcher without the GUI: a launcher of ours would start nothing, so it is removed" absent $PORT
reset d32-dry-run
fs_batocera
mkdir -p $BATOCERA_BIN
printf '%s\n' "$V2_FILE" >$PORT
inst --dry-run --version v1.3.1
check "dry run, not eligible: says it would remove the launcher" out_has "Would remove $PORT"
check "dry run, not eligible: the launcher is still there" test -f $PORT
check "dry run, not eligible: no refresh" absent "$FAKE_LOG.refresh"
inst --dry-run --version v1.3.0
check "dry run, eligible: says it would write the launcher" out_has "Would write: $PORT"
check "dry run, eligible: the old launcher is untouched" text_is "$(cat $PORT)" "$V2_FILE"
reset d32-dry-run-unknown
fs_batocera
inst --dry-run --version v1.3.0
check "dry run without a bin_dir: the launcher depends on the eligibility" out_has "only if the GUI turns out to be eligible"
check "dry run without a bin_dir: no launcher path claimed" out_lacks "Would write: $PORT"
reset d32-uninstall-all-markers
fs_batocera
for marker in v1 v2 v3; do
  printf '#!/bin/sh\n# mothdex-launcher %s\n' "$marker" >$PORT
  inst --uninstall
  check "uninstall removes a $marker launcher" absent $PORT
done

# arch mapping of the GUI archive (the same as the CLI archive)
reset gui-arch-armv7
FAKE_UNAME_M=armv7l inst --dry-run --version v1.3.0
check "armv7l asks for the armv7 GUI archive" out_has "GUI archive: mothdex-gui_1.3.0_linux_armv7.tar.gz"
reset gui-arch-386
FAKE_UNAME_M=i686 inst --dry-run --version v1.3.0
check "linux 386 has no GUI archive: CLI only" out_has "no GUI archive for linux_386"
reset gui-darwin
FAKE_UNAME_S=Darwin FAKE_UNAME_M=arm64 inst --dry-run --version v1.3.0
check "darwin: no GUI part at all" out_lacks "GUI"
INST_LAUNCHER=1

# --- PATH, pipe safety and install state (T-110 TM-1, TM-2, TM-7 to TM-11, TM-14) -------------------------------

GB=/home/u/.local/bin
ST=$GB/.mothdex-install.json
LINE="export PATH=\"/home/u/.local/bin:\$PATH\" # mothdex"
FISH_LINE="fish_add_path -g '/home/u/.local/bin' # mothdex"
INST_LAUNCHER=0
count_is() { [ "$(grep -cxF -- "$2" "$1" 2>/dev/null)" -eq "$3" ]; }
piped() { # arguments of the install, run as `curl ... | sh -s -- <args>`
  # shellcheck disable=SC2002  # the pipe is the point: stdin of sh is the script
  cat "$REPO/install.sh" | sh -s -- "$@" >"$OUT" 2>&1
  RC=$?
}

for sh_name in bash zsh; do
  reset "path-$sh_name"
  printf 'alias ll="ls -l"' >"/home/u/.${sh_name}rc" # no trailing newline
  chmod 0600 "/home/u/.${sh_name}rc"
  SHELL=/bin/$sh_name inst --version v1.2.3
  check "exit 0" rc_is 0
  check "our line, once" count_is "/home/u/.${sh_name}rc" "$LINE" 1
  check "the user's line is intact" count_is "/home/u/.${sh_name}rc" 'alias ll="ls -l"' 1
  check "mode kept" mode_is "/home/u/.${sh_name}rc" 600
  check "no other startup file written" absent /home/u/.profile
  check "state records the file" file_has "$ST" "\"path_file\": \"/home/u/.${sh_name}rc\""
  check "state records path_added" file_has "$ST" '"path_added": true'
  check "state mode 0644" mode_is "$ST" 644
  check "final message" out_has "mothdex 1.2.3 installed in $GB"
  check "tells how to use it now" out_has "run in this one: export PATH="
  installed_at=$(grep installed_at "$ST")
  SHELL=/bin/$sh_name inst --version v1.2.3
  check "reinstall does not duplicate" count_is "/home/u/.${sh_name}rc" "$LINE" 1
  check "reinstall keeps path_added" file_has "$ST" '"path_added": true'
  check "reinstall keeps installed_at" file_has "$ST" "$installed_at"
  # shellcheck disable=SC2016  # literal $ on purpose
  printf '%s\n' 'alias m=mothdex # mothdex' 'export PATH="/opt/x:$PATH" # mothdex' >>"/home/u/.${sh_name}rc"
  SHELL=/bin/$sh_name inst --uninstall
  check "uninstall ok" rc_is 0
  check "our line removed" count_is "/home/u/.${sh_name}rc" "$LINE" 0
  check "a user line with the marker survives" count_is "/home/u/.${sh_name}rc" 'alias m=mothdex # mothdex' 1
  # shellcheck disable=SC2016  # literal $ on purpose
  check "another PATH line with the marker survives" count_is "/home/u/.${sh_name}rc" 'export PATH="/opt/x:$PATH" # mothdex' 1
  check "mode kept after uninstall" mode_is "/home/u/.${sh_name}rc" 600
  check "state removed" absent "$ST"
done

reset path-fish
SHELL=/usr/bin/fish inst --version v1.2.3
check "fish line in config.fish" count_is /home/u/.config/fish/config.fish "$FISH_LINE" 1
check "fish folder 0755" mode_is /home/u/.config/fish 755
check "state kind fish" file_has "$ST" '"path_kind": "fish"'
SHELL=/usr/bin/fish inst --uninstall
check "fish line removed" count_is /home/u/.config/fish/config.fish "$FISH_LINE" 0
reset path-old-fish
mkdir -p /tmp/fishbin
printf '#!/bin/sh\necho "fish, version 3.1.2"\n' >/tmp/fishbin/fish
chmod 0755 /tmp/fishbin/fish
SHELL=/usr/bin/fish PATH=/tmp/fishbin:$PATH inst --version v1.2.3
check "old fish: nothing written" absent /home/u/.config/fish/config.fish
check "old fish: command printed" out_has "fish_add_path $GB"

reset path-already-present
printf 'x\n' >/home/u/.bashrc
SHELL=/bin/bash PATH=$PATH:$GB inst --version v1.2.3
check "exit 0" rc_is 0
check "rc untouched" text_is "$(cat /home/u/.bashrc)" x
check "state says nothing added" file_has "$ST" '"path_added": false'
check "says it is there" out_has "already on your PATH"
SHELL=/bin/bash inst --uninstall
check "uninstall leaves the rc" text_is "$(cat /home/u/.bashrc)" x

for pair in 'bash|>> ~/.bashrc' 'zsh|>> ~/.zshrc' 'fish|fish_add_path /home/u/.local/bin' 'dash|>> ~/.profile' 'tcsh|startup file of your shell'; do
  reset "no-modify-path-${pair%%|*}"
  SHELL=/bin/${pair%%|*} inst --version v1.2.3 --no-modify-path
  check "exit 0" rc_is 0
  check "command for the shell" out_has "${pair#*|}"
  check "never setx" out_lacks setx
  check "no startup file touched" absent_all /home/u/.bashrc /home/u/.zshrc /home/u/.profile /home/u/.config
  check "state says nothing added" file_has "$ST" '"path_added": false'
done
reset no-modify-path-exact-bash
SHELL=/bin/bash inst --version v1.2.3 --no-modify-path
check "exact bash command" out_has "echo 'export PATH=\"$GB:\$PATH\"' >> ~/.bashrc"

reset no-modify-path-uninstall
SHELL=/bin/bash inst --version v1.2.3
SHELL=/bin/bash inst --uninstall --no-modify-path
check "line kept on request" count_is /home/u/.bashrc "$LINE" 1

reset path-dry-run
SHELL=/bin/bash inst --version v1.2.3 --dry-run
check "dry run writes no rc and no state" absent_all /home/u/.bashrc "$ST"

reset path-rc-world-writable
printf 'x\n' >/home/u/.bashrc
chmod 0666 /home/u/.bashrc
SHELL=/bin/bash inst --version v1.2.3
check "exit 0" rc_is 0
check "rc untouched" text_is "$(cat /home/u/.bashrc)" x
check "command printed instead" out_has ">> ~/.bashrc"
reset path-rc-foreign-owner
printf 'x\n' >/home/u/.bashrc
chown 1000 /home/u/.bashrc
SHELL=/bin/bash inst --version v1.2.3
check "a file of another user is untouched" text_is "$(cat /home/u/.bashrc)" x
reset path-sudo
printf 'x\n' >/home/u/.bashrc
chown 1000 /home/u /home/u/.bashrc
SHELL=/bin/bash inst --version v1.2.3 --bin-dir /opt/m
check "sudo: the user's rc is untouched" text_is "$(cat /home/u/.bashrc)" x
check "sudo: says why" out_has "sudo"
reset path-rc-symlink
mkdir -p /home/u/dotfiles
printf 'x\n' >/home/u/dotfiles/bashrc
ln -s dotfiles/bashrc /home/u/.bashrc
SHELL=/bin/bash inst --version v1.2.3
check "written through the link" count_is /home/u/dotfiles/bashrc "$LINE" 1
SHELL=/bin/bash inst --uninstall
check "link kept" test -L /home/u/.bashrc
check "line removed from the target" count_is /home/u/dotfiles/bashrc "$LINE" 0

for tag in v1.1.0 v1.0.5; do
  reset "path-untouched-on-failure-$tag"
  SHELL=/bin/bash inst --version "$tag"
  check "no rc and no state" absent_all /home/u/.bashrc "$ST"
done
reset path-untouched-on-probe-error
SHELL=/bin/bash inst --version v1.4.7
check "exit 1" rc_is 1
check "no rc and no state" absent_all /home/u/.bashrc "$ST"

reset state-hostile
SHELL=/bin/bash inst --version v1.2.3
printf 'keep\n' >/etc/profile.mothdex-test
cp "$ST" /tmp/good.json
# shellcheck disable=SC2016  # literal $ on purpose
for bad in 's|"path_file": ".*"|"path_file": "/etc/profile.mothdex-test"|' 's|"path_value": ".*",|"path_value": "keep$(touch /tmp/pwn)",|' \
  's|"path_value": ".*",|"path_value": "keep",|' 's|"dir": ".*"|"dir": "/opt"|' 's|"schema_version": 1|"schema_version": 2|' 's|"path_kind": ".*"|"path_kind": "../x"|'; do
  sed "$bad" /tmp/good.json >"$ST"
  mkdir -p "$GB" && : >"$GB/mothdex"
  SHELL=/bin/bash inst --uninstall
  check "hostile state: uninstall ok ($bad)" rc_is 0
  check "hostile state: warned" out_has "not a valid install state"
  check "hostile state: our rc line left alone" count_is /home/u/.bashrc "$LINE" 1
  check "hostile state: foreign file untouched" text_is "$(cat /etc/profile.mothdex-test)" keep
  check "hostile state: nothing ran" absent /tmp/pwn
done
rm -f /etc/profile.mothdex-test /tmp/good.json

reset state-link-refused
mkdir -p $GB
echo victim >/tmp/victim
ln -s /tmp/victim $ST
inst --version v1.2.3
check "state link not written through" text_is "$(cat /tmp/victim)" victim

reset uninstall-without-state
mkdir -p $GB
: >$GB/mothdex
printf '%s\n' "$LINE" >/home/u/.bashrc
inst --uninstall
check "uninstall ok" rc_is 0
check "says it cannot know the PATH entry" out_has "no install state"
check "rc untouched" count_is /home/u/.bashrc "$LINE" 1

reset message-portuguese
SHELL=/bin/bash LANG=pt_BR.UTF-8 inst --version v1.2.3
check "portuguese message" out_has "mothdex 1.2.3 instalado em $GB"
check "portuguese PATH line" out_has "✓ PATH do usuário atualizado."

reset piped-install
SHELL=/bin/bash piped --version v1.2.3 --no-gui
check "curl | sh installs" rc_is 0
check "binary installed" test -x $GB/mothdex
check "PATH line added" count_is /home/u/.bashrc "$LINE" 1
reset piped-stdin-is-not-the-script
mkdir -p /tmp/readers
printf '#!/bin/sh\ncat >>/tmp/stdin.seen\nexit 0\n' >/tmp/readers/cosign
chmod 0755 /tmp/readers/cosign
PATH=$SHIMS:/tmp/readers:$BASE_PATH piped --version v1.0.6
check "install completes" out_has "Test: mothdex --version"
check "the readers ran and saw an empty stdin (TM-2)" test -f /tmp/stdin.seen -a ! -s /tmp/stdin.seen

reset truncated-download
total=$(wc -c <"$REPO/install.sh")
bad=""
for pct in 5 10 20 30 40 50 60 70 80 90 99 end; do
  if [ "$pct" = end ]; then n=$((total - 2)); else n=$((total * pct / 100)); fi
  head -c "$n" "$REPO/install.sh" >/tmp/cut.sh
  # a cut inside the leading comments is an empty script; from the opening brace on, the cut must be a syntax error
  if sh -s -- --version v1.2.3 </tmp/cut.sh >"$OUT" 2>&1 && grep -qx '{' /tmp/cut.sh; then bad="$bad $pct(exit 0)"; fi
  if [ -e $GB ] || [ -e /home/u/.profile ] || [ -s "$FAKE_LOG" ]; then bad="$bad $pct(side effect)"; fi
done
check "every truncation fails and does nothing (TM-1):$bad" text_is "$bad" ""

# --- the probe's exit codes (cli-output.md 4.1) -------------------------------------------------------------------
reset probe-exit1-legacy
fs_batocera
inst --version v0.1.5
check "0.1.x: exit 1 is not eligible" rc_is 0
check "0.1.x: says not installed" out_has "mothdex-gui was not installed"
reset probe-exit1-new
fs_batocera
inst --version v1.4.7
check "from 0.2.0: exit 1 is an error" rc_is 1
check "from 0.2.0: reported as a probe error" out_has "mothdex-gui --probe (exit 1)"
INST_LAUNCHER=1

printf '%s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
