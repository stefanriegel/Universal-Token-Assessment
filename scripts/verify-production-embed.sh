#!/usr/bin/env bash
set -euo pipefail

binary="${1:-./uta-production}"
log_file="$(mktemp)"
index_file="$(mktemp)"
asset_file="$(mktemp)"
pid=""
cleanup() {
  set +e
  if [[ -n "$pid" ]]; then kill "$pid" 2>/dev/null; wait "$pid" 2>/dev/null; fi
  rm -f "$log_file" "$index_file" "$asset_file"
  set -e
}
trap cleanup EXIT

LISTEN_ADDR=127.0.0.1:0 NO_BROWSER=1 "$binary" >"$log_file" 2>&1 &
pid=$!
url=""
for _ in {1..100}; do
  url="$(sed -n 's/.*url \(http:\/\/localhost:[0-9][0-9]*\)).*/\1/p' "$log_file" | tail -1)"
  [[ -n "$url" ]] && curl --fail --silent --show-error "$url/" -o "$index_file" && break
  kill -0 "$pid" 2>/dev/null || { cat "$log_file" >&2; exit 1; }
  sleep 0.1
done
[[ -n "$url" && -s "$index_file" ]] || { cat "$log_file" >&2; exit 1; }
! grep -q 'flow42-test-static-files' "$index_file"
asset="$(grep -m1 -oE '(src|href)="(\./|/)?assets/[^"]+"' "$index_file" | cut -d'"' -f2)"
asset="${asset#./}"
asset="${asset#/}"
[[ "$asset" == assets/* ]]
curl --fail --silent --show-error "$url/$asset" -o "$asset_file"
[[ -s "$asset_file" ]]
! grep -q 'flow42-test-static-files' "$asset_file"
printf 'production embed served %s and %s\n' "$url/" "$asset"
