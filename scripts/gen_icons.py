#!/usr/bin/env python3
"""Generates Antigravity-style appicon.png and multi-size Windows icon.ico using Python stdlib only."""
import math
import os
import struct
import zlib

ICO_SIZES = (16, 24, 32, 48, 64, 128, 256)


def make_png(size: int) -> bytes:
    rows = []
    cx = cy = size / 2.0
    border_width = max(1.0, size * (3.0 / 256.0))
    chev_half = max(0.052, 0.85 / size)
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
                if (ny <= 0.03 and d1 < chev_half) or (ny >= -0.03 and d2 < chev_half):
                    if nx <= 0.02 and abs(ny) <= 0.21:
                        in_chev = True
            in_under = (0.06 <= nx <= 0.27) and (0.12 <= ny <= 0.20)
            in_border = -border_width <= dist <= 0.0

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


def make_ico(sizes: tuple[int, ...] = ICO_SIZES, prebuilt: dict[int, bytes] | None = None) -> bytes:
    """Build a multi-size PNG-in-ICO container."""
    frames: list[tuple[int, bytes]] = []
    for sz in sizes:
        if not (1 <= sz <= 256):
            raise ValueError(f"Unsupported ICO frame size: {sz}")
        png_bytes = prebuilt[sz] if (prebuilt and sz in prebuilt) else make_png(sz)
        frames.append((sz, png_bytes))

    header = struct.pack("<HHH", 0, 1, len(frames))
    entries = []
    offset = 6 + 16 * len(frames)
    for sz, png_bytes in frames:
        dim = 0 if sz == 256 else sz
        entries.append(struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(png_bytes), offset))
        offset += len(png_bytes)
    return header + b"".join(entries) + b"".join(data for _, data in frames)


def main() -> None:
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    os.makedirs(os.path.join(root, "build", "windows"), exist_ok=True)
    png256 = make_png(256)
    with open(os.path.join(root, "build", "appicon.png"), "wb") as f:
        f.write(png256)

    ico_bytes = make_ico(ICO_SIZES, prebuilt={256: png256})
    with open(os.path.join(root, "build", "windows", "icon.ico"), "wb") as f:
        f.write(ico_bytes)
    print(f"Generated build/appicon.png and multi-size build/windows/icon.ico ({', '.join(str(s) for s in ICO_SIZES)}px)")


if __name__ == "__main__":
    main()
