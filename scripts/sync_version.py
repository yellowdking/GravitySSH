#!/usr/bin/env python3
"""Single-source version sync and verification script for GravitySSH.

Reads the canonical semantic version from the repository root `VERSION` file
and syncs (or checks with `--check`) all version declarations across:
  - VERSION
  - wails.json
  - build/windows/info.json
  - build/windows/wails.exe.manifest (4-part X.Y.Z.0)
  - main.go
  - frontend/package.json
  - frontend/src/hooks/useAppData.ts
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

SEMVER_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)(?:[-+][0-9A-Za-z.-]+)?$")
MANIFEST_RE = re.compile(
    r'(<assemblyIdentity\b[^>]*\bname="GravitySSH"[^>]*\bversion=")([^"]+)(")',
    re.MULTILINE,
)
MAIN_GO_RE = re.compile(r'(var\s+AppVersion\s*=\s*")([^"]+)(")')
USE_APP_DATA_RE = re.compile(
    r"(const\s+\[appVersion,\s*setAppVersion\]\s*=\s*useState\(\s*')([^']+)('\s*\))"
)


def parse_version(raw: str) -> tuple[str, str]:
    ver = raw.strip()
    m = SEMVER_RE.match(ver)
    if not m:
        raise ValueError(f"Invalid semantic version: {raw!r} (expected MAJOR.MINOR.PATCH)")
    major, minor, patch = m.group(1), m.group(2), m.group(3)
    return ver, f"{major}.{minor}.{patch}.0"


def sync_json_fields(path: Path, updates: list[tuple[tuple[str, ...], str]], check: bool) -> list[str]:
    issues: list[str] = []
    if not path.is_file():
        return [f"MISSING {path}"]
    text = path.read_text(encoding="utf-8")
    data = json.loads(text)
    changed = False
    for keys, expected in updates:
        node = data
        for k in keys[:-1]:
            if not isinstance(node, dict) or k not in node:
                issues.append(f"{path}: missing key path {'.'.join(keys)}")
                node = None
                break
            node = node[k]
        if node is None:
            continue
        leaf = keys[-1]
        actual = node.get(leaf)
        if actual != expected:
            issues.append(f"{path} ({'.'.join(keys)}): got {actual!r}, expected {expected!r}")
            node[leaf] = expected
            changed = True
    if changed and not check:
        rendered = json.dumps(data, indent=2, ensure_ascii=False) + "\n"
        path.write_text(rendered, encoding="utf-8", newline="\n")
    return issues


def sync_regex_file(path: Path, pattern: re.Pattern[str], expected: str, label: str, check: bool) -> list[str]:
    if not path.is_file():
        return [f"MISSING {path}"]
    text = path.read_text(encoding="utf-8")
    m = pattern.search(text)
    if not m:
        return [f"{path}: could not locate {label}"]
    actual = m.group(2)
    if actual == expected:
        return []
    if not check:
        updated = pattern.sub(rf"\g<1>{expected}\g<3>", text, count=1)
        path.write_text(updated, encoding="utf-8", newline="\n")
    return [f"{path} ({label}): got {actual!r}, expected {expected!r}"]


def run(repo_root: Path, check: bool, set_version: str | None) -> int:
    version_file = repo_root / "VERSION"
    if set_version is not None:
        if check:
            print("ERROR: --set cannot be combined with --check", file=sys.stderr)
            return 2
        version, manifest_ver = parse_version(set_version)
        version_file.write_text(version + "\n", encoding="utf-8", newline="\n")
    else:
        if not version_file.is_file():
            print(f"ERROR: {version_file} not found", file=sys.stderr)
            return 2
        version, manifest_ver = parse_version(version_file.read_text(encoding="utf-8"))

    diffs: list[str] = []

    # 1. VERSION canonical formatting check
    raw_ver = version_file.read_text(encoding="utf-8")
    if raw_ver.strip() != version:
        diffs.append(f"{version_file}: got {raw_ver.strip()!r}, expected {version!r}")
        if not check:
            version_file.write_text(version + "\n", encoding="utf-8", newline="\n")

    # 2. wails.json
    diffs.extend(
        sync_json_fields(
            repo_root / "wails.json",
            [(("info", "productVersion"), version)],
            check=check,
        )
    )

    # 3. build/windows/info.json
    diffs.extend(
        sync_json_fields(
            repo_root / "build" / "windows" / "info.json",
            [
                (("fixed", "file_version"), version),
                (("info", "0000", "ProductVersion"), version),
            ],
            check=check,
        )
    )

    # 4. build/windows/wails.exe.manifest
    diffs.extend(
        sync_regex_file(
            repo_root / "build" / "windows" / "wails.exe.manifest",
            MANIFEST_RE,
            manifest_ver,
            "GravitySSH assemblyIdentity version",
            check=check,
        )
    )

    # 5. main.go
    diffs.extend(
        sync_regex_file(
            repo_root / "main.go",
            MAIN_GO_RE,
            version,
            "AppVersion",
            check=check,
        )
    )

    # 6. frontend/package.json
    diffs.extend(
        sync_json_fields(
            repo_root / "frontend" / "package.json",
            [(("version",), version)],
            check=check,
        )
    )

    # 7. frontend/src/hooks/useAppData.ts
    diffs.extend(
        sync_regex_file(
            repo_root / "frontend" / "src" / "hooks" / "useAppData.ts",
            USE_APP_DATA_RE,
            version,
            "useAppData appVersion fallback",
            check=check,
        )
    )

    if check:
        if diffs:
            print(f"Version drift detected (VERSION={version}):", file=sys.stderr)
            for d in diffs:
                print(f"  - {d}", file=sys.stderr)
            return 1
        print(f"OK: all 7 version targets match VERSION={version} (manifest={manifest_ver})")
        return 0

    if diffs:
        print(f"Synced version targets to {version} (manifest={manifest_ver}):")
        for d in diffs:
            print(f"  * {d}")
    else:
        print(f"Already in sync at VERSION={version} (manifest={manifest_ver})")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Sync or check GravitySSH version across all manifests.")
    parser.add_argument("--check", action="store_true", help="Fail with exit code 1 if any target differs from VERSION.")
    parser.add_argument("--set", dest="set_version", default=None, help="Set VERSION to X.Y.Z and sync all targets.")
    parser.add_argument(
        "--root",
        type=Path,
        default=Path(__file__).resolve().parent.parent,
        help="Repository root directory (defaults to parent of scripts/).",
    )
    args = parser.parse_args(argv)
    try:
        return run(args.root.resolve(), check=args.check, set_version=args.set_version)
    except ValueError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
