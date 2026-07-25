#!/usr/bin/env bash
# lumen-bench.sh — Lumen performance benchmark for macOS
#
# Benchmarks: CPU (primes/sec), memory bandwidth, disk I/O, network latency.
# Produces a composite score (0-100) with rating.
#
# Usage:
#   ./scripts/lumen-bench.sh             # quick benchmark
#   ./scripts/lumen-bench.sh --full      # extended benchmark (longer)
#   ./scripts/lumen-bench.sh --json      # machine-readable output

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

MODE="${1:-quick}"
SCORES=()

bench_cpu() {
    echo -e "  ${CYAN}CPU: computing primes...${NC}"
    local primes=0
    local start=$(python3 -c 'import time; print(time.time())' 2>/dev/null || echo "0")
    for ((i=2; i<50000; i++)); do
        local is_prime=1
        for ((j=2; j*j<=i; j++)); do
            if ((i % j == 0)); then is_prime=0; break; fi
        done
        ((is_prime)) && ((primes++))
    done
    local end=$(python3 -c 'import time; print(time.time())' 2>/dev/null || echo "0")
    local elapsed=$(echo "$end - $start" | bc 2>/dev/null || echo "1")
    local rate=$(echo "$primes / $elapsed" | bc 2>/dev/null || echo "0")
    echo "  CPU: ${rate} primes/sec (${primes} primes in ${elapsed}s)"
    # Score: normalize to ~10000 primes/sec baseline
    local score=$(echo "scale=0; ($rate / 100) + 0.5" | bc 2>/dev/null || echo "0")
    if [[ "$score" -gt 100 ]]; then score=100; fi
    SCORES+=("$score")
}

bench_memory() {
    echo -e "  ${CYAN}Memory: testing bandwidth...${NC}"
    # Write 100MB to /dev/null via dd as a rough bandwidth test
    local result=$(dd if=/dev/zero of=/dev/null bs=1m count=100 2>&1 | tail -1)
    local rate=$(echo "$result" | grep -oE '[0-9]+' | head -1 || echo "0")
    echo "  Memory: ~${rate} MB/s write bandwidth"
    local score=$(echo "scale=0; ($rate / 50) + 0.5" | bc 2>/dev/null || echo "0")
    if [[ "$score" -gt 100 ]]; then score=100; fi
    SCORES+=("$score")
}

bench_disk() {
    echo -e "  ${CYAN}Disk: testing I/O...${NC}"
    local file="/tmp/lumen-bench-$$.tmp"
    local result=$(dd if=/dev/zero of="$file" bs=1m count=50 2>&1 | tail -1)
    rm -f "$file"
    local rate=$(echo "$result" | grep -oE '[0-9]+' | head -1 || echo "0")
    echo "  Disk: ~${rate} MB/s write"
    local score=$(echo "scale=0; ($rate / 20) + 0.5" | bc 2>/dev/null || echo "0")
    if [[ "$score" -gt 100 ]]; then score=100; fi
    SCORES+=("$score")
}

bench_network() {
    echo -e "  ${CYAN}Network: testing API latency...${NC}"
    local total=0 count=0
    for url in "https://api.deepseek.com" "https://api.kimi.com" "https://api.x.ai"; do
        local latency=$(curl -s -o /dev/null -w "%{time_total}" --connect-timeout 5 "$url" 2>/dev/null || echo "5000")
        local ms=$(echo "$latency * 1000" | bc 2>/dev/null | cut -d. -f1 || echo "5000")
        echo "  ${url##https://}: ${ms}ms"
        total=$((total + ms))
        count=$((count + 1))
    done
    local avg=$((total / count))
    echo "  Network: avg ${avg}ms"
    # Score: 100 for <50ms, 0 for >2000ms
    local score=$(echo "scale=0; 100 - ($avg / 20) + 0.5" | bc 2>/dev/null || echo "0")
    if [[ "$score" -lt 0 ]]; then score=0; fi
    if [[ "$score" -gt 100 ]]; then score=100; fi
    SCORES+=("$score")
}

compute_composite() {
    local sum=0
    for s in "${SCORES[@]}"; do sum=$((sum + s)); done
    echo $((sum / ${#SCORES[@]}))
}

echo ""
echo -e "${BOLD}══════════════════════════════════════════${NC}"
echo -e "${BOLD}  Lumen Performance Benchmark${NC}"
echo -e "${BOLD}══════════════════════════════════════════${NC}"
echo ""

bench_cpu
bench_memory
bench_disk
bench_network

composite=$(compute_composite)
echo ""
echo -e "${BOLD}──────────────────────────────────────────${NC}"
case "$composite" in
    [8-9][0-9]|100) rating="${GREEN}Excellent${NC}" ;;
    [6-7][0-9])     rating="${GREEN}Good${NC}" ;;
    [4-5][0-9])     rating="${YELLOW}Average${NC}" ;;
    *)              rating="${YELLOW}Below Average${NC}" ;;
esac
echo -e "  ${BOLD}Composite Score:${NC} ${composite}/100 — ${rating}"
echo ""

if [[ "$MODE" == "--json" ]]; then
    echo "{"
    echo "  \"composite\": ${composite},"
    echo "  \"cpu\": ${SCORES[0]},"
    echo "  \"memory\": ${SCORES[1]},"
    echo "  \"disk\": ${SCORES[2]},"
    echo "  \"network\": ${SCORES[3]}"
    echo "}"
fi
