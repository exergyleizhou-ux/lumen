#!/usr/bin/env bash
# lumen-doctor.sh — comprehensive system diagnostic for Lumen on macOS
#
# Checks: OS, CPU, RAM, disk, network connectivity to API endpoints,
# Rust toolchain, git, Lumen config validity, and binary health.
# Outputs machine-readable JSON (--json) or human-friendly report.
#
# Usage:
#   ./scripts/lumen-doctor.sh           # human-readable report
#   ./scripts/lumen-doctor.sh --json    # machine-readable JSON
#   ./scripts/lumen-doctor.sh --quiet   # exit 0=healthy, 1=issues

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

MODE="${1:-report}"
ISSUES=0
CHECK_NAMES=()
CHECK_STATUSES=()
CHECK_DETAILS=()

check() {
    local name="$1" status="$2" detail="$3"
    CHECK_NAMES+=("$name")
    CHECK_STATUSES+=("$status")
    CHECK_DETAILS+=("$detail")
    if [[ "$status" == "FAIL" ]] || [[ "$status" == "WARN" ]]; then
        ISSUES=$((ISSUES + 1))
    fi
}

# ── OS ──
os_name=$(sw_vers -productName 2>/dev/null || echo "macOS")
os_ver=$(sw_vers -productVersion 2>/dev/null || echo "unknown")
os_build=$(sw_vers -buildVersion 2>/dev/null || echo "unknown")
arch=$(uname -m)
check "OS" "PASS" "${os_name} ${os_ver} (${os_build}) ${arch}"

# ── CPU ──
cpu_name=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || echo "unknown")
cpu_cores=$(sysctl -n hw.ncpu 2>/dev/null || echo "0")
cpu_phys=$(sysctl -n hw.physicalcpu 2>/dev/null || echo "0")
check "CPU" "PASS" "${cpu_name} (${cpu_phys}P/${cpu_cores}E cores)"

# ── RAM ──
ram_bytes=$(sysctl -n hw.memsize 2>/dev/null || echo "0")
ram_gb=$((ram_bytes / 1024 / 1024 / 1024))
ram_free=$(vm_stat 2>/dev/null | awk '/Pages free/ {print $3}' | sed 's/\.//' || echo "0")
if [[ "$ram_gb" -lt 8 ]]; then
    check "RAM" "WARN" "${ram_gb}GB (minimum 8GB recommended for compilation)"
else
    check "RAM" "PASS" "${ram_gb}GB"
fi

# ── Disk ──
disk_avail=$(df -g / | tail -1 | awk '{print $4}' 2>/dev/null || echo "0")
if [[ "$disk_avail" -lt 10 ]]; then
    check "Disk" "WARN" "${disk_avail}GB free (10GB+ recommended)"
elif [[ "$disk_avail" -lt 5 ]]; then
    check "Disk" "FAIL" "${disk_avail}GB free (insufficient for builds)"
else
    check "Disk" "PASS" "${disk_avail}GB free"
fi

# ── Rust ──
if command -v rustc &>/dev/null; then
    rust_ver=$(rustc --version 2>/dev/null | awk '{print $2}')
    cargo_ver=$(cargo --version 2>/dev/null | awk '{print $2}')
    check "Rust" "PASS" "rustc ${rust_ver}, cargo ${cargo_ver}"
else
    check "Rust" "FAIL" "Rust not installed. Install: curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
fi

# ── Git ──
if command -v git &>/dev/null; then
    git_ver=$(git --version 2>/dev/null | awk '{print $3}')
    check "Git" "PASS" "git ${git_ver}"
else
    check "Git" "FAIL" "Git not installed. Install: xcode-select --install"
fi

# ── OpenSSH ──
if command -v ssh &>/dev/null; then
    ssh_ver=$(ssh -V 2>&1 | head -1)
    check "SSH" "PASS" "${ssh_ver}"
else
    check "SSH" "WARN" "OpenSSH not found (needed for remote compute)"
fi

# ── Lumen Binary ──
LUMEN_BIN="${LUMEN_BIN:-$HOME/.lumen/bin/lumen}"
if [[ -x "$LUMEN_BIN" ]]; then
    lumen_ver=$("$LUMEN_BIN" --version 2>/dev/null || echo "unknown")
    check "Lumen" "PASS" "${lumen_ver}"
elif command -v lumen &>/dev/null; then
    lumen_ver=$(lumen --version 2>/dev/null || echo "unknown")
    check "Lumen" "PASS" "${lumen_ver} (PATH)"
else
    check "Lumen" "WARN" "Lumen binary not found. Build: cd ~/code/lumen/agent && cargo build --release"
fi

