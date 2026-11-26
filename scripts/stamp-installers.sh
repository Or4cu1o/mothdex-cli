#!/bin/sh
# Writes the installers published as release assets (T-110 TM-5): each one carries the version of its own release and
# installs that version unless told otherwise, and install.cmd fetches the install.ps1 of the same release.
#
# Usage: scripts/stamp-installers.sh <version> <out-dir>   (version without the leading v; run from the repository root)
# GoReleaser runs it as a hook and lists the result in checksums.txt, so the installers are signed and attested.
set -eu

[ $# -eq 2 ] || { echo "usage: $0 <version> <out-dir>" >&2; exit 2; }
version=$1
out=$2
case $version in
  '' | *[!0-9A-Za-z.-]*) echo "stamp-installers: bad version '$version'" >&2; exit 2 ;;
esac

# stamp <file> <exact line to replace> <new line>: fails unless the line is there exactly once.
stamp() {
  [ "$(tr -d '\r' <"$1" | grep -cxF -- "$2")" -eq 1 ] || { echo "stamp-installers: $1 has no single line '$2'" >&2; exit 1; }
  FROM=$2 TO=$3 awk '{ cr = sub(/\r$/, "") } $0 == ENVIRON["FROM"] { $0 = ENVIRON["TO"] } { printf "%s%s\n", $0, (cr ? "\r" : "") }' \
    "$1" >"$out/$1"
}

mkdir -p "$out"
stamp install.sh 'STAMPED_VERSION=""' "STAMPED_VERSION=\"$version\""
stamp install.ps1 "\$StampedVersion = ''" "\$StampedVersion = '$version'"

latest='releases/latest/download/install.ps1'
[ "$(grep -cF -- "$latest" install.cmd)" -eq 1 ] || { echo "stamp-installers: install.cmd has no single '$latest'" >&2; exit 1; }
# CRLF whatever the checkout did: cmd.exe can miss a label in a file with bare line feeds
tr -d '\r' <install.cmd | sed "s|$latest|releases/download/v$version/install.ps1|" | awk '{ printf "%s\r\n", $0 }' >"$out/install.cmd"
