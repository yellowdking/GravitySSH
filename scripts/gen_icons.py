#!/usr/bin/env python3
"""Generates 1024x1024 Retina appicon.png and multi-size Windows icon.ico using Python stdlib only."""
import math
import os
import struct
import zlib

APPICON_SIZE = 1024
ICO_SIZES = (16, 24, 32, 48, 64, 128, 256)


def make_png(size: int) -> bytes:
    rows = []
    cx = cy = size / 2.0
    half_box = size * 0.30
    corner_r = size * 0.16
    inv_size = 1.0 / size
    inv_2size = 1.0 / (2.0 * size)
    border_width = max(1.0, size * (3.0 / 256.0))
    chev_half = max(0.052, 0.85 / size)
    hypot = math.hypot

    for y in range(size):
        row = bytearray([0])
        dy = abs(y - cy + 0.5) - half_box
        dy_pos = dy if dy > 0.0 else 0.0
        glow_dy = (y - size * 0.28) * inv_size
        ny = (y - cx) * inv_size
        abs_ny = abs(ny)
        chev_y_ok = abs_ny <= 0.21
        under_y_ok = 0.12 <= ny <= 0.20

        for x in range(size):
            dx = abs(x - cx + 0.5) - half_box
            dx_pos = dx if dx > 0.0 else 0.0
            max_d = dx if dx > dy else dy
            dist = hypot(dx_pos, dy_pos) + (max_d if max_d < 0.0 else 0.0) - corner_r
            if dist > 1.0:
                row.extend((0, 0, 0, 0))
                continue
            alpha = 255 if dist <= 0.0 else int((1.0 - dist) * 255)
            one_minus_t = 1.0 - (x + y) * inv_2size
            bg_r = int(14 + 22 * one_minus_t)
            bg_g = int(18 + 24 * one_minus_t)
            bg_b = int(32 + 46 * one_minus_t)
            glow = 1.0 - hypot((x - size * 0.32) * inv_size, glow_dy) * 1.55
            if glow > 0.0:
                bg_r = min(255, int(bg_r + glow * 75))
                bg_g = min(255, int(bg_g + glow * 88))
                bg_b = min(255, int(bg_b + glow * 210))

            nx = (x - cx) * inv_size
            in_chev = False
            if chev_y_ok and -0.25 <= nx <= 0.02:
                slope = 0.78 * (nx - 0.01)
                if (ny <= 0.03 and abs(ny - slope) < chev_half) or (ny >= -0.03 and abs(ny + slope) < chev_half):
                    in_chev = True
            in_under = under_y_ok and (0.06 <= nx <= 0.27)
            in_border = -border_width <= dist <= 0.0

            if in_chev or in_under:
                gt = (nx + 0.3) / 0.6
                gt_clamped = 0.0 if gt < 0.0 else (1.0 if gt > 1.0 else gt)
                r = int(130 + 70 * (1.0 - gt_clamped))
                g = int(165 + 75 * gt_clamped)
                row.extend((r, g, 255, alpha))
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
    png_app = make_png(APPICON_SIZE)
    with open(os.path.join(root, "build", "appicon.png"), "wb") as f:
        f.write(png_app)

    ico_bytes = make_ico(ICO_SIZES)
    with open(os.path.join(root, "build", "windows", "icon.ico"), "wb") as f:
        f.write(ico_bytes)
    print(f"Generated {APPICON_SIZE}x{APPICON_SIZE} build/appicon.png and multi-size build/windows/icon.ico ({', '.join(str(s) for s in ICO_SIZES)}px)")


if __name__ == "__main__":
    main()
