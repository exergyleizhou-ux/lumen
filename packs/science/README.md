# Lumen Science Pack

Claude Science Bridge — native Go proxy for DeepSeek-powered scientific research.

## Quick Start (≤3 steps)

### Step 1: Build
```bash
cd packs/science && go build -o lumen-science ./cmd/science
```

### Step 2: Start
```bash
./lumen-science start    # proxy + sandbox + browser (one click)
```

### Step 3: Use
```bash
./lumen-science gui      # control panel at http://127.0.0.1:18990
./lumen-science brief "aspirin"  # research brief generation
```

## More
- `./lumen-science status` — proxy/sandbox/cache status
- `./lumen-science watch` — live DeepSeek prefix-cache dashboard
- `./lumen-science doctor` — read-only diagnostics
- `./lumen-science native verify --live` — 5-ship MCP fleet check

## Source
Migrated from `~/lumen/internal/science/` (legacy Go agent).
Runs as standalone binary; no dependency on the Lumen Rust agent runtime.
