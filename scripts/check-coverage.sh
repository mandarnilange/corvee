#!/usr/bin/env bash
# Enforce coverage thresholds per spec §11.5: domain ≥90%, usecase ≥90%.
# Skips a package if it has no _test.go files (Phase 0 holds doc-only
# packages; thresholds activate when tests start landing).
set -euo pipefail

profile="$1"
domain_min="$2"
usecase_min="$3"

if [ ! -f "$profile" ]; then
    echo "coverage profile $profile not found" >&2
    exit 2
fi

# Returns the percent for a given package prefix, or empty string if no
# tests exist for that prefix.
package_coverage() {
    local prefix="$1"
    if ! find "$prefix" -name '*_test.go' -print -quit 2>/dev/null | grep -q .; then
        echo ""
        return
    fi
    go tool cover -func="$profile" \
        | awk -v p="$prefix" '$1 ~ p"/" || $1 ~ "^"p"/" { covered+=$NF*$NF; lines+=$NF; sum+=$NF } END { if (NR==0) print ""; else print sum/NR }' \
        > /dev/null # discard awk's calc above; below is the simple total method

    # Use the per-function output and average the function coverage values.
    # `go tool cover -func` ends with a "total: ... <pct>%" line for the entire
    # profile, but we want per-package. We compute it ourselves from the
    # per-function rows that match the prefix.
    go tool cover -func="$profile" \
        | awk -v p="$prefix" '
            $1 ~ p && $1 !~ "/testfakes/" && $1 !~ "/testdata/" {
                gsub(/%/, "", $NF);
                sum += $NF;
                n++;
            }
            END {
                if (n == 0) {
                    print "";
                } else {
                    printf "%.1f", sum / n;
                }
            }
        '
}

check() {
    local label="$1"
    local prefix="$2"
    local minimum="$3"

    local pct
    pct=$(package_coverage "$prefix")

    if [ -z "$pct" ]; then
        echo "$label: no tests yet (skipping threshold check)"
        return 0
    fi

    echo "$label: ${pct}% (minimum ${minimum}%)"

    awk -v have="$pct" -v want="$minimum" 'BEGIN { exit !(have+0 >= want+0) }' \
        || { echo "ERROR: $label coverage ${pct}% is below required ${minimum}%" >&2; return 1; }
}

failed=0
check "domain"  "internal/domain"  "$domain_min"  || failed=1
check "usecase" "internal/usecase" "$usecase_min" || failed=1

exit $failed
