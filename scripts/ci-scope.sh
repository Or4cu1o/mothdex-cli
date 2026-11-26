#!/bin/sh
# Decide inside a job whether its heavy steps must run, so the workflow can trigger on every pull request and
# its job names stay stable (a workflow skipped by a "paths:" filter never reports a check, so it cannot be a
# required status check).
#
#   scripts/ci-scope.sh SCOPE|PATTERN...
#
# SCOPE is a name from the list below (one list per protected thing, so the workflows cannot drift apart).
# PATTERN is a shell glob matched against each path changed by the pull request (a "*" also matches "/", so
# "internal/*" is the whole tree). A pattern that matches no tracked file (a path was moved or renamed) answers
# run=true, so a move turns CI loud instead of silently skipping. Use only "*" and plain names in a pattern: the
# stale check uses git pathspec and the match uses case globs, which differ for "?" and "[". Writes "run=true" or "run=false" to $GITHUB_OUTPUT (stdout when unset).
#
# Environment:
#   EVENT_NAME  github.event_name; anything but "pull_request" runs everything
#   BASE_SHA    github.event.pull_request.base.sha
#
# Fails closed: a missing base, a failed fetch or a failed diff answers run=true. No token and no network beyond
# the fetch of one commit from the repository the job already checked out.
set -u

# Everything compiled into the archives or the GUI probe: go:embed data (locales, profiles) counts.
release_scope='.goreleaser.yaml scripts/fetch-sdl2-windows.sh scripts/sdl2-windows.pin scripts/check-gui-archives.sh scripts/check-gui-zip.ps1 install.sh install.ps1 install.cmd scripts/stamp-installers.sh tests/installer/shims/* THIRD_PARTY/* .github/workflows/release.yml .github/workflows/release-check.yml scripts/verify-dist.sh scripts/gen-go-notices.sh scripts/ci-scope.sh tools/* cmd/* internal/* pkg/* locales/* profiles/* go.mod go.sum LICENSE README.md CHANGELOG.md'
gui_scope='internal/ui/gui/* cmd/mothdex-gui/* locales/* profiles/* scripts/gui-probe.sh scripts/ci-scope.sh go.mod go.sum'
gui_probe_scope="$gui_scope scripts/check-gui-isolation.sh scripts/check-sdl-constants.sh scripts/sdl2-windows.pin .github/workflows/gui-spike.yml"
gui_qemu_scope="$gui_scope .github/workflows/gui-spike-qemu.yml"

out=${GITHUB_OUTPUT:-/dev/stdout}
answer() {
	printf 'run=%s\n' "$1" >>"$out"
	echo "ci-scope: run=$1 ($2)"
	exit 0
}

[ "$#" -gt 0 ] || answer true "no patterns given"
[ "${EVENT_NAME:-}" = pull_request ] || answer true "event ${EVENT_NAME:-unset} is not a pull request"
case "${BASE_SHA:-}" in
*[!0-9a-f]* | "") answer true "base sha missing or malformed" ;;
esac

# Expand scope names (the lists hold no spaces inside a pattern).
patterns=""
for arg in "$@"; do
	case $arg in
	release) patterns="$patterns $release_scope" ;;
	gui) patterns="$patterns $gui_probe_scope" ;;
	gui-qemu) patterns="$patterns $gui_qemu_scope" ;;
	*) patterns="$patterns $arg" ;;
	esac
done
set -f # the patterns are globs for case, not for the shell
# shellcheck disable=SC2086
set -- $patterns
for pattern in "$@"; do
	[ -n "$(git ls-files -- "$pattern" | head -n 1)" ] || answer true "stale pattern $pattern (matches no tracked file)"
done

# The checked-out commit is the merge of the head into the base, so base..HEAD is what the pull request changes.
git cat-file -e "$BASE_SHA^{commit}" 2>/dev/null || git fetch --no-tags --depth=1 origin "$BASE_SHA" >/dev/null 2>&1 ||
	answer true "could not fetch the base commit"
changed=$(git -c core.quotepath=off diff --name-only --no-renames "$BASE_SHA" HEAD) || answer true "git diff failed"

# One path per line (names with spaces stay whole); no subshell, so answer exits the script.
while IFS= read -r path; do
	# git C-quotes a path that holds a double quote, tab, newline or backslash ("a\tb"): it cannot match a pattern
	# safely, so fail closed.
	case $path in
	\"*) answer true "quoted path $path" ;;
	esac
	for pattern in "$@"; do
		# shellcheck disable=SC2254
		case $path in
		$pattern) answer true "$path matches $pattern" ;;
		esac
	done
done <<EOF
$changed
EOF
answer false "no changed path matches"
