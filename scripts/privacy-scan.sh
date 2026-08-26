#!/usr/bin/env bash
# Static privacy scan for the Microsoft-allocation surface (D-13 static half).
#
# Enforces two independent rules and reports a single roll-up exit status:
#
#   Part A (D-14) — a positive reserved-range rule. Any IPv4/IPv6 literal or
#     hostname on the scan scope below must sit inside a documentation range
#     reserved by RFC 5737 / RFC 3849 / RFC 2606. Anything else is a
#     violation. This is the opposite of a denylist: nothing is permitted
#     unless it matches one of the reserved forms.
#
#   Part B (D-15) — the existing customer/internal-name denylist, reused
#     rather than reimplemented, run in tree mode via .githooks/pre-commit.
#
# D-16 (logs) is explicitly out of scope for this script by user decision;
# see 08-CONTEXT.md and 08-03-PLAN.md's backstop truths.
#
# Exit status: 0 only if both parts are clean. Non-zero and the offending
# file:line otherwise.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# ---------------------------------------------------------------------------
# Part A scope (FA-2, confirmed 2026-08-24 in 08-CONTEXT.md): the reserved-
# range rule is scoped to the Microsoft-allocation surface plus testdata/,
# not the whole tracked tree. Applied to every tracked file, the rule is
# unsatisfiable — main.go must contain 127.0.0.1 per CLAUDE.md's bind
# requirement, and the tree carries ~338 other IPv4-shaped literals, mostly
# RFC 1918 addresses in unrelated cloud-scanner tests and version strings
# that are not addresses at all. Satisfying a whole-tree rule would require
# exactly the per-file allowlist D-14 rejects. Widening this list later is a
# one-line change here, not a design change.
SCOPE=(
  'internal/scanner/nios/ms_*'
  'internal/scanner/nios/microsoft*'
  'server/microsoft_allocation*'
  'server/nios_microsoft*'
  'internal/exporter/ms_allocation_sheet*'
  'frontend/src/app/components/results/ms-allocation-panel*'
  'frontend/src/app/components/results/__tests__/*'
  'testdata/'
  '**/testdata/**'
  'docs/superpowers/plans/2026-07-03-nios-microsoft-managed-servers.md'
)

VIOLATIONS=0

# ---- IPv4: permit only 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24 ----
while IFS=: read -r file line addr; do
  [ -z "${file:-}" ] && continue
  case "$addr" in
    192.0.2.*|198.51.100.*|203.0.113.*) ;;
    *)
      echo "privacy-scan: $file:$line: non-reserved IPv4 literal '$addr'" >&2
      VIOLATIONS=1
      ;;
  esac
done < <(git grep -I -n -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' -- "${SCOPE[@]}" 2>/dev/null || true)

# ---- IPv6: permit only the 2001:db8::/32 documentation prefix ----
while IFS=: read -r file line addr; do
  [ -z "${file:-}" ] && continue
  case "$(printf '%s' "$addr" | tr 'A-F' 'a-f')" in
    2001:db8*) ;;
    *)
      echo "privacy-scan: $file:$line: non-reserved IPv6-shaped literal '$addr'" >&2
      VIOLATIONS=1
      ;;
  esac
done < <(git grep -I -n -oE '([0-9A-Fa-f]{1,4}:){2,7}[0-9A-Fa-f]{0,4}' -- "${SCOPE[@]}" 2>/dev/null || true)

