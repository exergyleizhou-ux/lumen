#!/usr/bin/env bash
# Generate SPDX 2.3 SBOM for the Rust agent, standalone Go modules, and notices.
# No third-party SBOM CLI required — uses cargo metadata, go list, and hashes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
OUT="$ROOT/SBOM.spdx.json"
VERSION_FILE="$ROOT/VERSION"
META="$(mktemp)"
GO_META="$(mktemp)"
trap 'rm -f "$META" "$GO_META"' EXIT
[[ -s "$VERSION_FILE" ]] || {
  echo "FAIL: missing or empty VERSION" >&2
  exit 1
}
LUMEN_VERSION="$(tr -d '[:space:]' <"$VERSION_FILE")"
[[ -n "$LUMEN_VERSION" ]] || {
  echo "FAIL: VERSION is empty after whitespace normalization" >&2
  exit 1
}
cd "$ROOT/agent"

echo "=== generate-sbom ==="
# Prefer full dependency graph; fall back to workspace packages only.
if ! cargo metadata --format-version 1 >"$META" 2>/dev/null; then
  cargo metadata --format-version 1 --no-deps >"$META"
fi
if [[ -f "$ROOT/packs/science/go.mod" ]]; then
  (cd "$ROOT/packs/science" && go list -m -json all >"$GO_META")
fi

python3 - "$ROOT" "$OUT" "$META" "$GO_META" "$LUMEN_VERSION" <<'PY'
import hashlib, json, sys, subprocess
from datetime import datetime, timezone
from pathlib import Path

root = Path(sys.argv[1])
out = Path(sys.argv[2])
meta = json.loads(Path(sys.argv[3]).read_text())
go_meta_text = Path(sys.argv[4]).read_text()
lumen_version = sys.argv[5].strip()
now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
evidence_head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()

# An SBOM is evidence for the immutable source candidate, not for its later
# SOURCE_LOCK/SBOM/readiness suffix. Reconciliation therefore keys it to the
# source SHA frozen in SOURCE_LOCK.json, while retaining the writer commit as
# separate provenance metadata.
try:
    source_lock = json.loads((root / "SOURCE_LOCK.json").read_text())
    source_head = (source_lock.get("monorepo") or {}).get("git_head") or ""
except (OSError, json.JSONDecodeError) as exc:
    raise SystemExit(f"SOURCE_LOCK.json is required for SBOM provenance: {exc}")
if len(source_head) != 40 or any(char not in "0123456789abcdef" for char in source_head):
    raise SystemExit("SOURCE_LOCK.json has no valid monorepo.git_head")
if not lumen_version:
    raise SystemExit("empty Lumen version")

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
    "versionInfo": lumen_version,
    "downloadLocation": "NOASSERTION",
    "filesAnalyzed": False,
    "licenseConcluded": "Apache-2.0",
    "licenseDeclared": "Apache-2.0",
    "copyrightText": "See NOTICE and LEGAL.md",
    "supplier": "Organization: Lumen authors",
    "externalRefs": [{
        "referenceCategory": "OTHER",
        "referenceType": "gitCommit",
        "referenceLocator": source_head,
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

# `go list -m -json all` emits a stream of JSON objects rather than one array.
decoder = json.JSONDecoder()
offset = 0
go_modules = []
while offset < len(go_meta_text):
    while offset < len(go_meta_text) and go_meta_text[offset].isspace():
        offset += 1
    if offset >= len(go_meta_text):
        break
    module, offset = decoder.raw_decode(go_meta_text, offset)
    go_modules.append(module)

for module in go_modules:
    path = module.get("Path") or "unknown-go-module"
    version = module.get("Version") or (source_head[:7] if module.get("Main") else "0")
    key = f"go:{path}@{version}"
    if key in seen:
        continue
    seen.add(key)
    pid = spdx_id(f"go-{path}-{version}")
    package = {
        "SPDXID": pid,
        "name": path,
        "versionInfo": version,
        "downloadLocation": "NOASSERTION",
        "filesAnalyzed": False,
        "licenseConcluded": "Apache-2.0" if module.get("Main") else "NOASSERTION",
        "licenseDeclared": "Apache-2.0" if module.get("Main") else "NOASSERTION",
        "copyrightText": "See NOTICE and LEGAL.md" if module.get("Main") else "NOASSERTION",
        "supplier": "Organization: Lumen authors" if module.get("Main") else "NOASSERTION",
        "externalRefs": [{
            "referenceCategory": "PACKAGE-MANAGER",
            "referenceType": "purl",
            "referenceLocator": f"pkg:golang/{path}@{version}",
        }],
    }
    packages.append(package)
    relationships.append({
        "spdxElementId": root_pkg_id,
        "relationshipType": "CONTAINS" if module.get("Main") else "DEPENDS_ON",
        "relatedSpdxElement": pid,
    })

file_hashes = {}
for rel in [
    "NOTICE", "LEGAL.md", "agent/LICENSE", "agent/THIRD-PARTY-NOTICES",
    "SOURCE_LOCK.json", "packs/science/go.mod",
]:
    p = root / rel
    if p.is_file():
        file_hashes[rel] = hashlib.sha256(p.read_bytes()).hexdigest()

doc = {
    "spdxVersion": "SPDX-2.3",
    "dataLicense": "CC0-1.0",
    "SPDXID": doc_id,
    "name": f"lumen-{source_head[:7]}",
    "documentNamespace": f"https://lumen.local/spdx/{source_head}",
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
            "monorepo_git_head": source_head,
            "evidence_git_head": evidence_head,
            "lumen_version": lumen_version,
            "package_count": len(packages),
            "file_sha256": file_hashes,
            "go_module_count": len(go_modules),
            "generator": "cargo metadata + go list -m + root legal files",
        }),
    }],
}
out.write_text(json.dumps(doc, indent=2) + "\n")
print(
    f"OK: wrote {out} packages={len(packages)} "
    f"source={source_head[:7]} evidence={evidence_head[:7]} version={lumen_version}"
)
PY
