#!/usr/bin/env bash
# lumen-credential.sh — manage Lumen API keys via macOS Keychain
#
# Stores API keys encrypted at rest using the macOS Keychain.
# Supported providers: deepseek, kimi, openai, anthropic, grok (xai)
#
# Usage:
#   ./scripts/lumen-credential.sh set deepseek    # prompt for key
#   ./scripts/lumen-credential.sh set deepseek KEY # provide key directly
#   ./scripts/lumen-credential.sh get deepseek     # retrieve key (never prints)
#   ./scripts/lumen-credential.sh list             # list stored providers
#   ./scripts/lumen-credential.sh remove deepseek  # delete key

set -euo pipefail

SERVICE_PREFIX="com.lumen.api"

cmd_set() {
    local provider="${1:?provider required (deepseek, kimi, openai, anthropic, grok)}"
    local key="${2:-}"
    if [[ -z "$key" ]]; then
        read -rsp "Enter API key for ${provider}: " key
        echo ""
    fi
    security add-generic-password -a "$USER" -s "${SERVICE_PREFIX}.${provider}" -w "$key" -U 2>/dev/null
    echo "✓ API key stored for ${provider} (Keychain: ${SERVICE_PREFIX}.${provider})"
}

cmd_get() {
    local provider="${1:?provider required}"
    security find-generic-password -a "$USER" -s "${SERVICE_PREFIX}.${provider}" -w 2>/dev/null || {
        echo "✗ No key found for ${provider}"
        exit 1
    }
}

cmd_list() {
    echo "Stored Lumen API keys:"
    security find-generic-password -s "${SERVICE_PREFIX}." 2>/dev/null | grep "svce" | sed 's/.*"svce".*"\(.*\)".*/\1/' | sed "s/${SERVICE_PREFIX}\.//" | while read -r provider; do
        echo "  ${provider}"
    done
    if [[ ${PIPESTATUS[0]} -ne 0 ]]; then
        echo "  (none)"
    fi
}

cmd_remove() {
    local provider="${1:?provider required}"
    security delete-generic-password -a "$USER" -s "${SERVICE_PREFIX}.${provider}" 2>/dev/null && \
        echo "✓ Removed key for ${provider}" || \
        echo "✗ No key found for ${provider}"
}

case "${1:-}" in
    set)    shift; cmd_set "$@" ;;
    get)    shift; cmd_get "$@" ;;
    list)   cmd_list ;;
    remove) shift; cmd_remove "$@" ;;
    *)      echo "Usage: $0 {set|get|list|remove} [provider] [key]"; exit 1 ;;
esac
