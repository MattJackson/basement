#!/usr/bin/env bash
# postdeploy-smoke.sh — black-box health check for a live basement.
#
# Verifies the timing budgets that have regressed in the past:
#   - /api/v1/admin/clusters       <1s   (reads connections.json)
#   - /api/v1/admin/clusters/.../_test  ≤10s  (Garage v1 client timeout)
#   - /api/v1/admin/buckets        ≤4s   (per-cluster 3s deadline + overhead)
#   - /api/v1/admin/keys           ≤4s   (same)
# Plus auth round-trip, full bucket lifecycle (create/get/arm/delete),
# validation gates (empty alias, duplicate, missing confirm), and the
# 2026-05-19 favicon-cache regression.
#
# Usage:
#   ./scripts/postdeploy-smoke.sh
#   BASEMENT_URL=https://basement.example.com \
#     BASEMENT_USER=matthew BASEMENT_PASS=password \
#     ./scripts/postdeploy-smoke.sh
#   ./scripts/postdeploy-smoke.sh -v          # verbose curl
#   ./scripts/postdeploy-smoke.sh --no-color  # CI-friendly output
#
# Exit codes:
#   0  all checks passed
#   1  a check failed (first failure aborts)
#   2  bad invocation (missing dep, bad flag)

set -euo pipefail

# ---------- config ----------
BASEMENT_URL="${BASEMENT_URL:-https://basement.pq.io}"
BASEMENT_URL="${BASEMENT_URL%/}" # strip trailing slash
BASEMENT_USER="${BASEMENT_USER:-matthew}"
BASEMENT_PASS="${BASEMENT_PASS:-password}"

# Test resources are prefixed so partial-cleanup leftovers are obvious.
# Bucket aliases must match ^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$ — no
# underscores allowed by the API, so the prefix is "smoke-" not "_smoke_".
SMOKE_PREFIX="smoke"
RUN_ID="$(date +%s)-$$"

VERBOSE=0
USE_COLOR=1

# ---------- arg parsing ----------
for arg in "$@"; do
    case "$arg" in
        -v|--verbose) VERBOSE=1 ;;
        --no-color)   USE_COLOR=0 ;;
        -h|--help)
            sed -n '2,/^set -euo/p' "$0" | sed -n '2,$p' | grep -E '^#' | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            printf 'unknown flag: %s\n' "$arg" >&2
            exit 2
            ;;
    esac
done

# ---------- terminal detection / colors ----------
if [[ "$USE_COLOR" -eq 1 ]] && [[ -t 1 ]] && command -v tput >/dev/null 2>&1 && [[ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]]; then
    C_GREEN="$(tput setaf 2)"
    C_RED="$(tput setaf 1)"
    C_YELLOW="$(tput setaf 3)"
    C_DIM="$(tput dim)"
    C_BOLD="$(tput bold)"
    C_RESET="$(tput sgr0)"
else
    C_GREEN=""; C_RED=""; C_YELLOW=""; C_DIM=""; C_BOLD=""; C_RESET=""
fi

# ---------- dependency checks ----------
for cmd in curl jq awk; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        printf '%smissing required command: %s%s\n' "$C_RED" "$cmd" "$C_RESET" >&2
        exit 2
    fi
done

# ---------- state for cleanup trap ----------
TMPDIR_SMOKE="$(mktemp -d -t basement-smoke.XXXXXX)"
COOKIE_JAR="$TMPDIR_SMOKE/cookies"
CLEANUP_BUCKETS=() # entries: "cid|bid"