# ── Config ──
declare -a config_issues=()
for f in "$HOME/.lumen/config.toml" "$HOME/.grok/config.toml"; do
    if [[ -f "$f" ]]; then
        if grep -q "^default\s*=" "$f" 2>/dev/null; then
            default=$(grep "^default\s*=" "$f" | head -1 | sed 's/.*=\s*"\(.*\)".*/\1/')
            config_issues+=("$f: default=$default")
        else
            config_issues+=("$f: no default model set")
        fi
    else
        config_issues+=("$f: MISSING")
    fi
done
if [[ ${#config_issues[@]} -gt 0 ]]; then
    has_missing=false
    for msg in "${config_issues[@]}"; do
        if echo "$msg" | grep -q "MISSING"; then has_missing=true; fi
    done
    if $has_missing; then
        check "Config" "FAIL" "${config_issues[*]}"
    else
        check "Config" "WARN" "$(IFS='; '; echo "${config_issues[*]}")"
    fi
else
    check "Config" "PASS" "Both configs present with defaults set"
fi

# ── API Connectivity ──
for pair in "DeepSeek|https://api.deepseek.com/v1/models" "Kimi|https://api.kimi.com/coding/v1/models" "xAI|https://api.x.ai/v1/models"; do
    name="${pair%%|*}"
    url="${pair##*|}"
    if curl -s --connect-timeout 5 --max-time 10 -o /dev/null -w "%{http_code}" "$url" 2>/dev/null | grep -qE "^(200|401|403)$"; then
        check "API:${name}" "PASS" "${url} reachable"
    else
        check "API:${name}" "WARN" "${url} unreachable (check network/proxy)"
    fi
done

# ── Firewall (macOS) ──
fw_status=$(/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate 2>/dev/null | grep -o "enabled\|disabled" || echo "unknown")
if [[ "$fw_status" == "enabled" ]]; then
    check "Firewall" "PASS" "macOS firewall enabled"
else
    check "Firewall" "WARN" "macOS firewall disabled"
fi

# ── GPU ──
if system_profiler SPDisplaysDataType 2>/dev/null | grep -q "Chipset Model"; then
    gpu=$(system_profiler SPDisplaysDataType 2>/dev/null | grep "Chipset Model" | head -1 | sed 's/.*: //')
    check "GPU" "PASS" "${gpu}"
else
    check "GPU" "WARN" "No dedicated GPU detected"
fi

# ── Output ──
output_report() {
    echo ""
    echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}  Lumen System Diagnostic${NC}"
    echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
    echo ""
    printf "  %-20s %s\n" "Component" "Status"
    printf "  %-20s %s\n" "--------------------" "------"
    local i
    for ((i=0; i<${#CHECK_NAMES[@]}; i++)); do
        local name="${CHECK_NAMES[$i]}"
        local status="${CHECK_STATUSES[$i]}"
        local detail="${CHECK_DETAILS[$i]}"
        local color=""
        case "$status" in
            PASS) color="$GREEN" ;;
            WARN) color="$YELLOW" ;;
            FAIL) color="$RED" ;;
        esac
        printf "  %-20s ${color}%-6s${NC} %s\n" "$name" "$status" "$detail"
    done
    echo ""
    echo -e "  ${BOLD}Total issues:${NC} ${ISSUES}"
    echo ""
    if [[ $ISSUES -eq 0 ]]; then
        echo -e "  ${GREEN}✓ System healthy — ready for Lumen${NC}"
    else
        echo -e "  ${YELLOW}⚠ Resolve the issues above for optimal experience${NC}"
    fi
}

output_json() {
    echo "{"
    echo "  \"os\": \"${os_name} ${os_ver}\","
    echo "  \"arch\": \"${arch}\","
    echo "  \"cpu\": \"${cpu_name}\","
    echo "  \"ram_gb\": ${ram_gb},"
    echo "  \"disk_free_gb\": ${disk_avail},"
    echo "  \"issues\": ${ISSUES},"
    echo "  \"checks\": {"
    local first=true
    local i
    for ((i=0; i<${#CHECK_NAMES[@]}; i++)); do
        local name="${CHECK_NAMES[$i]}"
        local status="${CHECK_STATUSES[$i]}"
        local detail="${CHECK_DETAILS[$i]}"
        $first && first=false || echo ","
        echo -n "    \"${name}\": {\"status\": \"${status}\", \"detail\": \"${detail}\"}"
    done
    echo ""
    echo "  }"
    echo "}"
}

case "$MODE" in
    --json)  output_json ;;
    --quiet) [[ $ISSUES -eq 0 ]] && exit 0 || exit 1 ;;
    *)       output_report ;;
esac
