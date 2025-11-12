#!/usr/bin/env bash
set -euo pipefail

pkgs=(
    ./internal/worker/messaging
    ./internal/observability/metrics
)

threshold=90
GO_BIN=${GO_BIN:-$(command -v go 2>/dev/null || command -v go.exe 2>/dev/null)}
if [[ -z "$GO_BIN" ]]; then
    echo "go binary not found" >&2
    exit 1
fi

fail=0
for pkg in "${pkgs[@]}"; do
    tmp="coverage_${pkg##*/}.out"
    trap 'rm -f "$tmp"' EXIT
    "$GO_BIN" test -coverprofile="$tmp" -covermode=atomic "$pkg" >/dev/null
    pct=$("$GO_BIN" tool cover -func="$tmp" | awk '/total:/ {print $3}' | tr -d '%')
    rm -f "$tmp"
    if [[ -z "$pct" ]]; then
        echo "No coverage data for $pkg" >&2
        fail=1
    elif (( ${pct%.*} < threshold )); then
        echo "Coverage for $pkg below ${threshold}%: $pct" >&2
        fail=1
    else
        echo "Coverage for $pkg: $pct%"
    fi
done

exit $fail
