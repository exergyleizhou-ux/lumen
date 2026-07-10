# OnlyOffice Document Server for Lumen Lab

## Env
```bash
# Set in /etc/lumen-keys.env or systemd Environment= for lumen-lab.service
LUMEN_ONLYOFFICE_URL=http://127.0.0.1:8088
```

Example `/etc/lumen/onlyoffice.env.example`:
```bash
# OnlyOffice Document Server URL (optional)
# Uncomment and set to enable WYSIWYG Office editing in the Lab inspector.
# Requires onlyoffice/documentserver running (needs ~4GB+ RAM).
# LUMEN_ONLYOFFICE_URL=http://127.0.0.1:8088
```

## Health
`GET /api/lab/health` returns:
```json
{
  "onlyoffice": {
    "url": "http://127.0.0.1:8088",
    "configured": true
  }
}
```
- `configured`: `true` when `LUMEN_ONLYOFFICE_URL` is set and non-empty.
- `url`: the configured URL (empty if not set).

## Lab UI
The inspector "Office" tab:
- Input: workspace-relative file path (e.g. `reports/sample.docx`)
- "打开/预览" button: if `LUMEN_ONLYOFFICE_URL` is configured, opens OnlyOffice editor in view mode; otherwise falls back to text extraction + download.
- "下载原文件" button: always available, downloads the raw file.

The editor is loaded via a standalone `office-editor.html` page served by Lab's static file handler. It receives the Document Server URL and file download URL as query parameters, avoiding srcdoc/iframe script issues.

### Docker network (macOS Docker Desktop)
When the Document Server runs in Docker on the same machine:
- Lab must listen on `0.0.0.0` (not just `127.0.0.1`) so the Docker container can reach it:
  ```bash
  lumen science lab --addr 0.0.0.0:18992 --no-browser
  ```
- The frontend auto-detects `localhost`/`127.0.0.1` in the DS URL and rewrites the file download URL to use `host.docker.internal` (Docker Desktop's magic hostname for reaching the host from containers).
- For Linux Docker (non-Desktop), use `--add-host host.docker.internal:host-gateway` or configure the host IP manually.

## Local Development (macOS with Docker Desktop)

### One-shot (recommended)
```bash
# Pulls ~3GB image + starts container on :8088 (may fail on bad CDN/network)
./scripts/science/setup-onlyoffice.sh

# Lab picks up LUMEN_ONLYOFFICE_URL when :8088 answers
./scripts/science/lab-local-with-sidecars.sh
```

### Manual: Start OnlyOffice Document Server
```bash
docker rm -f onlyoffice 2>/dev/null || true
docker run -d \
  --name onlyoffice \
  -p 8088:80 \
  -e JWT_ENABLED=false \
  --restart unless-stopped \
  onlyoffice/documentserver

# Wait for ready (first run pulls ~3GB image and initializes, may take 2–5 min)
for i in $(seq 1 60); do
  code=$(curl -sS -o /dev/null -w "%{http_code}" http://127.0.0.1:8088/ 2>/dev/null || echo 000)
  echo "try $i http=$code"
  [ "$code" = "200" ] || [ "$code" = "302" ] && break
  sleep 5
done
curl -sS -o /dev/null -w "final:%{http_code}\n" http://127.0.0.1:8088/
```

### Start Lab
```bash
export LUMEN_ONLYOFFICE_URL=http://127.0.0.1:8088
cd /path/to/lumen
lumen science lab --addr 0.0.0.0:18992 --no-browser
```

### 3. Verify
```bash
# Health check
curl -sS http://127.0.0.1:18992/api/lab/health | python3 -m json.tool | grep -A3 onlyoffice
# Expected: "configured": true, "url": "http://127.0.0.1:8088"
```

### 4. Test in Browser
1. Open `http://127.0.0.1:18992/`
2. Create or select a project
3. Create a real `.docx` file in the workspace (e.g. `reports/sample.docx`)
   - Using Python: `python3 -c "from docx import Document; Document().save('reports/sample.docx')"`
   - Or upload via Lab's file panel
4. Switch to "Office" inspector tab
5. Enter path `reports/sample.docx`, click "打开/预览"
6. OnlyOffice editor should appear in view mode

## Fallback (no Document Server)
When `LUMEN_ONLYOFFICE_URL` is empty or unset:
- Content preview: plain-text extraction from Office Open XML files (.docx, .xlsx, .pptx)
- Download: `GET /api/lab/files/download?project_id=<slug>&path=<file>` serves the raw file
- Health reports `configured: false` honestly

## Install on dedicated server (needs ~4GB+ RAM)
Official Docker (recommended):
```bash
docker run -i -t -d -p 8088:80 --name onlyoffice \
  -e JWT_ENABLED=false \
  onlyoffice/documentserver

# Then set on the Lab host:
# LUMEN_ONLYOFFICE_URL=http://<ds-host>:8088
```

## VPS constraints
This VPS baseline (~3.4GiB RAM) often cannot run Document Server stably
(Document Server alone needs ~2GB+). Lab ships the integration + fallback
without requiring Docker on small hosts. For production editing, deploy to a
≥4GB host and point `LUMEN_ONLYOFFICE_URL` accordingly.

## Current Status
- **Editor mode**: view-only (no edit/callback implemented yet)
- **File types**: .docx (word), .xlsx (cell), .pptx (slide)
- **Legacy formats** (.doc, .ppt, .xls): download-only, no preview
- **Docker network**: auto `host.docker.internal` rewrite on macOS; may need manual config on Linux
