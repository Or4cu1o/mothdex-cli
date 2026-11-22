#!/bin/sh
# Runs the installer tests (cases.sh) in throwaway containers with no network: Alpine (busybox ash, as on
# EmuELEC/Recalbox-like systems) and Debian (dash, GNU userland). Images are pinned by digest.
# Usage: sh tests/installer/run.sh   (needs docker)
set -eu

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

IMAGES="
alpine@sha256:49340b346798875b5592e5d420241f2f9cfba5f47e62ebb1446a1b34734e898f
debian@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
"

status=0
for image in $IMAGES; do
  printf '== %s\n' "$image"
  docker run --rm --network none -v "$repo:/repo:ro" "$image" sh /repo/tests/installer/cases.sh || status=1
  # second pass as uid 1000: own HOME, a directory owned by another non-root uid, and two noexec mounts (/noexec for
  # TMPDIR, /userdata as a Batocera share that cannot run programs)
  docker run --rm --network none --user 1000:1000 -e NONROOT=1 -e HOME=/tmp/home -e FAKE_RELEASE=/tmp/fake-release \
    -e OTHER_DIR=/tmp/other --tmpfs /tmp/home:exec,uid=1000,gid=1000,mode=755 \
    --tmpfs /tmp/other:uid=2000,gid=2000,mode=755 \
    --tmpfs /noexec:noexec,uid=1000,gid=1000,mode=755 --tmpfs /userdata:noexec,uid=1000,gid=1000,mode=755 \
    -v "$repo:/repo:ro" "$image" sh /repo/tests/installer/cases.sh || status=1
done
exit $status
