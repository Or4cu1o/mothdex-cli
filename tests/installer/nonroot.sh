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
