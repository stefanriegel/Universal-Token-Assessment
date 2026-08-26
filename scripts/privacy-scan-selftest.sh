#!/usr/bin/env bash
# Proving run for scripts/privacy-scan.sh (MSPAR-05 gap closure, G1/G3).
#
# Both halves of scripts/privacy-scan.sh had only ever been observed passing —
# nobody had watched either half actually catch a leak. This script plants
# synthetic values on top of the real scan and asserts the scan CATCHES them.
# Every planted value here is synthetic by construction (a fictitious hostname
# label, a made-up token) and must never be replaced with a real customer
# name, domain or address.
#
# Four assertions, each printing PASS/FAIL and contributing to a non-zero
# exit on any failure:
#   part-a-true-negative    - no probe present -> privacy-scan.sh exits 0
#   part-a-true-positive    - a non-reserved .local hostname planted in scope
#                              -> privacy-scan.sh exits non-zero AND its
#                              combined output names the probe path
#   part-b-negative-control - the synthetic Part B token appears in no
#                              tracked file before planting (proves the next
#                              assertion's catch comes from the plant, not
#                              from this script's own source)
#   part-b-true-positive    - the synthetic token planted in a tracked file
#                              -> .githooks/pre-commit (tree mode) exits
#                              non-zero AND names the probe path

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

PROBE="testdata/privacy-scan-selftest.probe"
FAILURES=0

# Part B synthetic pattern, assembled at runtime from two literals kept
# apart in the source. .githooks/pre-commit in tree mode greps the whole
# tracked tree, including this script, so a single joined literal here would
# make part-b-true-positive pass on the script's own text instead of on the
# plant, and would make part-b-negative-control impossible.
SYNTH_PREFIX="ZzSynthProbe"
SYNTH_SUFFIX="Name9"
SYNTH="${SYNTH_PREFIX}${SYNTH_SUFFIX}"

unplant() {
  git rm --cached -q --ignore-unmatch "$PROBE" >/dev/null 2>&1 || true
  rm -f "$PROBE"
}
trap unplant EXIT

plant() {
  local payload="$1"
  printf '%s\n' "$payload" >"$PROBE"
  git add -N "$PROBE"
}

# assert_rc NAME EXPECTED_RC -- CMD...
# Runs CMD, captures combined stdout+stderr into ASSERT_OUTPUT, compares rc
# against EXPECTED_RC. Sets ASSERT_RC_OK=1/0 but does not print — callers that
# also need assert_reports combine both into a single PASS/FAIL line.
assert_rc() {
  local expected="$1"
  shift
  local rc=0
  ASSERT_OUTPUT="$("$@" 2>&1)" || rc=$?
  [ "$rc" = "$expected" ] && ASSERT_RC_OK=1 || ASSERT_RC_OK=0
  ASSERT_RC_ACTUAL="$rc"
}

# assert_reports PATH
# Greps ASSERT_OUTPUT (set by the preceding assert_rc) for PATH. Sets
# ASSERT_REPORTS_OK=1/0; does not print.
assert_reports() {
  local path="$1"
  if printf '%s' "$ASSERT_OUTPUT" | grep -qF "$path"; then
    ASSERT_REPORTS_OK=1
  else
    ASSERT_REPORTS_OK=0
  fi
}

report() {
  local name="$1"
  local ok="$2"
  local detail="${3:-}"
  if [ "$ok" = "1" ]; then
    echo "PASS $name"
  else
    echo "FAIL $name${detail:+ ($detail)}"
    FAILURES=$((FAILURES + 1))
  fi
}

# ---- Part A: true negative (no probe present) ------------------------------
unplant
assert_rc 0 bash scripts/privacy-scan.sh
report part-a-true-negative "$ASSERT_RC_OK" "expected rc=0, got rc=$ASSERT_RC_ACTUAL"

# ---- Part A: true positive (non-reserved .local hostname planted) ---------
plant 'dc01.pscan-selftest.local'
assert_rc 1 bash scripts/privacy-scan.sh
if [ "$ASSERT_RC_OK" = "1" ]; then
  assert_reports "$PROBE"
else
  ASSERT_REPORTS_OK=0
fi
if [ "$ASSERT_RC_OK" = "1" ] && [ "$ASSERT_REPORTS_OK" = "1" ]; then
  report part-a-true-positive 1
else
  report part-a-true-positive 0 "expected rc=1 and output naming $PROBE, got rc=$ASSERT_RC_ACTUAL"
fi
unplant

# ---- Part A: true positive, uppercase (CR-01 regression guard) -----------
# Same probe as above, mixed/upper-cased. DOMAIN_RE's TLD alternation must
# match case-insensitively or an uppercase real-world FQDN (a common form
# for AD/NIOS-derived hostnames) bypasses Part A entirely.
plant 'DC01.PSCAN-SELFTEST.LOCAL'
assert_rc 1 bash scripts/privacy-scan.sh
if [ "$ASSERT_RC_OK" = "1" ]; then
  assert_reports "$PROBE"
else
  ASSERT_REPORTS_OK=0
fi
if [ "$ASSERT_RC_OK" = "1" ] && [ "$ASSERT_REPORTS_OK" = "1" ]; then
  report part-a-true-positive-uppercase 1
else
  report part-a-true-positive-uppercase 0 "expected rc=1 and output naming $PROBE, got rc=$ASSERT_RC_ACTUAL"
fi
unplant

# ---- Part B: negative control (synthetic token not already tracked) -------
NEGATIVE_CONTROL_HITS="$(git grep -I -c -iE "$SYNTH" -- . ":(exclude)$PROBE" 2>/dev/null || true)"
if [ -z "$NEGATIVE_CONTROL_HITS" ]; then
  report part-b-negative-control 1
else
  report part-b-negative-control 0 "synthetic token already present: $NEGATIVE_CONTROL_HITS"
fi

# ---- Part B: true positive (synthetic token planted, caught in tree mode) --
plant "internal note: $SYNTH is a synthetic probe value"
assert_rc 1 env CUSTOMER_NAME_DENYLIST="$SYNTH" CUSTOMER_NAME_SCAN_SCOPE=tree CUSTOMER_NAME_SCAN_REDACT=0 ./.githooks/pre-commit
if [ "$ASSERT_RC_OK" = "1" ]; then
  assert_reports "$PROBE"
else
  ASSERT_REPORTS_OK=0
fi
if [ "$ASSERT_RC_OK" = "1" ] && [ "$ASSERT_REPORTS_OK" = "1" ]; then
  report part-b-true-positive 1
else
  report part-b-true-positive 0 "expected rc=1 and output naming $PROBE, got rc=$ASSERT_RC_ACTUAL"
fi
unplant

echo ""
if [ "$FAILURES" -ne 0 ]; then
  echo "privacy-scan-selftest: $FAILURES assertion(s) failed."
  exit 1
fi

echo "privacy-scan-selftest: all assertions passed."
exit 0
