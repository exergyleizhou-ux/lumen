#!/usr/bin/env bash
# lumen-service.sh — install Lumen Science Server as a macOS LaunchAgent
#
# Creates a ~/Library/LaunchAgents/com.lumen.science.plist that starts
# the Lumen Science Server on login and restarts it on failure.
#
# Usage:
#   ./scripts/lumen-service.sh install     # install LaunchAgent
#   ./scripts/lumen-service.sh start       # start the service
#   ./scripts/lumen-service.sh stop        # stop the service
#   ./scripts/lumen-service.sh restart     # restart the service
#   ./scripts/lumen-service.sh status      # check if running
#   ./scripts/lumen-service.sh remove      # uninstall LaunchAgent

set -euo pipefail

LABEL="com.lumen.science"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"
LUMEN_DIR="$HOME/.lumen"
LOG_DIR="$LUMEN_DIR/logs"
SCIENCE_BIN="$LUMEN_DIR/bin/lumen-science"

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

cmd_install() {
    if [[ ! -f "$SCIENCE_BIN" ]]; then
        echo -e "${RED}✗${NC} Lumen Science binary not found at $SCIENCE_BIN"
        echo "  Build first: cd ~/code/lumen/agent && cargo build --release"
        exit 1
    fi

    mkdir -p "$LOG_DIR" "$HOME/Library/LaunchAgents"

    cat > "$PLIST" << PLISTEOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>${LABEL}</string>
    <key>ProgramArguments</key>
    <array>
        <string>${SCIENCE_BIN}</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>${LOG_DIR}/science.log</string>
    <key>StandardErrorPath</key>
    <string>${LOG_DIR}/science.err.log</string>
    <key>WorkingDirectory</key>
    <string>${LUMEN_DIR}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin</string>
        <key>HOME</key>
        <string>${HOME}</string>
    </dict>
</dict>
</plist>
PLISTEOF

    launchctl bootstrap gui/$(id -u) "$PLIST" 2>/dev/null || \
        launchctl load "$PLIST" 2>/dev/null || true
    echo -e "${GREEN}✓${NC} Lumen Science Server installed as LaunchAgent"
    echo "  Logs: $LOG_DIR/science.log"
}

cmd_start() {
    launchctl bootstrap gui/$(id -u) "$PLIST" 2>/dev/null || \
        launchctl load "$PLIST" 2>/dev/null
    echo -e "${GREEN}✓${NC} Service started"
}

cmd_stop() {
    launchctl bootout gui/$(id -u)/"$LABEL" 2>/dev/null || \
        launchctl unload "$PLIST" 2>/dev/null
    echo -e "${GREEN}✓${NC} Service stopped"
}

cmd_restart() {
    cmd_stop
    sleep 1
    cmd_start
}

cmd_status() {
    if launchctl list | grep -q "$LABEL"; then
        local pid=$(launchctl list | grep "$LABEL" | awk '{print $1}')
        echo -e "${GREEN}●${NC} Lumen Science Server running (PID: $pid)"
    else
        echo -e "${RED}○${NC} Lumen Science Server not running"
    fi
}

cmd_remove() {
    cmd_stop 2>/dev/null || true
    rm -f "$PLIST"
    echo -e "${GREEN}✓${NC} LaunchAgent removed"
}

case "${1:-}" in
    install) cmd_install ;;
    start)   cmd_start ;;
    stop)    cmd_stop ;;
    restart) cmd_restart ;;
    status)  cmd_status ;;
    remove)  cmd_remove ;;
    *)       echo "Usage: $0 {install|start|stop|restart|status|remove}"; exit 1 ;;
esac