cleanup() {
    local exit_code=$?

    # Best-effort cleanup of any test buckets still hanging around.
    # We don't abort or change exit status if cleanup fails — the
    # real test result is what matters; cleanup is hygiene.
    if [[ ${#CLEANUP_BUCKETS[@]} -gt 0 ]] && [[ -s "$COOKIE_JAR" ]]; then
        printf '%s---%s cleanup (%d resource(s))\n' "$C_DIM" "$C_RESET" "${#CLEANUP_BUCKETS[@]}" >&2
        for entry in "${CLEANUP_BUCKETS[@]}"; do
            local cid="${entry%%|*}"
            local bid="${entry##*|}"
            cleanup_bucket "$cid" "$bid" || true
        done
    fi

    rm -rf "$TMPDIR_SMOKE"

    if [[ $exit_code -eq 0 ]]; then
        printf '%s%s✓ all checks passed%s\n' "$C_BOLD" "$C_GREEN" "$C_RESET"
    fi
    exit $exit_code
}
trap cleanup EXIT INT TERM

cleanup_bucket() {
    local cid="$1" bid="$2"
    local arm_body
    arm_body="$(curl -fsS -b "$COOKIE_JAR" -X POST \
        "$BASEMENT_URL/api/v1/admin/clusters/$cid/buckets/$bid/_arm-delete" \
        -H 'Content-Type: application/json' 2>/dev/null || true)"
    [[ -z "$arm_body" ]] && return 0
    local token
    token="$(printf '%s' "$arm_body" | jq -r '.token // empty')"
    [[ -z "$token" ]] && return 0
    curl -fsS -b "$COOKIE_JAR" -X DELETE \
        "$BASEMENT_URL/api/v1/admin/clusters/$cid/buckets/$bid" \
        -H 'Content-Type: application/json' \
        -H "X-Confirm-Delete: $token" \
        -o /dev/null 2>/dev/null || true
}

# ---------- helpers ----------
say_pass() { printf '%s✓%s %s %s(%.3fs)%s\n' "$C_GREEN" "$C_RESET" "$1" "$C_DIM" "$2" "$C_RESET"; }
say_fail() {
    printf '%s✗ %s%s\n' "$C_RED" "$1" "$C_RESET" >&2
    [[ -n "${2:-}" ]] && printf '  %s%s%s\n' "$C_DIM" "$2" "$C_RESET" >&2
    exit 1
}
say_info() { printf '%s%s%s\n' "$C_DIM" "$1" "$C_RESET"; }
say_section() { printf '\n%s%s%s\n' "$C_BOLD" "$1" "$C_RESET"; }

# Issue a curl request, capturing body + status + elapsed time.
# Args: METHOD URL [extra curl args...]
# Sets globals: HTTP_CODE, HTTP_BODY, HTTP_ELAPSED (seconds, float)
http_call() {
    local method="$1" url="$2"
    shift 2
    local body_file="$TMPDIR_SMOKE/body.$$"
    local meta_file="$TMPDIR_SMOKE/meta.$$"

    local curl_args=(-sS -o "$body_file" -w '%{http_code} %{time_total}\n' -X "$method" "$url")
    if [[ "$VERBOSE" -eq 1 ]]; then
        curl_args=(-v "${curl_args[@]}")
    fi
    if [[ "$method" == "POST" || "$method" == "PATCH" || "$method" == "PUT" || "$method" == "DELETE" ]]; then
        curl_args+=(-H 'Content-Type: application/json')
    fi

    # set +e for the curl call so we can capture and report errors
    # ourselves; curl with -sS prints to stderr on failure.
    set +e
    curl "${curl_args[@]}" "$@" >"$meta_file" 2>&1
    local rc=$?
    set -e
    if [[ $rc -ne 0 ]]; then
        HTTP_CODE="000"
        HTTP_BODY="$(cat "$meta_file" 2>/dev/null || true)"
        HTTP_ELAPSED="0"
        return 0
    fi

    read -r HTTP_CODE HTTP_ELAPSED <"$meta_file"
    HTTP_BODY="$(cat "$body_file" 2>/dev/null || true)"
    rm -f "$body_file" "$meta_file"
}

# assert_under BUDGET_SECONDS ELAPSED CHECK_NAME [extra-context]
# Uses awk to compare floats (bash can't). Returns 0 if within budget,
# fails the script (and prints the regression message) if over.
assert_under() {
    local budget="$1" elapsed="$2" name="$3" extra="${4:-}"
    local over
    over="$(awk -v b="$budget" -v e="$elapsed" 'BEGIN{ print (e > b) ? "1" : "0" }')"
    if [[ "$over" == "1" ]]; then
        say_fail "$name exceeded timing budget" \
            "expected ≤${budget}s, took ${elapsed}s — ${extra:-likely regression}"
    fi
}

assert_status() {
    local want="$1" got="$2" name="$3"
    if [[ "$got" != "$want" ]]; then
        say_fail "$name: expected HTTP $want, got HTTP $got" "body: $(printf '%s' "$HTTP_BODY" | head -c 300)"
    fi
}

# ---------- header ----------
say_info "basement post-deploy smoke test"
say_info "target: $BASEMENT_URL"
say_info "user:   $BASEMENT_USER"
say_info "run id: $RUN_ID"

# ============================================================
# 1. Reachability + version (budget 2s)
# ============================================================
say_section "[1] reachability + version"
http_call GET "$BASEMENT_URL/api/v1/version"
assert_status 200 "$HTTP_CODE" "GET /api/v1/version"
assert_under 2.0 "$HTTP_ELAPSED" "version endpoint"

VERSION="$(printf '%s' "$HTTP_BODY" | jq -r '.version // empty')"
COMMIT="$(printf '%s' "$HTTP_BODY"  | jq -r '.commit  // empty')"
BUILT_AT="$(printf '%s' "$HTTP_BODY" | jq -r '.builtAt // empty')"
if [[ -z "$VERSION" || -z "$COMMIT" || -z "$BUILT_AT" ]]; then
    say_fail "version response missing fields" \
        "expected {version, commit, builtAt}, got: $HTTP_BODY"
fi
say_pass "GET /api/v1/version → $VERSION ($COMMIT, built $BUILT_AT)" "$HTTP_ELAPSED"

# ============================================================
# 2. Authentication (budget 3s combined)
# ============================================================
say_section "[2] authentication"

LOGIN_PAYLOAD="$(jq -nc --arg u "$BASEMENT_USER" --arg p "$BASEMENT_PASS" \
    '{username:$u, password:$p}')"

login_start="$(date +%s)"
http_call POST "$BASEMENT_URL/api/v1/auth/login" \
    -c "$COOKIE_JAR" \
    --data-raw "$LOGIN_PAYLOAD"
assert_status 200 "$HTTP_CODE" "POST /api/v1/auth/login"
LOGIN_ELAPSED="$HTTP_ELAPSED"

# Cookie jar should now contain __Host-basement_session.
if ! grep -q '__Host-basement_session' "$COOKIE_JAR" 2>/dev/null; then
    say_fail "login did not set __Host-basement_session cookie" \
        "cookie jar: $(cat "$COOKIE_JAR" 2>/dev/null | tail -n+5 | head -c 200)"
fi

http_call GET "$BASEMENT_URL/api/v1/auth/me" -b "$COOKIE_JAR"
assert_status 200 "$HTTP_CODE" "GET /api/v1/auth/me"
ME_USER="$(printf '%s' "$HTTP_BODY" | jq -r '.username // empty')"
ME_ROLE="$(printf '%s' "$HTTP_BODY" | jq -r '.role // empty')"
if [[ "$ME_USER" != "$BASEMENT_USER" || "$ME_ROLE" != "admin" ]]; then
    say_fail "/auth/me returned unexpected user/role" \
        "got user=$ME_USER role=$ME_ROLE want user=$BASEMENT_USER role=admin"
fi

login_end="$(date +%s)"
total_auth=$((login_end - login_start))
if [[ "$total_auth" -gt 3 ]]; then
    say_fail "auth round-trip exceeded budget" \
        "expected ≤3s, took ${total_auth}s — login alone was ${LOGIN_ELAPSED}s"
fi
say_pass "auth round-trip (login + /me) as $ME_USER ($ME_ROLE)" "$LOGIN_ELAPSED"

# ============================================================
# 3. Clusters list — must be FAST (budget <1s)
# ============================================================
say_section "[3] clusters list (connections.json read)"
http_call GET "$BASEMENT_URL/api/v1/admin/clusters" -b "$COOKIE_JAR"
assert_status 200 "$HTTP_CODE" "GET /api/v1/admin/clusters"
assert_under 1.0 "$HTTP_ELAPSED" "GET /api/v1/admin/clusters" \
    "should be a flat connections.json read — something is blocking the handler"

if ! printf '%s' "$HTTP_BODY" | jq -e 'type == "array"' >/dev/null 2>&1; then
    say_fail "/admin/clusters did not return a JSON array" \
        "body: $(printf '%s' "$HTTP_BODY" | head -c 300)"
fi

NUM_CLUSTERS="$(printf '%s' "$HTTP_BODY" | jq 'length')"
say_pass "GET /api/v1/admin/clusters → $NUM_CLUSTERS connection(s)" "$HTTP_ELAPSED"

if [[ "$NUM_CLUSTERS" -eq 0 ]]; then
    say_info "no connections configured — skipping per-cluster + lifecycle checks"
    SKIP_CLUSTER_CHECKS=1
else
    SKIP_CLUSTER_CHECKS=0
    # Stash cluster JSON for later use.
    printf '%s' "$HTTP_BODY" > "$TMPDIR_SMOKE/clusters.json"
fi

# ============================================================
# 4. Per-cluster _test (budget ≤10s, matches Garage v1 client timeout)
# ============================================================
HEALTHY_CID=""
if [[ "$SKIP_CLUSTER_CHECKS" -eq 0 ]]; then
    say_section "[4] per-cluster _test (Garage client timeout boundary)"
    FIRST_CID="$(jq -r '.[0].id' "$TMPDIR_SMOKE/clusters.json")"
    FIRST_LABEL="$(jq -r '.[0].label' "$TMPDIR_SMOKE/clusters.json")"

    http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$FIRST_CID/_test" -b "$COOKIE_JAR"
    assert_status 200 "$HTTP_CODE" "POST /api/v1/admin/clusters/$FIRST_CID/_test"
    assert_under 10.0 "$HTTP_ELAPSED" "_test for cluster $FIRST_LABEL" \
        "Garage v1 client timeout regressed — should fail within 10s"

    TEST_OK="$(printf '%s' "$HTTP_BODY" | jq -r '.ok')"
    TEST_MSG="$(printf '%s' "$HTTP_BODY" | jq -r '.message // empty')"
    if [[ "$TEST_OK" == "true" ]]; then
        HEALTHY_CID="$FIRST_CID"
        say_pass "POST /_test for $FIRST_LABEL → ok:true ($TEST_MSG)" "$HTTP_ELAPSED"
    else
        # ok:false is acceptable — the budget test is what matters.
        say_pass "POST /_test for $FIRST_LABEL → ok:false ($TEST_MSG) — within budget" "$HTTP_ELAPSED"
        # Look for a healthy cluster among the rest.
        OTHER_CIDS="$(jq -r '.[1:] | .[].id' "$TMPDIR_SMOKE/clusters.json")"
        for cid in $OTHER_CIDS; do
            http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$cid/_test" -b "$COOKIE_JAR"
            if [[ "$HTTP_CODE" == "200" ]] && [[ "$(printf '%s' "$HTTP_BODY" | jq -r '.ok')" == "true" ]]; then
                HEALTHY_CID="$cid"
                say_info "  found healthy cluster: $cid"
                break
            fi
        done
    fi
fi

# ============================================================
# 5. Cross-cluster aggregated reads (budget ≤4s each)
# ============================================================
say_section "[5] cross-cluster aggregated reads"

http_call GET "$BASEMENT_URL/api/v1/admin/buckets" -b "$COOKIE_JAR"
assert_status 200 "$HTTP_CODE" "GET /api/v1/admin/buckets"
assert_under 4.0 "$HTTP_ELAPSED" "GET /api/v1/admin/buckets" \
    "3s per-cluster deadline + overhead — even one stalled cluster should not push past 4s"

# Verify shape: {buckets: [...]} present. (errors is omitempty so we
# only assert it's an array IF present.)
if ! printf '%s' "$HTTP_BODY" | jq -e '.buckets | type == "array"' >/dev/null 2>&1; then
    say_fail "/admin/buckets missing buckets[] array" \
        "body: $(printf '%s' "$HTTP_BODY" | head -c 300)"
fi
if printf '%s' "$HTTP_BODY" | jq -e 'has("errors")' >/dev/null 2>&1; then
    if ! printf '%s' "$HTTP_BODY" | jq -e '.errors | type == "array"' >/dev/null 2>&1; then
        say_fail "/admin/buckets errors field is not an array"
    fi
fi
NUM_BUCKETS="$(printf '%s' "$HTTP_BODY" | jq '.buckets | length')"
NUM_BUCKET_ERRORS="$(printf '%s' "$HTTP_BODY" | jq '.errors // [] | length')"
say_pass "GET /api/v1/admin/buckets → $NUM_BUCKETS bucket(s), $NUM_BUCKET_ERRORS error(s)" "$HTTP_ELAPSED"

http_call GET "$BASEMENT_URL/api/v1/admin/keys" -b "$COOKIE_JAR"
assert_status 200 "$HTTP_CODE" "GET /api/v1/admin/keys"
assert_under 4.0 "$HTTP_ELAPSED" "GET /api/v1/admin/keys" \
    "3s per-cluster deadline + overhead"

if ! printf '%s' "$HTTP_BODY" | jq -e '.keys | type == "array"' >/dev/null 2>&1; then
    say_fail "/admin/keys missing keys[] array" \
        "body: $(printf '%s' "$HTTP_BODY" | head -c 300)"
fi
if printf '%s' "$HTTP_BODY" | jq -e 'has("errors")' >/dev/null 2>&1; then
    if ! printf '%s' "$HTTP_BODY" | jq -e '.errors | type == "array"' >/dev/null 2>&1; then
        say_fail "/admin/keys errors field is not an array"
    fi
fi
NUM_KEYS="$(printf '%s' "$HTTP_BODY" | jq '.keys | length')"
NUM_KEY_ERRORS="$(printf '%s' "$HTTP_BODY" | jq '.errors // [] | length')"
say_pass "GET /api/v1/admin/keys → $NUM_KEYS key(s), $NUM_KEY_ERRORS error(s)" "$HTTP_ELAPSED"

# ============================================================
# 6. Bucket lifecycle (create + get + arm + delete + verify-404)
# ============================================================
if [[ -z "$HEALTHY_CID" ]]; then
    say_section "[6] bucket lifecycle"
    say_info "skipped — no healthy cluster available"
else
    say_section "[6] bucket lifecycle on cluster $HEALTHY_CID"
    ALIAS="${SMOKE_PREFIX}-life-${RUN_ID}"

    # Create
    http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets" \
        -b "$COOKIE_JAR" \
        --data-raw "$(jq -nc --arg a "$ALIAS" '{alias:$a}')"
    assert_status 201 "$HTTP_CODE" "create bucket $ALIAS"
    BID="$(printf '%s' "$HTTP_BODY" | jq -r '.id // empty')"
    if [[ -z "$BID" ]]; then
        say_fail "create bucket: response missing id" "body: $HTTP_BODY"
    fi
    CLEANUP_BUCKETS+=("$HEALTHY_CID|$BID")
    say_pass "POST .../buckets {alias:$ALIAS} → 201 id=$BID" "$HTTP_ELAPSED"

    # Get
    http_call GET "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets/$BID" -b "$COOKIE_JAR"
    assert_status 200 "$HTTP_CODE" "get bucket $BID"
    GOT_ID="$(printf '%s' "$HTTP_BODY" | jq -r '.id // empty')"
    if [[ "$GOT_ID" != "$BID" ]]; then
        say_fail "get bucket returned wrong id" "got=$GOT_ID want=$BID"
    fi
    say_pass "GET .../buckets/$BID → 200" "$HTTP_ELAPSED"

    # Arm delete
    http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets/$BID/_arm-delete" -b "$COOKIE_JAR"
    assert_status 200 "$HTTP_CODE" "arm-delete bucket $BID"
    DEL_TOKEN="$(printf '%s' "$HTTP_BODY" | jq -r '.token // empty')"
    if [[ -z "$DEL_TOKEN" ]]; then
        say_fail "arm-delete: response missing token" "body: $HTTP_BODY"
    fi
    say_pass "POST .../buckets/$BID/_arm-delete → 200 (token issued)" "$HTTP_ELAPSED"

    # Delete with token
    http_call DELETE "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets/$BID" \
        -b "$COOKIE_JAR" \
        -H "X-Confirm-Delete: $DEL_TOKEN"
    assert_status 200 "$HTTP_CODE" "delete bucket $BID with confirm token"
    say_pass "DELETE .../buckets/$BID (with token) → 200" "$HTTP_ELAPSED"

    # Verify gone
    http_call GET "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets/$BID" -b "$COOKIE_JAR"
    if [[ "$HTTP_CODE" != "404" ]]; then
        say_fail "deleted bucket still returns HTTP $HTTP_CODE (expected 404)" \
            "body: $(printf '%s' "$HTTP_BODY" | head -c 200)"
    fi
    # Successful delete — remove from cleanup list.
    new_cleanup=()
    for entry in "${CLEANUP_BUCKETS[@]}"; do
        [[ "$entry" != "$HEALTHY_CID|$BID" ]] && new_cleanup+=("$entry")
    done
    CLEANUP_BUCKETS=("${new_cleanup[@]:-}")
    # Re-prune empty placeholder slot from array reset above.
    if [[ "${#CLEANUP_BUCKETS[@]}" -eq 1 && -z "${CLEANUP_BUCKETS[0]}" ]]; then
        CLEANUP_BUCKETS=()
    fi
    say_pass "GET .../buckets/$BID after delete → 404" "$HTTP_ELAPSED"
fi

# ============================================================
# 7. Validation gates
# ============================================================
say_section "[7] validation gates"

if [[ -z "$HEALTHY_CID" ]]; then
    say_info "skipped — no healthy cluster available"
else
    # 7a. Empty alias → 400 ALIAS_REQUIRED
    http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets" \
        -b "$COOKIE_JAR" --data-raw '{"alias":""}'
    assert_status 400 "$HTTP_CODE" "empty alias rejection"
    CODE="$(printf '%s' "$HTTP_BODY" | jq -r '.error.code // empty')"
    if [[ "$CODE" != "ALIAS_REQUIRED" ]]; then
        say_fail "empty alias: expected code ALIAS_REQUIRED, got $CODE" "body: $HTTP_BODY"
    fi
    say_pass "empty alias → 400 ALIAS_REQUIRED" "$HTTP_ELAPSED"

    # 7b. Duplicate alias → first 201, second 409 DUPLICATE_ALIAS, then cleanup
    DUP_ALIAS="${SMOKE_PREFIX}-dup-${RUN_ID}"
    http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets" \
        -b "$COOKIE_JAR" \
        --data-raw "$(jq -nc --arg a "$DUP_ALIAS" '{alias:$a}')"
    assert_status 201 "$HTTP_CODE" "create first duplicate-test bucket"
    DUP_BID="$(printf '%s' "$HTTP_BODY" | jq -r '.id // empty')"
    [[ -z "$DUP_BID" ]] && say_fail "duplicate-test: first create returned no id"
    CLEANUP_BUCKETS+=("$HEALTHY_CID|$DUP_BID")

    http_call POST "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets" \
        -b "$COOKIE_JAR" \
        --data-raw "$(jq -nc --arg a "$DUP_ALIAS" '{alias:$a}')"
    assert_status 409 "$HTTP_CODE" "duplicate alias rejection"
    CODE="$(printf '%s' "$HTTP_BODY" | jq -r '.error.code // empty')"
    if [[ "$CODE" != "DUPLICATE_ALIAS" ]]; then
        say_fail "duplicate alias: expected code DUPLICATE_ALIAS, got $CODE" "body: $HTTP_BODY"
    fi
    say_pass "duplicate alias → 409 DUPLICATE_ALIAS" "$HTTP_ELAPSED"

    # Clean up the duplicate-test bucket now (also covered by trap fallback)
    cleanup_bucket "$HEALTHY_CID" "$DUP_BID"
    new_cleanup=()
    for entry in "${CLEANUP_BUCKETS[@]}"; do
        [[ "$entry" != "$HEALTHY_CID|$DUP_BID" ]] && new_cleanup+=("$entry")
    done
    CLEANUP_BUCKETS=("${new_cleanup[@]:-}")
    if [[ "${#CLEANUP_BUCKETS[@]}" -eq 1 && -z "${CLEANUP_BUCKETS[0]}" ]]; then
        CLEANUP_BUCKETS=()
    fi

    # 7c. DELETE without X-Confirm-Delete → 400 CONFIRMATION_REQUIRED.
    # Use a fake (but realistic-looking) bucket id — handler checks
    # the header BEFORE looking up the bucket, so 400 wins over 404.
    http_call DELETE "$BASEMENT_URL/api/v1/admin/clusters/$HEALTHY_CID/buckets/notarealbucketjustchecking" \
        -b "$COOKIE_JAR"
    assert_status 400 "$HTTP_CODE" "delete without X-Confirm-Delete"
    CODE="$(printf '%s' "$HTTP_BODY" | jq -r '.error.code // empty')"
    if [[ "$CODE" != "CONFIRMATION_REQUIRED" ]]; then
        say_fail "missing X-Confirm-Delete: expected code CONFIRMATION_REQUIRED, got $CODE" \
            "body: $HTTP_BODY"
    fi
    say_pass "delete without X-Confirm-Delete → 400 CONFIRMATION_REQUIRED" "$HTTP_ELAPSED"
fi

# ============================================================
# 8. Static asset cache headers (2026-05-19 favicon-cache incident)
# ============================================================
say_section "[8] static asset cache headers"

# Favicon: short cache, must-revalidate (NOT immutable).
FAVICON_HEADERS="$(curl -fsSI "$BASEMENT_URL/favicon.svg" 2>/dev/null || true)"
if [[ -z "$FAVICON_HEADERS" ]]; then
    say_fail "could not fetch /favicon.svg headers"
fi
FAVICON_CC="$(printf '%s' "$FAVICON_HEADERS" | awk -F': ' 'tolower($1)=="cache-control"{$1=""; sub(/^ /,""); print; exit}' | tr -d '\r')"
if [[ "$FAVICON_CC" != *"max-age=3600"* ]] || [[ "$FAVICON_CC" != *"must-revalidate"* ]]; then
    say_fail "/favicon.svg has wrong Cache-Control" \
        "got: '$FAVICON_CC' — expected 'public, max-age=3600, must-revalidate' (see 2026-05-19 favicon incident)"
fi
if [[ "$FAVICON_CC" == *"immutable"* ]]; then
    say_fail "/favicon.svg is marked immutable — REGRESSED to favicon-cache incident" \
        "Cache-Control: $FAVICON_CC"
fi
say_pass "/favicon.svg Cache-Control: $FAVICON_CC" "0.0"

# Hashed asset under /assets/: long immutable cache.
INDEX_HTML="$(curl -fsS "$BASEMENT_URL/" 2>/dev/null || true)"
HASHED_ASSET="$(printf '%s' "$INDEX_HTML" | grep -oE '/assets/[^"]+\.(js|css)' | head -n 1 || true)"
if [[ -z "$HASHED_ASSET" ]]; then
    say_fail "could not find a hashed /assets/* bundle in index HTML" \
        "is the SPA built and embedded? this is what dist/ contains."
fi
ASSET_HEADERS="$(curl -fsSI "$BASEMENT_URL$HASHED_ASSET" 2>/dev/null || true)"
if [[ -z "$ASSET_HEADERS" ]]; then
    say_fail "could not fetch $HASHED_ASSET headers"
fi
ASSET_CC="$(printf '%s' "$ASSET_HEADERS" | awk -F': ' 'tolower($1)=="cache-control"{$1=""; sub(/^ /,""); print; exit}' | tr -d '\r')"
if [[ "$ASSET_CC" != *"immutable"* ]]; then
    say_fail "$HASHED_ASSET missing 'immutable' in Cache-Control" \
        "got: '$ASSET_CC' — content-hashed assets should be public, max-age=31536000, immutable"
fi
say_pass "$HASHED_ASSET Cache-Control: $ASSET_CC" "0.0"

# Exit handled by trap.
