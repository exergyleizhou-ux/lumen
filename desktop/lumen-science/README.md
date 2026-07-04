# Lumen Science — Acceptance Desktop

macOS Tauri shell wrapping the native Go control panel on `http://127.0.0.1:18990`.

## Icon

Brand icon (emerald science + lumen mark). Regenerate all platform sizes from source:

```bash
npx tauri icon app-icon-source.png
```

Source: `app-icon-source.png` (1024×1024). Matches GUI tokens (`--forest` #047857).

## Build

```bash
npm install
npm run tauri build
```

Artifact: `src-tauri/target/release/bundle/macos/Lumen Science.app`

Bundle ID: `com.lumen.science.acceptance`

## Runtime

- Spawns `lumen science gui --no-browser` (set `LUMEN_BIN` if not on PATH)
- Quit stops proxy only (`POST /api/quit-proxy`) — sandbox keeps running (CSswitch semantics)
- No Python proxy subprocess

## Verify

```bash
SCRATCH=/tmp/lumen-scratch bash ../../scripts/science/verify-desktop-health.sh
```