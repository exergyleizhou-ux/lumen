#!/usr/bin/env python3
"""Invalidate stale readiness evidence after a Lumen version bump."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path


class InvalidationError(RuntimeError):
    pass


def atomic_json_write(path: Path, value: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    mode = path.stat().st_mode if path.exists() else 0o644
    with tempfile.NamedTemporaryFile(
        "w", encoding="utf-8", dir=path.parent, delete=False
    ) as handle:
        json.dump(value, handle, indent=2)
        handle.write("\n")
        temporary = Path(handle.name)
    os.chmod(temporary, mode)
    os.replace(temporary, path)


def load_json_object(path: Path) -> dict[str, object]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise InvalidationError(f"cannot read {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise InvalidationError(f"{path} must contain a JSON object")
    return value


def invalidate(root: Path) -> None:
    version_path = root / "VERSION"
    try:
        version = version_path.read_text(encoding="utf-8").strip()
    except OSError as exc:
        raise InvalidationError(f"cannot read {version_path}: {exc}") from exc
    if not version:
        raise InvalidationError("VERSION is empty")

    source_lock_path = root / "SOURCE_LOCK.json"
    source_lock = load_json_object(source_lock_path)
    if source_lock.get("lumen_version") != version:
        raise InvalidationError(
            "SOURCE_LOCK.json must be refreshed for the bumped VERSION before "
            "readiness is invalidated"
        )

    try:
        head = subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True
        ).strip()
        head_short = subprocess.check_output(
            ["git", "rev-parse", "--short", "HEAD"], cwd=root, text=True
        ).strip()
    except (OSError, subprocess.CalledProcessError) as exc:
        raise InvalidationError(f"cannot resolve repository HEAD: {exc}") from exc

    generated_at = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    source_lock_sha256 = hashlib.sha256(source_lock_path.read_bytes()).hexdigest()
    blocker = (
        "release_version_changed:"
        f"rerun scripts/verify-readiness.sh for {version} at {head}"
    )

    status: dict[str, object] = {
        "schema_version": 1,
        "generated_at": generated_at,
        "version": version,
        "head_short": head_short,
        "ready": False,
        "state": "BLOCKED",
        "can_tool_call": False,
        "l0_pass": False,
        "engineering_complete": False,
        "source_lock_sha256": source_lock_sha256,
        "binary_sha256": None,
        "blockers": [blocker],
        "checks": [],
        "reconcile_pass": False,
        "reconciled_at": None,
        "note": (
            "A release version change invalidates prior readiness evidence. "
            "Run scripts/verify-readiness.sh to produce evidence for this version."
        ),
    }
    engineering_complete: dict[str, object] = {
        "schema_version": 1,
        "check_id": "engineering_complete",
        "version": version,
        "head_short": head_short,
        "pass": False,
        "meaning": (
            "Readiness evidence was invalidated by a release version change "
            "and must be regenerated."
        ),
        "auto_blockers": [blocker],
        "can_tool_call": False,
        "source_lock_sha256": source_lock_sha256,
        "binary_sha256": None,
        "generated_at": generated_at,
    }

    readiness = root / "artifacts" / "readiness"
    atomic_json_write(readiness / "status.json", status)
    atomic_json_write(
        readiness / "engineering_complete.json", engineering_complete
    )
    print(
        "OK: invalidated readiness evidence for "
        f"{version} at {head_short}; state remains BLOCKED"
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--root",
        type=Path,
        default=Path(__file__).resolve().parent.parent,
        help="repository root",
    )
    args = parser.parse_args()
    try:
        invalidate(args.root.resolve())
    except InvalidationError as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
