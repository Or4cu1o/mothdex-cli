# Second pass of the installer tests, as a non-root user (sourced by cases.sh when NONROOT=1). run.sh gives the
# container a writable HOME owned by this uid and OTHER_DIR owned by another non-root uid. Proves the
# "owned by the installing user" branch of dir_safe, which the root pass cannot reach (root matches -user 0).
CASE=nonroot
FAKE_UNAME_S=Linux
FAKE_UNAME_M=x86_64
PATH=$SHIMS:$BASE_PATH
export FAKE_UNAME_S FAKE_UNAME_M PATH
: >"$FAKE_LOG"

check "running as a non-root user" test "$(id -u)" -ne 0

rm -rf "$HOME/.local"
inst --version v1.2.3
check "default install into an own HOME works" rc_is 0
check "binary installed" test -x "$HOME/.local/bin/mothdex"
check "binary owned by this user" owned_by_me "$HOME/.local/bin/mothdex"

inst --dry-run --version v1.2.3 --bin-dir /usr/local/bin
check "root-owned bin_dir is accepted (dry run)" rc_is 0

inst --dry-run --version v1.2.3 --bin-dir "$OTHER_DIR"
check "bin_dir owned by another non-root user is refused" rc_not0
check "says owned by another user" out_has "another user"

mkdir -p "$HOME/w"
chmod 0777 "$HOME/w"
inst --version v1.2.3 --bin-dir "$HOME/w"
check "own but world-writable bin_dir refused" rc_not0

FAKE_FSTYPE=exfat inst --version v1.2.3 --profile batocera --bin-dir "$HOME/w"
check "FAT exception is for root only" rc_not0

# Ports (T-70 IC-2): the probe runs from bin_dir, so a temp dir that cannot run programs does not hide the GUI.
rm -rf "$HOME/.local"
TMPDIR=/noexec inst --version v1.3.0
check "noexec TMPDIR: install exit 0" rc_is 0
check "noexec TMPDIR: the GUI is installed" test -x "$HOME/.local/bin/mothdex-gui"
check "noexec TMPDIR: the probe ran from bin_dir" test "$(dirname "$(cat /tmp/probe.where)")" = "$HOME/.local/bin"
check "noexec TMPDIR: not silently dropped" out_lacks "not installed"

# A bin_dir on a mount that cannot run programs falls back to $HOME/.local/bin, and the launcher uses the fallback
# (ports.md section 11).
rm -rf "$HOME/.local"
: >"$FAKE_LOG"
inst --profile batocera --version v1.3.0
check "noexec bin_dir: install exit 0" rc_is 0
check "noexec bin_dir: says it fell back" out_has "cannot run programs"
check "noexec bin_dir: binaries in the fallback" test -x "$HOME/.local/bin/mothdex-gui"
check "noexec bin_dir: launcher written" test -f /userdata/roms/ports/Mothdex.sh
check "noexec bin_dir: launcher points at the fallback dir" file_has /userdata/roms/ports/Mothdex.sh "\"$HOME/.local/bin/mothdex-gui\""
