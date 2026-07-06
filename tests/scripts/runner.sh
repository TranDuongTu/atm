#!/bin/sh
# POSIX sh test harness for scripts/_release_lib.sh. No bats dependency.
set -u

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)

# shellcheck source=../../scripts/_release_lib.sh
. "$REPO_ROOT/scripts/_release_lib.sh"

passes=0
fails=0

assert_eq() {
  if [ "$1" = "$2" ]; then
    passes=$((passes + 1))
  else
    fails=$((fails + 1))
    echo "FAIL: $3"
    echo "  expected: $2"
    echo "  got:      $1"
  fi
}

assert_exit() {
  expected=$1; shift
  "$@" >/dev/null 2>&1; got=$?
  if [ "$got" = "$expected" ]; then
    passes=$((passes + 1))
  else
    fails=$((fails + 1))
    echo "FAIL: expected exit $expected from: $*"
    echo "  got exit: $got"
  fi
}

for t in "$SCRIPT_DIR"/*.bats.sh; do
  . "$t"
done

echo "scripts-test: $passes passed, $fails failed"
[ "$fails" = 0 ] || exit 1