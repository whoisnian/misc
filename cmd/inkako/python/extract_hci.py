#!/usr/bin/env python3
"""
extract_hci.py - Extract image data from btsnoop HCI logs captured during
inkako (粉墨) BLE image transfers.

Parses btsnoop_hci.log files, extracts image pixel data from the BLE packets,
and saves as .bin (raw 2-bit packed) and .bmp (24-bit RGB preview).

Usage:
  python extract_hci.py examples/btsnoop_hci.1.log -o extracted
  python extract_hci.py examples/btsnoop_hci.1.log --dump-commands
"""

import argparse
import logging
import struct
import sys
from pathlib import Path

import numpy as np
from PIL import Image

logger = logging.getLogger("extract_hci")

# Color palette matching inkako 4-color e-ink
PALETTE = {
    0: (0, 0, 0),        # Black
    1: (255, 255, 255),   # White
    2: (255, 255, 0),     # Yellow
    3: (255, 0, 0),       # Red
}

SCREEN_TYPES = {
    1: (200, 200), 2: (200, 200), 3: (168, 384), 4: (200, 300),
    5: (528, 768), 6: (192, 176), 7: (124, 250), 8: (428, 428),
}


def parse_btsnoop(filepath: str) -> list[tuple[int, int, bytes]]:
    """
    Parse a btsnoop v1 HCI log file.

    Returns list of (record_index, flags, data) tuples.
    """
    records = []
    with open(filepath, "rb") as f:
        magic = f.read(8)
        if magic != b"btsnoop\x00":
            raise ValueError(f"Not a btsnoop file: {magic!r}")
        version, datalink = struct.unpack(">II", f.read(8))
        if version != 1:
            raise ValueError(f"Unsupported btsnoop version: {version}")

        while True:
            hdr = f.read(24)
            if len(hdr) < 24:
                break
            orig_len, incl_len, flags = struct.unpack(">III", hdr[:12])
            # hdr[12:16] = drops, hdr[16:24] = timestamp (already read)
            data = f.read(incl_len)
            if len(data) < incl_len:
                break
            records.append((len(records), flags, data))

    return records


def extract_att_packets(records: list[tuple[int, int, bytes]]):
    """
    Extract ATT-layer packets from HCI ACL records.

    Yields (record_index, direction, att_opcode, att_handle, app_data) tuples.
    """
    for rec_i, flags, d in records:
        if len(d) < 10 or d[0] != 0x02:
            continue
        direction = "TX" if (flags & 1) == 0 else "RX"
        att_opcode = d[9]
        att_payload = d[10:]
        if len(att_payload) < 2:
            continue
        handle = struct.unpack("<H", att_payload[:2])[0]
        app_data = att_payload[2:]
        yield rec_i, direction, att_opcode, handle, app_data


