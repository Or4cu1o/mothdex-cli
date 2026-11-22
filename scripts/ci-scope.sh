#!/bin/sh
# Decide inside a job whether its heavy steps must run, so the workflow can trigger on every pull request and
# its job names stay stable (a workflow skipped by a "paths:" filter never reports a check, so it cannot be a
# required status check).
#
#   scripts/ci-scope.sh PATTERN...
#
# PATTERN is a shell glob matched against each path changed by the pull request (a "*" also matches "/", so
# "internal/*" is the whole tree). Writes "run=true" or "run=false" to $GITHUB_OUTPUT (stdout when unset).
#
# Environment:
#   EVENT_NAME  github.event_name; anything but "pull_request" runs everything
#   BASE_SHA    github.event.pull_request.base.sha
#
# Fails closed: a missing base, a failed fetch or a failed diff answers run=true. No token and no network beyond
# the fetch of one commit from the repository the job already checked out.
set -u

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

# The checked-out commit is the merge of the head into the base, so base..HEAD is what the pull request changes.
git cat-file -e "$BASE_SHA^{commit}" 2>/dev/null || git fetch --no-tags --depth=1 origin "$BASE_SHA" >/dev/null 2>&1 ||
	answer true "could not fetch the base commit"
changed=$(git -c core.quotepath=off diff --name-only --no-renames "$BASE_SHA" HEAD) || answer true "git diff failed"

# One path per line (names with spaces stay whole); no subshell, so answer exits the script.
while IFS= read -r path; do
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
