# OnlyOffice Document Server for Lumen Lab

## Env
```bash
# Document Server root URL
LUMEN_ONLYOFFICE_URL=http://127.0.0.1:8088

# Optional: shared secret for callback authentication
LUMEN_ONLYOFFICE_CALLBACK_TOKEN=your-secret-token
```

## Health
`GET /api/lab/health`:
```json
{
  "onlyoffice": {
    "url": "http://127.0.0.1:8088",
    "configured": true,
    "edit": true,
    "editHint": "编辑回写已启用（callback 就绪）"
  }
}
```
- `configured`: `true` when `LUMEN_ONLYOFFICE_URL` is set.
- `edit`: `true` when DS URL is configured (callback route always registered).
- `editHint`: human-readable status.

## Lab UI
The inspector "Office" tab:
- Path input + **mode selector** (查看/编辑)
- "打开/预览" → iframe loads `office-editor.html?ds=...&mode=view|edit&cb=...`
- "下载原文件" → raw download

### View mode
- Editor opens in read-only mode.
- No `callbackUrl` sent to Document Server.

### Edit mode
- Editor opens with `callbackUrl` pointing to `POST /api/lab/onlyoffice/callback?project_id=<slug>&path=<rel>`.
- When user saves in the editor, Document Server POSTs the callback with the modified file URL.
- Lab downloads the file and writes it back into the project workspace.

## Callback API

### POST /api/lab/onlyoffice/callback
Query params:
- `project_id` — project slug
- `path` — workspace-relative path to save to
- `token` — shared secret (required if `LUMEN_ONLYOFFICE_CALLBACK_TOKEN` is set)

Request body (OnlyOffice standard):
```json
{
  "status": 2,
  "url": "https://ds-host/cache/files/.../output.docx",
  "key": "document-key"
}
```

- `status: 1` (editing) → acknowledged, no action.
- `status: 2` (save ready) → download from `url`, write to workspace.
- `status: 6` (force save) → same as status 2.
- `32MB` max download size.
- Path traversal blocked by workspace Guard.

Response: `{"error": 0}` (0 = success, 1 = failure).

## Docker network (macOS Docker Desktop)
When Document Server runs in Docker on the same Mac:
- Lab must listen on `0.0.0.0:18992`.
- The frontend auto-detects `localhost`/`127.0.0.1` in DS URL and rewrites:
  - Download URL host → `host.docker.internal`
  - Callback URL host → `host.docker.internal`
- This ensures the Docker container can reach the Lab for both file download and save callbacks.

## Security
- Callback path is validated via workspace Guard (no traversal).
- Optional shared-secret token (`LUMEN_ONLYOFFICE_CALLBACK_TOKEN`):
  ```bash
  export LUMEN_ONLYOFFICE_CALLBACK_TOKEN=$(openssl rand -hex 16)
  ```
  **Do not put the token in frontend code.** Lab mints the callback URL via:
  ```http
  GET /api/lab/onlyoffice/session?project_id=<slug>&path=<rel>&mode=edit
  ```
  Response includes `callback_url` (with `?token=...` when env is set) and a stable
  `document_key` (hash of project+path+size+mtime; changes after save so the editor reloads).
- Download SSRF guard: callback only fetches hosts matching `LUMEN_ONLYOFFICE_URL`,
  localhost / `127.0.0.1`, or `host.docker.internal` (extend with
  `LUMEN_ONLYOFFICE_DOWNLOAD_HOSTS=host1,host2`).
- Reverse proxy: set `X-Forwarded-Proto` / `X-Forwarded-Host` / `X-Forwarded-Prefix`
  or `LUMEN_PUBLIC_PATH_PREFIX=/lumen-lab` so minted callback URLs use the public base.
- JWT: set `JWT_ENABLED=false` on the DS container for simple setups.

## Install (self-host, needs ~4GB+ RAM)
```bash
docker rm -f onlyoffice 2>/dev/null || true
docker run -d --name onlyoffice -p 8088:80 \
  -e JWT_ENABLED=false \
  --restart unless-stopped \
  onlyoffice/documentserver

# Wait for ready (first pull ~3GB, init 1–3 min)
for i in $(seq 1 60); do
  code=$(curl -sS -o /dev/null -w "%{http_code}" http://127.0.0.1:8088/ 2>/dev/null || echo 000)
  echo "try $i http=$code"
  [ "$code" = "200" ] || [ "$code" = "302" ] && break
  sleep 5
done
```

## Troubleshooting
| Symptom | Cause | Fix |
|---------|-------|-----|
| api.js 404 | DS not running or wrong URL | Check `LUMEN_ONLYOFFICE_URL`, curl DS root |
| White iframe | DS can't reach download URL | Ensure Lab listens on `0.0.0.0`, check host.docker.internal rewrite |
| Save doesn't persist | callback URL unreachable from DS | Check container networking, callback token |
| key conflict | Stale key after external rewrite | Re-open tab; key is size+mtime based and refreshes after save |
| JWT error | DS requires JWT | Set `JWT_ENABLED=false` on container, or configure JWT secret |

## VPS constraints
This VPS (~3.4GiB RAM) cannot run Document Server stably. Keep `LUMEN_ONLYOFFICE_URL` unset on VPS (health reports `configured: false` honestly). Use an external DS host if needed.

## Production topology (external DS)

When a dedicated Document Server host (≥4GB RAM) is available, connect Lab to it:

```
┌─────────────────────┐     ┌──────────────────────┐     ┌─────────────────────┐
│   User Browser      │────▶│   Lab (VPS/any)      │◀────│   Document Server   │
│                     │     │   :18992 or :443     │     │   :8088 (≥4GB RAM)  │
│ loads office-       │     │                      │     │                      │
│ editor.html         │     │ GET /files/download   │◀────│ GET document.url     │
│                     │     │ POST /onlyoffice/     │────▶│ POST callbackUrl     │
│                     │     │   callback            │     │                      │
└─────────────────────┘     └──────────────────────┘     └──────────────────────┘
```

### Setup checklist

1. **Document Server host** (separate from Lab VPS, ≥4GB RAM):
   ```bash
   docker run -d --name onlyoffice -p 8088:80 \
     -e JWT_ENABLED=false \
     --restart unless-stopped \
     onlyoffice/documentserver
   ```

2. **Lab config** (on the Lab host, NOT the Document Server):
   ```bash
   export LUMEN_ONLYOFFICE_URL=http://<ds-host-ip>:8088
   export LUMEN_ONLYOFFICE_CALLBACK_TOKEN=$(openssl rand -hex 16)
   ```

3. **Network**: DS must be able to reach Lab's download and callback URLs.
   - Same network: use internal IPs
   - Public Lab: DS accesses `https://demo.oasisdata2026.xyz/lumen-lab/api/lab/...`

4. **Verify**:
   ```bash
   # DS API reachable
   curl -sS -o /dev/null -w '%{http_code}\n' http://<ds-host>:8088/web-apps/apps/api/documents/api.js
   # Lab health
   curl -sS http://127.0.0.1:18992/api/lab/health | python3 -c "import json,sys; print(json.load(sys.stdin)['onlyoffice'])"
   # Expected: configured:true, edit:true
   ```

5. **Open in browser**: Lab → Office tab → enter `.docx` path → view/edit
