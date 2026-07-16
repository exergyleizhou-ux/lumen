#!/usr/bin/env bash
# Generate SPDX 2.3 SBOM for Lumen monorepo (agent Cargo packages + root notices).
# No third-party SBOM CLI required — uses cargo metadata + filesystem hashes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
OUT="$ROOT/SBOM.spdx.json"
META="$(mktemp)"
trap 'rm -f "$META"' EXIT
cd "$ROOT/agent"

echo "=== generate-sbom ==="
# Prefer full dependency graph; fall back to workspace packages only.
if ! cargo metadata --format-version 1 >"$META" 2>/dev/null; then
  cargo metadata --format-version 1 --no-deps >"$META"
fi

python3 - "$ROOT" "$OUT" "$META" <<'PY'
import hashlib, json, sys, subprocess
from datetime import datetime, timezone
from pathlib import Path

root = Path(sys.argv[1])
out = Path(sys.argv[2])
meta = json.loads(Path(sys.argv[3]).read_text())
now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()

packages = []
relationships = []
doc_id = "SPDXRef-DOCUMENT"
root_pkg_id = "SPDXRef-Package-lumen-monorepo"

def spdx_id(name: str) -> str:
    safe = "".join(c if c.isalnum() else "-" for c in name)[:80]
    return f"SPDXRef-Package-{safe}"

packages.append({
    "SPDXID": root_pkg_id,
    "name": "lumen",
    "versionInfo": "0.1.220-alpha.4",
    "downloadLocation": "NOASSERTION",
    "filesAnalyzed": False,
    "licenseConcluded": "Apache-2.0",
    "licenseDeclared": "Apache-2.0",
    "copyrightText": "See NOTICE and LEGAL.md",
    "supplier": "Organization: Lumen authors",
    "externalRefs": [{
        "referenceCategory": "OTHER",
        "referenceType": "gitCommit",
        "referenceLocator": head,
    }],
})
relationships.append({
    "spdxElementId": doc_id,
    "relationshipType": "DESCRIBES",
    "relatedSpdxElement": root_pkg_id,
})

seen = set()
for p in meta.get("packages", []):
    name = p.get("name") or "unknown"
    ver = p.get("version") or "0"
    key = f"{name}@{ver}"
    if key in seen:
        continue
    seen.add(key)
    pid = spdx_id(f"{name}-{ver}")
    lic = p.get("license") or "NOASSERTION"
    if not isinstance(lic, str):
        lic = "NOASSERTION"
    packages.append({
        "SPDXID": pid,
        "name": name,
        "versionInfo": ver,
        "downloadLocation": p.get("source") or "NOASSERTION",
        "filesAnalyzed": False,
        "licenseConcluded": lic,
        "licenseDeclared": lic,
        "copyrightText": "NOASSERTION",
        "supplier": "NOASSERTION",
    })
    relationships.append({
        "spdxElementId": root_pkg_id,
        "relationshipType": "DEPENDS_ON",
        "relatedSpdxElement": pid,
    })

file_hashes = {}
for rel in ["NOTICE", "LEGAL.md", "agent/LICENSE", "agent/THIRD-PARTY-NOTICES", "SOURCE_LOCK.json"]:
    p = root / rel
    if p.is_file():
        file_hashes[rel] = hashlib.sha256(p.read_bytes()).hexdigest()

doc = {
    "spdxVersion": "SPDX-2.3",
    "dataLicense": "CC0-1.0",
    "SPDXID": doc_id,
    "name": f"lumen-{head[:7]}",
    "documentNamespace": f"https://lumen.local/spdx/{head}",
    "creationInfo": {
        "created": now,
        "creators": ["Tool: scripts/generate-sbom.sh", "Organization: Lumen"],
        "licenseListVersion": "3.21",
    },
    "packages": packages,
    "relationships": relationships,
    "annotations": [{
        "annotationType": "OTHER",
        "annotator": "Tool: scripts/generate-sbom.sh",
        "annotationDate": now,
        "comment": json.dumps({
            "monorepo_git_head": head,
            "package_count": len(packages),
            "file_sha256": file_hashes,
            "generator": "cargo metadata + root legal files",
        }),
    }],
}
out.write_text(json.dumps(doc, indent=2) + "\n")
print(f"OK: wrote {out} packages={len(packages)} head={head[:7]}")
PY