# ---- Domains: permit only example.com/net/org (RFC 2606), hostnames ending
# in the four outright-reserved TLDs .example/.test/.invalid/.localhost, and
# hostnames ending .local ONLY when the registrable label is a reserved
# documentation placeholder (example/test/contoso). A TLD-list-based match is
# required, not a generic dotted-token pattern: this codebase is full of
# dotted identifiers that are not hostnames at all — NIOS `__type` strings
# such as ".com.infoblox.dns.net" (leading dot; skipped below), and Go module
# import paths such as "github.com/..." (trailing slash; skipped below). A
# generic pattern would flag both classes, forcing exactly the per-file
# exception list D-14 rejects.
#
# Two separate alternation lists, composed together into one regex:
#   REAL_TLDS     - labels that must never appear unqualified; a match here
#                   is always inspected by the permit case below.
#   RESERVED_TLDS - RFC 2606 / RFC 6761 reserved suffixes (.localhost before
#                   .local: POSIX ERE alternation is leftmost-longest, and
#                   the explicit ordering documents the intent rather than
#                   relying on it). Also inspected by the permit case below —
#                   listing them separately keeps the distinction between
#                   "must never appear" and "must be inspected then permitted"
#                   visible in the source, rather than collapsing both into
#                   one undifferentiated list.
# Matched case-insensitively (-i on the git grep below): real AD/NIOS-derived
# hostnames routinely appear all-caps or mixed-case (Windows FQDN
# convention), and these alternations are lowercase-only literals. The `tr
# 'A-Z' 'a-z'` fold on `bare` before the permit-list `case` below is what
# makes the downstream comparison work once -i lets a match through in the
# first place — do not drop either half, or an uppercase domain silently
# bypasses this gate again (CR-01).
REAL_TLDS='com|net|org|edu|gov|mil|int|info|biz|io|co|dev|app|cloud|uk|de|fr|nl|se|ch|at|es|it|ru|cn|jp|au|ca|in|br'
RESERVED_TLDS='localhost|local|test|invalid|example'
DOMAIN_RE='\.?[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*\.('"$REAL_TLDS"'|'"$RESERVED_TLDS"')([^A-Za-z0-9]|$)'
while IFS=: read -r file line match; do
  [ -z "${file:-}" ] && continue
  case "$match" in
    .*) continue ;; # leading dot: NIOS __type continuation, not a hostname
  esac
  # Only strip the trailing boundary character when it is actually
  # non-alphanumeric. DOMAIN_RE's boundary group can match the empty string
  # at end-of-line, so a hostname that ends a line has an alphanumeric final
  # character — unconditionally stripping it would truncate a legitimately
  # reserved name (e.g. "...local" losing its final "l") before the permit
  # comparison below.
  last_char="${match: -1}"
  case "$last_char" in
    [A-Za-z0-9]) bare="$match" ;;
    *) bare="${match%?}" ;;
  esac
  [ "$last_char" = "/" ] && continue # import path / URL, not a bare hostname
  case "$(printf '%s' "$bare" | tr 'A-Z' 'a-z')" in
    *.example.com|*.example.net|*.example.org|example.com|example.net|example.org) continue ;;
    # RFC 2606 / RFC 6761 reserved TLDs: cannot be registered by anyone, so
    # no real customer domain can end in one. Also covers the *.ms-allocation.test
    # fixture hostnames and *.test.tsx/*.test.ts import-string fragments.
    *.test|test|*.invalid|invalid|*.localhost|localhost|*.example|example) continue ;;
    # .local is the one reserved suffix real deployments actually use —
    # "<company>.local" is the standard on-prem Active Directory internal
    # domain — so it is NOT blanket-permitted. Only these reserved
    # documentation registrable labels are: example/test (RFC 2606) and
    # contoso (Microsoft's own reserved documentation domain, used by the
    # live contoso.local fixtures). Widen this list deliberately if a new
    # false positive appears; never collapse it to a bare "*.local" branch.
    *.example.local|example.local|*.test.local|test.local|*.contoso.local|contoso.local) continue ;;
  esac
  echo "privacy-scan: $file:$line: non-reserved domain literal '$bare'" >&2
  VIOLATIONS=1
done < <(git grep -I -n -oiE "$DOMAIN_RE" -- "${SCOPE[@]}" 2>/dev/null || true)

SCOPE_FILE_COUNT="$(git ls-files -- "${SCOPE[@]}" | wc -l | tr -d ' ')"

if [ "$VIOLATIONS" = "0" ]; then
  echo "privacy-scan: Part A (reserved-range address/domain rule) clean over $SCOPE_FILE_COUNT files in scope."
fi

# ---------------------------------------------------------------------------
# Part B (D-15): reuse the existing customer/internal-name denylist hook in
# tree mode rather than reimplementing pattern loading and matching.
#
# A proving run — the one the phase gate actually relies on — supplies the
# real denylist and requires it be present:
#   REQUIRE_CUSTOMER_NAME_DENYLIST=1 CUSTOMER_NAME_SCAN_SCOPE=tree \
#     ./.githooks/pre-commit
# This script deliberately does not set REQUIRE_CUSTOMER_NAME_DENYLIST — a
# local run without the private denylist available must not fail merely
# because the private source is absent.
PRIVATE_PATTERN_SOURCE=0
if [ -n "${CUSTOMER_NAME_DENYLIST_B64:-}" ] || [ -n "${CUSTOMER_NAME_DENYLIST_FILE:-}" ] || \
   [ -n "${CUSTOMER_NAME_DENYLIST:-}" ] || [ -f "$REPO_ROOT/.githooks/customer-names.local.txt" ]; then
  PRIVATE_PATTERN_SOURCE=1
fi

if [ "$PRIVATE_PATTERN_SOURCE" = "0" ]; then
  echo "" >&2
  echo "! privacy-scan: no private customer-name denylist source is configured." >&2
  echo "! privacy-scan: the name half below ran against .githooks/customer-names.txt's" >&2
  echo "! privacy-scan: tracked generic patterns ONLY. A clean result is NOT proof of" >&2
  echo "! privacy-scan: compliance. The phase gate's proving run supplies the real list" >&2
  echo "! privacy-scan: via the CUSTOMER_NAME_DENYLIST_B64 CI variable." >&2
  echo "" >&2
fi

NAME_SCAN_STATUS=0
CUSTOMER_NAME_SCAN_SCOPE=tree "$REPO_ROOT/.githooks/pre-commit" || NAME_SCAN_STATUS=$?

if [ "$NAME_SCAN_STATUS" = "0" ]; then
  echo "privacy-scan: Part B (customer/internal-name denylist, tree mode) clean."
fi

if [ "$VIOLATIONS" != "0" ] || [ "$NAME_SCAN_STATUS" != "0" ]; then
  exit 1
fi

exit 0
