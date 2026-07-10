# OnlyOffice Document Server for Lumen Lab

## Env
```bash
# Set in /etc/lumen-keys.env or systemd Environment= for lumen-lab.service
LUMEN_ONLYOFFICE_URL=https://documentserver.example.com
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
    "url": "",
    "configured": false
  }
}
```
- `configured`: `true` when `LUMEN_ONLYOFFICE_URL` is set and non-empty.
- `url`: the configured URL (empty if not set).

## Lab UI
The inspector "Office" pane:
- If `LUMEN_ONLYOFFICE_URL` is configured: embeds OnlyOffice DocsAPI editor.
- If NOT configured (default): uses text extraction fallback (`ExtractOfficeText`) for .docx/.xlsx/.pptx preview, plus a download link for the full file.

## Fallback (no Document Server)
When `LUMEN_ONLYOFFICE_URL` is empty:
- Content preview: plain-text extraction from Office Open XML files (.docx, .xlsx, .pptx)
- Download: `GET /api/lab/files/download?project_id=<slug>&path=<file>` serves the raw file
- Health reports `configured: false` honestly

## Install (self-host, needs ~4GB+ RAM)
Official Docker (recommended when host allows):
```bash
docker run -i -t -d -p 8088:80 --name onlyoffice \
  -e JWT_ENABLED=false \
  onlyoffice/documentserver

# Then set:
# LUMEN_ONLYOFFICE_URL=http://127.0.0.1:8088
```

## VPS constraints
This VPS baseline (~3.4GiB RAM) often cannot run Document Server stably
(Document Server alone needs ~2GB+). Lab ships the integration + fallback
without requiring Docker on small hosts. For production editing, deploy to a
≥4GB host and point LUMEN_ONLYOFFICE_URL accordingly.
