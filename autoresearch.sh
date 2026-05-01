#!/bin/bash
set -euo pipefail

# Run benchmarks multiple times for stability and report medians
cd "$(dirname "$0")"

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

# Run quiet benchmarks (no log spam) 5 times each
go test -bench='BenchmarkRouteHelloJSONQuiet|BenchmarkRouteStringQuiet|BenchmarkRouteWithMiddlewareQuiet|BenchmarkHubFanoutSmallQuiet|BenchmarkHubFanoutLargeQuiet|BenchmarkHubSubscribeUnsubscribeQuiet|BenchmarkSSEWriteEventQuiet' \
  -benchmem -run='^$' -count=5 . > "$TMPFILE" 2>/dev/null

# Parse median values using benchstat-like logic (simple median of 5 runs)
parse_bench() {
  local name="$1"
  local file="$2"
  grep "^${name}-" "$file" | awk '{print $3}' | sort -n | sed -n '3p'
}

parse_allocs() {
  local name="$1"
  local file="$2"
  grep "^${name}-" "$file" | awk '{print $7}' | sort -n | sed -n '3p'
}

parse_bytes() {
  local name="$1"
  local file="$2"
  grep "^${name}-" "$file" | awk '{print $5}' | sort -n | sed -n '3p'
}

ROUTE_JSON_NS=$(parse_bench BenchmarkRouteHelloJSONQuiet "$TMPFILE")
ROUTE_STR_NS=$(parse_bench BenchmarkRouteStringQuiet "$TMPFILE")
ROUTE_MW_NS=$(parse_bench BenchmarkRouteWithMiddlewareQuiet "$TMPFILE")
HUB_SMALL_NS=$(parse_bench BenchmarkHubFanoutSmallQuiet "$TMPFILE")
HUB_LARGE_NS=$(parse_bench BenchmarkHubFanoutLargeQuiet "$TMPFILE")
HUB_SUB_NS=$(parse_bench BenchmarkHubSubscribeUnsubscribeQuiet "$TMPFILE")
SSE_NS=$(parse_bench BenchmarkSSEWriteEventQuiet "$TMPFILE")

ROUTE_JSON_ALLOCS=$(parse_allocs BenchmarkRouteHelloJSONQuiet "$TMPFILE")
ROUTE_STR_ALLOCS=$(parse_allocs BenchmarkRouteStringQuiet "$TMPFILE")
ROUTE_MW_ALLOCS=$(parse_allocs BenchmarkRouteWithMiddlewareQuiet "$TMPFILE")
HUB_SMALL_ALLOCS=$(parse_allocs BenchmarkHubFanoutSmallQuiet "$TMPFILE")
HUB_LARGE_ALLOCS=$(parse_allocs BenchmarkHubFanoutLargeQuiet "$TMPFILE")
HUB_SUB_ALLOCS=$(parse_allocs BenchmarkHubSubscribeUnsubscribeQuiet "$TMPFILE")
SSE_ALLOCS=$(parse_allocs BenchmarkSSEWriteEventQuiet "$TMPFILE")

# Composite primary metric: geometric mean of route ns, scaled to avoid tiny numbers
# Using integer math via awk
PRIMARY=$(awk "BEGIN {printf \"%.0f\", (($ROUTE_JSON_NS * $ROUTE_STR_NS * $ROUTE_MW_NS)^(1/3))}")

# Hub composite
HUB_PRIMARY=$(awk "BEGIN {printf \"%.0f\", (($HUB_SMALL_NS * $HUB_LARGE_NS)^(1/2))}")

# Total route allocs
ROUTE_ALLOCS=$((ROUTE_JSON_ALLOCS + ROUTE_STR_ALLOCS + ROUTE_MW_ALLOCS))

# Total hub allocs
HUB_ALLOCS=$((HUB_SMALL_ALLOCS + HUB_LARGE_ALLOCS))

echo "METRIC route_ns=${PRIMARY}"
echo "METRIC hub_ns=${HUB_PRIMARY}"
echo "METRIC route_allocs=${ROUTE_ALLOCS}"
echo "METRIC hub_allocs=${HUB_ALLOCS}"
echo "METRIC route_json_ns=${ROUTE_JSON_NS}"
echo "METRIC route_str_ns=${ROUTE_STR_NS}"
echo "METRIC route_mw_ns=${ROUTE_MW_NS}"
echo "METRIC hub_small_ns=${HUB_SMALL_NS}"
echo "METRIC hub_large_ns=${HUB_LARGE_NS}"
echo "METRIC hub_sub_ns=${HUB_SUB_NS}"
echo "METRIC sse_ns=${SSE_NS}"
echo "METRIC route_json_allocs=${ROUTE_JSON_ALLOCS}"
echo "METRIC route_str_allocs=${ROUTE_STR_ALLOCS}"
echo "METRIC route_mw_allocs=${ROUTE_MW_ALLOCS}"
echo "METRIC hub_small_allocs=${HUB_SMALL_ALLOCS}"
echo "METRIC hub_large_allocs=${HUB_LARGE_ALLOCS}"
echo "METRIC hub_sub_allocs=${HUB_SUB_ALLOCS}"
echo "METRIC sse_allocs=${SSE_ALLOCS}"
