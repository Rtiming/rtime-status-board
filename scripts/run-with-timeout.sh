#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <seconds> <command> [args...]" >&2
  exit 64
fi

seconds="$1"
shift

if ! [[ "$seconds" =~ ^[0-9]+$ ]] || [[ "$seconds" -le 0 ]]; then
  echo "[ERROR] timeout seconds must be a positive integer: $seconds" >&2
  exit 64
fi

timeout_flag="${TMPDIR:-/tmp}/rtime-status-board-timeout-$$"
rm -f "$timeout_flag"

"$@" &
cmd_pid=$!

(
  sleep "$seconds"
  if kill -0 "$cmd_pid" 2>/dev/null; then
    echo "[ERROR] command timed out after ${seconds}s: $*" >&2
    touch "$timeout_flag"
    kill "$cmd_pid" 2>/dev/null || true
    sleep 2
    kill -9 "$cmd_pid" 2>/dev/null || true
  fi
) &
watchdog_pid=$!

set +e
wait "$cmd_pid" 2>/dev/null
status=$?
set -e

kill "$watchdog_pid" 2>/dev/null || true
wait "$watchdog_pid" 2>/dev/null || true

if [[ -f "$timeout_flag" ]]; then
  rm -f "$timeout_flag"
  exit 124
fi

exit "$status"
