#!/usr/bin/env python3
"""Generates Antigravity-style appicon.png and Windows icon.ico using Python stdlib only."""
import math
import os
import struct
import zlib


def make_png(size: int) -> bytes:
    rows = []
    cx = cy = size / 2.0
    for y in range(size):
        row = bytearray([0])
        for x in range(size):
            dx = abs(x - cx + 0.5) - (size * 0.30)
            dy = abs(y - cy + 0.5) - (size * 0.30)
            dist = math.hypot(max(dx, 0.0), max(dy, 0.0)) + min(max(dx, dy), 0.0) - (size * 0.16)
            if dist > 1.0:
                row.extend((0, 0, 0, 0))
                continue
            alpha = int(max(0.0, min(1.0, 1.0 - dist)) * 255)
            t = (x + y) / (2.0 * size)
            bg_r = int(14 + 22 * (1 - t))
            bg_g = int(18 + 24 * (1 - t))
            bg_b = int(32 + 46 * (1 - t))
            glow = max(0.0, 1.0 - math.hypot((x - size * 0.32) / size, (y - size * 0.28) / size) * 1.55)
            bg_r = min(255, int(bg_r + glow * 75))
            bg_g = min(255, int(bg_g + glow * 88))
            bg_b = min(255, int(bg_b + glow * 210))

            nx = (x - size * 0.5) / size
            ny = (y - size * 0.5) / size
            in_chev = False
            if -0.25 <= nx <= 0.04:
                d1 = abs(ny - (0.78 * (nx - 0.01)))
                d2 = abs(ny + (0.78 * (nx - 0.01)))
                if (ny <= 0.03 and d1 < 0.052) or (ny >= -0.03 and d2 < 0.052):
                    if nx <= 0.02 and abs(ny) <= 0.21:
                        in_chev = True
            in_under = (0.06 <= nx <= 0.27) and (0.12 <= ny <= 0.20)
            in_border = -3.0 <= dist <= 0.0

            if in_chev or in_under:
                gt = (nx + 0.3) / 0.6
                r = int(130 + 70 * max(0.0, min(1.0, 1 - gt)))
                g = int(165 + 75 * max(0.0, min(1.0, gt)))
                b = 255
                row.extend((r, g, b, alpha))
            elif in_border:
                row.extend((99, 124, 250, alpha))
            else:
                row.extend((bg_r, bg_g, bg_b, alpha))
        rows.append(bytes(row))

    raw = b"".join(rows)

    def chunk(tag: bytes, data: bytes) -> bytes:
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")


def main() -> None:
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    os.makedirs(os.path.join(root, "build", "windows"), exist_ok=True)
    png256 = make_png(256)
    with open(os.path.join(root, "build", "appicon.png"), "wb") as f:
        f.write(png256)

    ico_header = struct.pack("<HHH", 0, 1, 1)
    ico_entry = struct.pack("<BBBBHHII", 0, 0, 0, 0, 1, 32, len(png256), 6 + 16)
    with open(os.path.join(root, "build", "windows", "icon.ico"), "wb") as f:
        f.write(ico_header + ico_entry + png256)
    print("Generated build/appicon.png and build/windows/icon.ico")


if __name__ == "__main__":
    main()
