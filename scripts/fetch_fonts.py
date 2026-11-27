#!/usr/bin/env python3
"""Optional helper to deterministically fetch and verify Smiley Sans (v2.0.1, SIL OFL 1.1) WOFF2.

Note: GravitySSH's default Windows UI uses native system fonts (Segoe UI Variable / Microsoft YaHei)
and does not require running this script during normal development or release builds.
"""
import hashlib
import os
import shutil
import subprocess
import sys
import tempfile
import urllib.parse
import urllib.request
import zipfile

SMILEY_SANS_VERSION = "v2.0.1"
ZIP_URL = (
    f"https://github.com/atelier-anchor/smiley-sans/releases/download/"
    f"{SMILEY_SANS_VERSION}/smiley-sans-{SMILEY_SANS_VERSION}.zip"
)
ZIP_SHA256 = "299c0be6c960ae37361762eca76f7d0cd516615435bb96c0d4b98a1e70178a07"
ZIP_MEMBER = "SmileySans-Oblique.ttf.woff2"
FONT_FILENAME = "SmileySans-Oblique.woff2"
FONT_SHA256 = "731f22973349404b15a88a99ef3b5dd4104c0965c23b7e485c1f11e84fea99e2"

LICENSE_URL = (
    f"https://raw.githubusercontent.com/atelier-anchor/smiley-sans/"
    f"{SMILEY_SANS_VERSION}/LICENSE"
)
LICENSE_FILENAME = "OFL-SmileySans.txt"
LICENSE_SHA256 = "9401f4050f1b66c26b6ccdc8b0e14a3c1cc37aac122eda84386f25854a9bec72"


def sha256_file(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


def is_valid(path: str, expected_sha256: str) -> bool:
    return os.path.isfile(path) and sha256_file(path) == expected_sha256


def download(url: str, dest: str) -> None:
    parsed = urllib.parse.urlparse(url)
    if parsed.scheme != "https":
        raise ValueError(f"Refusing non-HTTPS URL: {url}")
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "GravitySSH-FontFetcher/1.0"})
        with urllib.request.urlopen(req, timeout=60) as resp, open(dest, "wb") as out:
            shutil.copyfileobj(resp, out)
        return
    except Exception as exc:
        curl = shutil.which("curl.exe") or shutil.which("curl")
        if not curl:
            raise RuntimeError(f"Failed to download {url}: {exc}") from exc
        cmd = [curl, "-fsSL", "--proto", "=https", "--max-time", "60"]
        if os.name == "nt":
            cmd.append("--ssl-no-revoke")
        cmd.extend(["-o", dest, url])
        subprocess.run(cmd, check=True)


def atomic_write(dest: str, data: bytes) -> None:
    parent = os.path.dirname(os.path.abspath(dest))
    os.makedirs(parent, exist_ok=True)
    tmp_path = None
    try:
        with tempfile.NamedTemporaryFile(dir=parent, prefix=".fetch-font-", suffix=".tmp", delete=False) as tf:
            tmp_path = tf.name
            tf.write(data)
            tf.flush()
        os.replace(tmp_path, dest)
        tmp_path = None
    finally:
        if tmp_path and os.path.exists(tmp_path):
            try:
                os.unlink(tmp_path)
            except OSError:
                pass


def fetch_font(fonts_dir: str) -> None:
    font_path = os.path.join(fonts_dir, FONT_FILENAME)
    if is_valid(font_path, FONT_SHA256):
        print(f"Smiley Sans already present and verified: {font_path}")
        return

    with tempfile.TemporaryDirectory(prefix="gravityssh-fonts-") as tmpdir:
        zip_path = os.path.join(tmpdir, f"smiley-sans-{SMILEY_SANS_VERSION}.zip")
        print(f"Downloading Smiley Sans {SMILEY_SANS_VERSION}...")
        download(ZIP_URL, zip_path)

        got_zip_hash = sha256_file(zip_path)
        if got_zip_hash != ZIP_SHA256:
            raise RuntimeError(
                f"ZIP SHA-256 mismatch: got {got_zip_hash}, expected {ZIP_SHA256}"
            )

        with zipfile.ZipFile(zip_path, "r") as zf:
            data = zf.read(ZIP_MEMBER)

        got_font_hash = hashlib.sha256(data).hexdigest()
        if got_font_hash != FONT_SHA256:
            raise RuntimeError(
                f"Font SHA-256 mismatch: got {got_font_hash}, expected {FONT_SHA256}"
            )
        atomic_write(font_path, data)
        print(f"Extracted and verified {font_path} ({len(data):,} bytes)")


def fetch_license(fonts_dir: str) -> None:
    lic_path = os.path.join(fonts_dir, LICENSE_FILENAME)
    if is_valid(lic_path, LICENSE_SHA256):
        return

    with tempfile.TemporaryDirectory(prefix="gravityssh-ofl-") as tmpdir:
        tmp_lic = os.path.join(tmpdir, LICENSE_FILENAME)
        download(LICENSE_URL, tmp_lic)
        got_hash = sha256_file(tmp_lic)
        if got_hash != LICENSE_SHA256:
            raise RuntimeError(
                f"LICENSE SHA-256 mismatch: got {got_hash}, expected {LICENSE_SHA256}"
            )
        with open(tmp_lic, "rb") as f:
            atomic_write(lic_path, f.read())
        print(f"Saved SIL OFL 1.1 license: {lic_path}")


def main() -> int:
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    fonts_dir = os.path.join(root, "frontend", "public", "fonts")
    os.makedirs(fonts_dir, exist_ok=True)
    fetch_font(fonts_dir)
    fetch_license(fonts_dir)
    return 0


if __name__ == "__main__":
    sys.exit(main())
