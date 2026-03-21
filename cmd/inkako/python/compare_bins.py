#!/usr/bin/env python3
"""Compare two e-ink .bin files and report pixel-level similarity."""

import argparse
import sys

import numpy as np

WIDTH, HEIGHT = 428, 428

COLOR_NAMES = ["Black", "White", "Yellow", "Red"]


def unpack_to_indices(data: bytes) -> np.ndarray:
    """Unpack 2-bit packed data (4 pixels/byte, MSB first) to flat index array."""
    arr = np.frombuffer(data, dtype=np.uint8)
    pixels = np.empty(len(arr) * 4, dtype=np.uint8)
    pixels[0::4] = (arr >> 6) & 0x03
    pixels[1::4] = (arr >> 4) & 0x03
    pixels[2::4] = (arr >> 2) & 0x03
    pixels[3::4] = arr & 0x03
    return pixels


def compare(file_a: str, file_b: str) -> None:
    data_a = open(file_a, "rb").read()
    data_b = open(file_b, "rb").read()

    if len(data_a) != len(data_b):
        print(f"Error: file sizes differ ({len(data_a)} vs {len(data_b)} bytes)")
        sys.exit(1)

    expected_size = (WIDTH * HEIGHT + 3) // 4
    if len(data_a) != expected_size:
        print(f"Error: unexpected file size {len(data_a)}, expected {expected_size} for {WIDTH}×{HEIGHT}")
        sys.exit(1)

    print(f"Resolution: {WIDTH}×{HEIGHT} ({len(data_a)} bytes)")

    pixels_a = unpack_to_indices(data_a)
    pixels_b = unpack_to_indices(data_b)
    total = WIDTH * HEIGHT

    match = pixels_a == pixels_b
    matched = int(np.sum(match))
    differing = total - matched

    print(f"\nTotal pixels:    {total}")
    print(f"Matching:        {matched} ({100 * matched / total:.2f}%)")
    print(f"Differing:       {differing} ({100 * differing / total:.2f}%)")

    if differing == 0:
        print("\nFiles are identical.")
        return

    # Per-color breakdown
    print("\nPer-color accuracy:")
    for idx, name in enumerate(COLOR_NAMES):
        mask = pixels_a == idx
        count = int(np.sum(mask))
        if count == 0:
            continue
        correct = int(np.sum(mask & match))
        print(f"  {name:6s}: {correct}/{count} correct ({100 * correct / count:.2f}%)")


def main():
    parser = argparse.ArgumentParser(description="Compare two e-ink .bin files pixel by pixel.")
    parser.add_argument("file_a", help="First .bin file")
    parser.add_argument("file_b", help="Second .bin file")
    args = parser.parse_args()
    compare(args.file_a, args.file_b)


if __name__ == "__main__":
    main()
