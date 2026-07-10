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
  The token is passed as `?token=...` in the callback URL.
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
| key conflict | Same document opened twice with same key | Document key includes timestamp for uniqueness |
| JWT error | DS requires JWT | Set `JWT_ENABLED=false` on container, or configure JWT secret |

## VPS constraints
This VPS (~3.4GiB RAM) cannot run Document Server stably. Keep `LUMEN_ONLYOFFICE_URL` unset on VPS (health reports `configured: false` honestly). Use an external DS host if needed.