def extract_image(filepath: str, dump_commands: bool = False):
    """
    Extract image data from a btsnoop HCI log.

    Returns (pixel_data_bytes, screen_info, image_index, width, height) or None.
    """
    records = parse_btsnoop(filepath)
    logger.info("Parsed %d HCI records from %s", len(records), filepath)

    screen_info = None
    color_count = None
    image_index = None
    data_length = None
    rows: dict[int, bytes] = {}

    for rec_i, direction, att_op, handle, app_data in extract_att_packets(records):
        if len(app_data) == 0:
            continue

        # Protocol commands (0x6A TX, 0x6B RX)
        if app_data[0] == 0x6A and direction == "TX":
            cmd = app_data[1] if len(app_data) > 1 else 0
            if dump_commands:
                logger.info("rec=%d TX CMD: %s", rec_i, app_data.hex())

            # Start image push: 6a 01 08 01 04 idx info len[4] chk
            if cmd == 0x01 and len(app_data) >= 12 and app_data[2] == 0x08:
                image_index = app_data[5]
                screen_info = app_data[6]
                data_length = struct.unpack(">I", app_data[7:11])[0]
                logger.info("Start command: imageIndex=%d screenInfo=%d "
                            "dataLength=%d", image_index, screen_info,
                            data_length)

        elif app_data[0] == 0x6B and direction == "RX":
            cmd = app_data[1] if len(app_data) > 1 else 0
            if dump_commands:
                logger.info("rec=%d RX CMD: %s", rec_i, app_data.hex())

            # Device info response: 6b 15 02 colorCount screenInfo ...
            if cmd == 0x15 and len(app_data) >= 5:
                color_count = app_data[3]
                screen_info_resp = app_data[4]
                logger.info("Device info: colorCount=%d screenInfo=%d",
                            color_count, screen_info_resp)
                if screen_info is None:
                    screen_info = screen_info_resp

        # Image data rows: AA idx rowHi rowLo size data[size] checksum
        elif app_data[0] == 0xAA and direction == "TX" and len(app_data) >= 5:
            row_num = struct.unpack(">H", app_data[2:4])[0]
            row_size = app_data[4]

            if row_num == 0xFFFF:
                # End-of-data marker (no checksum)
                logger.info("End-of-data marker at rec=%d", rec_i)
                continue

            if row_size == 0:
                continue

            pixel_data = app_data[5:5 + row_size]
            if len(pixel_data) != row_size:
                logger.warning("Row %d: expected %d bytes, got %d",
                               row_num, row_size, len(pixel_data))
                continue

            # Verify checksum
            expected_chk = sum(app_data[:5 + row_size]) & 0xFF
            actual_chk = app_data[5 + row_size] if len(app_data) > 5 + row_size else None
            if actual_chk is not None and actual_chk != expected_chk:
                logger.warning("Row %d: checksum mismatch (got %#04x, "
                               "expected %#04x)", row_num, actual_chk,
                               expected_chk)

            rows[row_num] = pixel_data

    if not rows:
        logger.error("No image data found in HCI log")
        return None

    # Determine dimensions
    if screen_info is not None and screen_info in SCREEN_TYPES:
        width, height = SCREEN_TYPES[screen_info]
    else:
        # Infer from row size: row_size = width / 4
        first_row = next(iter(rows.values()))
        width = len(first_row) * 4
        height = len(rows)
        logger.warning("Unknown screen type %s, inferred %dx%d",
                       screen_info, width, height)

    logger.info("Image: %dx%d, %d rows extracted, screenInfo=%s",
                width, height, len(rows), screen_info)

    # Assemble pixel data in row order
    row_size = width // 4
    expected_rows = height
    all_data = bytearray()
    missing = []
    for i in range(1, expected_rows + 1):
        if i in rows:
            all_data.extend(rows[i])
        else:
            missing.append(i)
            all_data.extend(b"\x55" * row_size)  # Fill with white

    if missing:
        logger.warning("Missing %d rows: %s", len(missing),
                       missing[:10] if len(missing) > 10 else missing)

    expected_size = (width * height + 3) // 4
    if len(all_data) != expected_size:
        logger.warning("Data size mismatch: got %d, expected %d",
                       len(all_data), expected_size)

    return bytes(all_data), screen_info, image_index, width, height


def unpack_to_indices(data: bytes, width: int, height: int) -> np.ndarray:
    """Unpack 2-bit packed data to palette index array."""
    indices = np.zeros(width * height, dtype=np.uint8)
    for i, byte in enumerate(data):
        base = i * 4
        if base + 3 < len(indices):
            indices[base] = (byte >> 6) & 0x03
            indices[base + 1] = (byte >> 4) & 0x03
            indices[base + 2] = (byte >> 2) & 0x03
            indices[base + 3] = byte & 0x03
    return indices.reshape(height, width)


def indices_to_rgb(indices: np.ndarray) -> np.ndarray:
    """Convert palette index array to RGB."""
    h, w = indices.shape
    rgb = np.zeros((h, w, 3), dtype=np.uint8)
    for idx, color in PALETTE.items():
        mask = indices == idx
        rgb[mask] = color
    return rgb


def main():
    parser = argparse.ArgumentParser(
        description="Extract image data from btsnoop HCI logs of inkako BLE transfers",
    )
    parser.add_argument("logfile", help="btsnoop_hci.log file")
    parser.add_argument("-o", "--output",
                        help="Output base name (default: derived from input)")
    parser.add_argument("--dump-commands", action="store_true",
                        help="Print all protocol commands")
    parser.add_argument("-v", "--verbose", action="store_true",
                        help="Verbose output")
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )

    result = extract_image(args.logfile, dump_commands=args.dump_commands)
    if result is None:
        sys.exit(1)

    pixel_data, screen_info, image_index, width, height = result

    output_base = args.output or Path(args.logfile).stem
    bin_path = output_base + ".bin"
    bmp_path = output_base + ".bmp"

    Path(bin_path).write_bytes(pixel_data)
    logger.info("Binary data saved to %s (%d bytes)", bin_path, len(pixel_data))

    indices = unpack_to_indices(pixel_data, width, height)
    rgb = indices_to_rgb(indices)
    Image.fromarray(rgb).save(bmp_path)
    logger.info("Preview BMP saved to %s", bmp_path)

    # Print color stats
    unique, counts = np.unique(indices, return_counts=True)
    total = indices.size
    names = {0: "Black", 1: "White", 2: "Yellow", 3: "Red"}
    for idx, count in zip(unique, counts):
        logger.info("  %s: %d (%.1f%%)", names.get(idx, f"#{idx}"), count,
                     count / total * 100)


if __name__ == "__main__":
    main()
