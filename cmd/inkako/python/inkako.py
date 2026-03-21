#!/usr/bin/env python3
"""
inkako.py - Image converter and BLE uploader for inkako (粉墨) 4-color e-ink screen.

Reverse-engineered from the inkako Android app (com.zhishudongli.inkako).

Supports:
  - Converting images to 4-color e-ink format (black, white, red, yellow)
  - Floyd-Steinberg dithering with weighted color distance
  - Output as .bin (raw 2-bit packed) and .bmp (24-bit preview)
  - BLE transfer to e-ink screen devices

Usage:
  # Convert only (preview + bin)
  python inkako.py convert input.jpg -o output

  # Convert and transfer via BLE
  python inkako.py transfer input.jpg --mac AA:BB:CC:DD:EE:FF

  # Scan for devices
  python inkako.py scan
"""

import argparse
import asyncio
import colorsys
import logging
import math
import time
from pathlib import Path

import numpy as np
from PIL import Image

logger = logging.getLogger("inkako")

# ── Device screen types (from DevListActivity) ──────────────────────────────
SCREEN_TYPES = {
    1: (200, 200),
    2: (200, 200),
    3: (168, 384),
    4: (200, 300),
    5: (528, 768),
    6: (192, 176),
    7: (124, 250),
    8: (428, 428),
}

# ── 4-color palette ─────────────────────────────────────────────────────────
# Index 0=Black, 1=White, 2=Yellow, 3=Red
PALETTE = np.array([
    [0, 0, 0],        # Black
    [255, 255, 255],   # White
    [255, 255, 0],     # Yellow
    [255, 0, 0],       # Red
], dtype=np.float64)

# BLE UUIDs
SERVICE_UUID = "79223401-1a11-21e1-8300-0940a1146603"
WRITE_UUID = "79223402-1a11-21e1-8300-0940a1146603"
NOTIFY_UUID = "79223403-1a11-21e1-8300-0940a1146603"

BLE_MTU = 247


# ── Image processing ────────────────────────────────────────────────────────

def floyd_steinberg_dither(image: np.ndarray, palette: np.ndarray,
                           weights: tuple[float, float, float] = (0.299, 0.587, 0.114)
                           ) -> np.ndarray:
    """
    Floyd-Steinberg dithering with weighted color distance.

    Matches the native libimage-lib.so PicDither() + applyErrorDiffusion():
      - Pixel buffer is int32 (matching Android's int[] passed to JNI)
      - Color distance uses float32: (wr*dr)^2 + (wg*dg)^2 + (wb*db)^2
      - Error diffusion uses integer arithmetic: err * weight / 16
        with truncation toward zero (C-style) and per-pixel [0,255] clamping
    """
    h, w = image.shape[:2]
    # Native code works with int32 pixel values (Java int[] → JNI)
    img = np.clip(np.round(image), 0, 255).astype(np.int32)
    result = np.zeros((h, w), dtype=np.uint8)

    wr, wg, wb = weights

    pal = palette.astype(np.int32)
    n_colors = len(pal)

    # Pre-extract palette as plain Python lists for inner loop
    pal_r_list = [int(pal[i, 0]) for i in range(n_colors)]
    pal_g_list = [int(pal[i, 1]) for i in range(n_colors)]
    pal_b_list = [int(pal[i, 2]) for i in range(n_colors)]

    w_minus_1 = w - 1
    h_minus_1 = h - 1

    for y in range(h):
        row = img[y]
        next_row = img[y + 1] if y < h_minus_1 else None
        for x in range(w):
            r0 = int(row[x, 0])
            g0 = int(row[x, 1])
            b0 = int(row[x, 2])

            # Find nearest palette color using float32 weighted distance
            # (native code converts int pixel values to float for distance calc)
            best_idx = 0
            best_dist = 1e30
            for i in range(n_colors):
                dr = (r0 - pal_r_list[i]) * wr
                dg = (g0 - pal_g_list[i]) * wg
                db = (b0 - pal_b_list[i]) * wb
                d = dr * dr + dg * dg + db * db
                if d < best_dist:
                    best_dist = d
                    best_idx = i

            result[y, x] = best_idx

            # Integer error (native: sub w5/w6/w7 instructions)
            er = r0 - pal_r_list[best_idx]
            eg = g0 - pal_g_list[best_idx]
            eb = b0 - pal_b_list[best_idx]

            # Distribute error using integer arithmetic matching native code:
            #   delta = int_trunc(err * weight / 16)  (ASR 4 with neg correction)
            #   new_val = clamp(old_val + delta, 0, 255)
            # Right (x+1, y) weight=7
            if x < w_minus_1:
                p = row[x + 1]
                for ch, err in ((0, er), (1, eg), (2, eb)):
                    v = err * 7
                    delta = v >> 4 if v >= 0 else -((-v) >> 4)
                    nv = int(p[ch]) + delta
                    p[ch] = 255 if nv > 255 else (0 if nv < 0 else nv)
            if next_row is not None:
                # Below-left (x-1, y+1) weight=3
                if x > 0:
                    p = next_row[x - 1]
                    for ch, err in ((0, er), (1, eg), (2, eb)):
                        v = err * 3
                        delta = v >> 4 if v >= 0 else -((-v) >> 4)
                        nv = int(p[ch]) + delta
                        p[ch] = 255 if nv > 255 else (0 if nv < 0 else nv)
                # Below (x, y+1) weight=5
                p = next_row[x]
                for ch, err in ((0, er), (1, eg), (2, eb)):
                    v = err * 5
                    delta = v >> 4 if v >= 0 else -((-v) >> 4)
                    nv = int(p[ch]) + delta
                    p[ch] = 255 if nv > 255 else (0 if nv < 0 else nv)
                # Below-right (x+1, y+1) weight=1
                if x < w_minus_1:
                    p = next_row[x + 1]
                    for ch, err in ((0, er), (1, eg), (2, eb)):
                        v = err
                        delta = v >> 4 if v >= 0 else -((-v) >> 4)
                        nv = int(p[ch]) + delta
                        p[ch] = 255 if nv > 255 else (0 if nv < 0 else nv)

    return result


def hsv_classify_pixel(r: int, g: int, b: int) -> int:
    """
    Classify a single pixel to a 4-color index using HSV thresholds.

    Matches EInkImageConverter.convertPixelToColorOptimized():
      - V < 0.3 → Black (0)
      - V > 0.85 and S < 0.2 → White (1)
      - H ≤ 15° or H ≥ 345°, S > 0.6 → Red (3)
      - 45° ≤ H ≤ 65°, S > 0.5 → Yellow (2)
      - Otherwise: V > 0.5 → White (1), else Black (0)
    """
    h, s, v = colorsys.rgb_to_hsv(r / 255.0, g / 255.0, b / 255.0)
    h_deg = h * 360.0

    if v < 0.3:
        return 0  # Black
    if v > 0.85 and s < 0.2:
        return 1  # White
    if (h_deg <= 15.0 or h_deg >= 345.0) and s > 0.6:
        return 3  # Red
    if 45.0 <= h_deg <= 65.0 and s > 0.5:
        return 2  # Yellow
    return 1 if v > 0.5 else 0


def apply_color_adjustments(image: np.ndarray,
                            brightness: int = 51,
                            contrast: int = 30,
                            saturation: int = 23) -> np.ndarray:
    """
    Apply brightness/contrast/saturation adjustments matching the Android app.

    The Android app (ColorMatrixPhotoView) applies three ColorMatrix transforms
    in order: saturation → contrast → brightness, using seekbar values:
      brightness → float b = progress / 50.0   (default 51 → 1.02)
      contrast   → float c = progress / 20.0   (default 30 → 1.50)
      saturation → float s = progress / 20.0   (default 23 → 1.15)
    """
    b = brightness / 50.0
    c = contrast / 20.0
    s = saturation / 20.0

    # Saturation: custom matrix (not standard setSaturation)
    #   R_scale = 1 + 0.3 * (s - 1)
    #   G_scale = 1 + 0.15 * (s - 1)
    #   B_scale = 1.0
    if s != 1.0:
        d = s - 1.0
        sat_r = 1.0 + 0.3 * d
        sat_g = 1.0 + 0.15 * d
    else:
        sat_r = sat_g = 1.0
    sat_b = 1.0

    # Combined: brightness(contrast(saturation(pixel)))
    #   channel' = c * sat_ch * channel + (1 - c) * 0.5 * 255 + (b - 1) * 255
    contrast_offset = (1.0 - c) * 0.5 * 255.0
    brightness_offset = (b - 1.0) * 255.0
    total_offset = contrast_offset + brightness_offset

    result = image.copy()
    result[:, :, 0] = result[:, :, 0] * (c * sat_r) + total_offset
    result[:, :, 1] = result[:, :, 1] * (c * sat_g) + total_offset
    result[:, :, 2] = result[:, :, 2] * (c * sat_b) + total_offset
    np.clip(result, 0, 255, out=result)
    return result


def pack_eink_data(indices: np.ndarray) -> bytes:
    """
    Pack 2-bit color indices into bytes (4 pixels per byte, MSB first).

    Matches EInkImageConverter.convertBitmapToEInkDataOptimized():
      byte = (px0 << 6) | (px1 << 4) | (px2 << 2) | px3
    """
    flat = indices.flatten()
    n_pixels = len(flat)
    n_bytes = (n_pixels + 3) // 4

    # Pad to multiple of 4
    padded = np.zeros(n_bytes * 4, dtype=np.uint8)
    padded[:n_pixels] = flat
    # Padding pixels default to 1 (white) as in the app (transparent → white)
    padded[n_pixels:] = 1

    reshaped = padded.reshape(-1, 4)
    packed = (
        (reshaped[:, 0].astype(np.uint8) << 6) |
        (reshaped[:, 1].astype(np.uint8) << 4) |
        (reshaped[:, 2].astype(np.uint8) << 2) |
        reshaped[:, 3].astype(np.uint8)
    )
    return bytes(packed)


def indices_to_rgb(indices: np.ndarray) -> np.ndarray:
    """Convert palette index array to RGB image for preview/BMP output."""
    h, w = indices.shape
    rgb = np.zeros((h, w, 3), dtype=np.uint8)
    for idx, color in enumerate(PALETTE):
        mask = indices == idx
        rgb[mask] = color.astype(np.uint8)
    return rgb


def convert_image(input_path: str, width: int, height: int,
                  use_dither: bool = True,
                  brightness: int = 51,
                  contrast: int = 30,
                  saturation: int = 23) -> tuple[np.ndarray, bytes]:
    """
    Full image conversion pipeline matching the inkako app.

    Pipeline (from EInkImageProcessor.processImageForEInk):
      1. Load and resize to target dimensions
      2. Apply brightness/contrast/saturation adjustments
      3. Apply Floyd-Steinberg dithering with 4-color palette
      4. Convert dithered image to 2-bit packed binary

    Args:
        input_path: Path to source image
        width: Target width in pixels
        height: Target height in pixels
        use_dither: Whether to apply Floyd-Steinberg dithering
        brightness: Brightness seekbar value (default: 51)
        contrast: Contrast seekbar value (default: 30)
        saturation: Saturation seekbar value (default: 23)

    Returns:
        Tuple of (palette_indices HxW array, packed binary data)
    """
    img = Image.open(input_path).convert("RGB")
    img = img.resize((width, height), Image.BILINEAR)
    pixels = np.array(img, dtype=np.float64)

    logger.info("Image loaded and resized to %dx%d", width, height)

    pixels = apply_color_adjustments(pixels, brightness, contrast, saturation)
    logger.info("Applied adjustments: brightness=%d, contrast=%d, saturation=%d",
                brightness, contrast, saturation)

    if use_dither:
        logger.info("Applying Floyd-Steinberg dithering...")
        t0 = time.monotonic()
        indices = floyd_steinberg_dither(pixels, PALETTE)
        logger.info("Dithering completed in %.1fs", time.monotonic() - t0)
    else:
        # Direct HSV-based classification (no dithering)
        logger.info("Applying direct color classification (no dithering)...")
        h, w = pixels.shape[:2]
        indices = np.zeros((h, w), dtype=np.uint8)
        for y in range(h):
            for x in range(w):
                r, g, b = pixels[y, x].astype(int)
                indices[y, x] = hsv_classify_pixel(r, g, b)

    packed = pack_eink_data(indices)
    expected_size = (width * height + 3) // 4
    assert len(packed) == expected_size, \
        f"Data size mismatch: got {len(packed)}, expected {expected_size}"

    # Print color distribution
    unique, counts = np.unique(indices, return_counts=True)
    total = indices.size
    names = {0: "Black", 1: "White", 2: "Yellow", 3: "Red"}
    logger.info("Color distribution:")
    for idx, count in zip(unique, counts):
        logger.info("  %s: %d (%.1f%%)", names.get(idx, f"#{idx}"), count,
                     count / total * 100)

    return indices, packed


# ── BLE protocol ─────────────────────────────────────────────────────────────

def checksum(data: bytes) -> int:
    """Calculate checksum: sum of all bytes & 0xFF."""
    return sum(data) & 0xFF


def add_checksum(data: bytes) -> bytes:
    """Append checksum byte to data."""
    return data + bytes([checksum(data)])


def make_start_command(image_index: int, screen_info: int,
                       data_length: int) -> bytes:
    """
    Build the start-image-push command.

    Format: [0x6A, 0x01, 0x08, 0x01, 0x04, image_index, screen_info,
             len_b3, len_b2, len_b1, len_b0, checksum]
    """
    cmd = bytes([
        0x6A, 0x01, 0x08, 0x01, 0x04,
        image_index & 0xFF,
        screen_info & 0xFF,
        (data_length >> 24) & 0xFF,
        (data_length >> 16) & 0xFF,
        (data_length >> 8) & 0xFF,
        data_length & 0xFF,
    ])
    return add_checksum(cmd)


def make_end_command(image_index: int, screen_info: int) -> bytes:
    """
    Build the end-image-push command.

    Format: [0x6A, 0x01, 0x06, 0x00, 0x04, image_index, screen_info,
             0x00, 0x00, checksum]
    """
    cmd = bytes([
        0x6A, 0x01, 0x06, 0x00, 0x04,
        image_index & 0xFF,
        screen_info & 0xFF,
        0x00, 0x00,
    ])
    return add_checksum(cmd)


def make_clean_flash_command() -> bytes:
    """Clean flash command: [0x6A, 0x16, 0x01, 0x01, checksum]"""
    return add_checksum(bytes([0x6A, 0x16, 0x01, 0x01]))


def make_update_image_command() -> bytes:
    """Update (refresh) display command."""
    cmd = bytes([0x6A, 0x14, 0x10, 0x01]) + bytes(17)
    return add_checksum(cmd)


def make_row_packet(image_index: int, row_num: int, row_data: bytes) -> bytes:
    """
    Build an image data row packet with 0xAA framing.

    Format: [0xAA, imageIndex, rowHi, rowLo, size, data[size], checksum]
    """
    header = bytes([
        0xAA,
        image_index & 0xFF,
        (row_num >> 8) & 0xFF,
        row_num & 0xFF,
        len(row_data) & 0xFF,
    ])
    frame = header + row_data
    return frame + bytes([sum(frame) & 0xFF])


def make_end_of_data_marker(image_index: int) -> bytes:
    """End-of-data marker: [0xAA, imageIndex, 0xFF, 0xFF, 0x00] (no checksum)."""
    return bytes([0xAA, image_index & 0xFF, 0xFF, 0xFF, 0x00])


def make_image_update_time_command(interval_seconds: int = 0) -> bytes:
    """
    Set image carousel auto-refresh interval.

    Use interval_seconds=0 to disable, 0xFFFF to query current value.
    """
    cmd = bytes([
        0x6A, 0x17, 0x02,
        (interval_seconds >> 8) & 0xFF,
        interval_seconds & 0xFF,
    ])
    return add_checksum(cmd)


def make_get_version_command() -> bytes:
    """Request firmware version: [0x6A, 0x11, 0x03, 0x00, 0x00, 0x00, checksum]"""
    return add_checksum(bytes([0x6A, 0x11, 0x03, 0x00, 0x00, 0x00]))


def make_dev_info_command() -> bytes:
    """Request device info command: [0x6A, 0x15, 0x02, 0x00, 0x00, checksum]"""
    return add_checksum(bytes([0x6A, 0x15, 0x02, 0x00, 0x00]))


async def scan_devices(timeout: float = 10.0):
    """Scan for inkako BLE devices."""
    from bleak import BleakScanner

    logger.info("Scanning for BLE devices (%.0fs)...", timeout)
    # discover() returns (BLEDevice, AdvertisementData) pairs in bleak >= 0.20;
    # RSSI lives on AdvertisementData, not BLEDevice.
    pairs = await BleakScanner.discover(timeout=timeout, return_adv=True)
    devices = []

    inkako_devices = []
    for addr, (d, adv) in pairs.items():
        devices.append(d)
        name = d.name or ""
        if name:
            inkako_devices.append(d)
            logger.info("  Found: %s (%s) RSSI=%s", d.name, d.address,
                         adv.rssi)

    if not inkako_devices:
        logger.info("No named devices found. All discovered addresses:")
        for addr, (d, adv) in pairs.items():
            logger.info("  %s (name=%s, RSSI=%s)", d.address,
                         d.name or "N/A", adv.rssi)

    return devices


async def transfer_image(mac_address: str, image_data: bytes,
                         screen_info: int = 8, image_index: int = 1,
                         width: int = 428):
    """
    Transfer image data to inkako device via BLE.

    Protocol (from DevImageActivity + upload.py reference):
      1. Connect, subscribe to notifications
      2. Send clean flash (no-response), wait 50ms
      3. Send start command (no-response)
      4. Wait specifically for start ACK (0x6B 0x01 0x08)
      5. Send rows one by one with 50ms delay; on 0xAB error retry from row 0
      6. Send end-of-data marker, wait 100ms
      7. Send end command; wait for end ACK (0x6B 0x01 0x06)
      8. Send image-update-time (disable carousel), wait for echo
      9. Send update/refresh command
    """
    from bleak import BleakClient

    row_size = width // 4  # maxRowSize = bleImageWidth / 4
    total_rows = math.ceil(len(image_data) / row_size)

    logger.info("Connecting to %s...", mac_address)

    start_ack = asyncio.Event()
    end_ack = asyncio.Event()
    update_time_ack = asyncio.Event()
    error_event = asyncio.Event()
    current_row = 0

    def notification_handler(sender, data: bytearray):
        nonlocal current_row
        if not data:
            return
        logger.debug("Notification: %s", data.hex())
        b0 = data[0]
        b1 = data[1] if len(data) > 1 else 0
        b2 = data[2] if len(data) > 2 else 0

        if b0 == 0xAB and len(data) >= 6 and b2 != 0xFF and data[5] == 0x00:
            # Transfer error: data[1]=imageIndex, data[2-3]=row (big-endian)
            err_row = (data[2] << 8) | data[3]
            logger.warning("Transfer error at row %d — will retry from row 0",
                           err_row)
            current_row = 0
            error_event.set()
        elif b0 == 0x6B:
            if b1 == 0x01 and b2 == 0x08:
                logger.info("Received start ACK")
                start_ack.set()
            elif b1 == 0x01 and b2 == 0x06:
                logger.info("Received end ACK")
                end_ack.set()
            elif b1 == 0x17:
                update_time_ack.set()
            elif b1 == 0x01 and b2 == 0x01:
                logger.warning("Device busy (display is refreshing)")

    async with BleakClient(mac_address, timeout=30.0) as client:
        # Negotiate MTU so that mtu_size is accurate (BlueZ-specific;
        # silently skipped on other backends).
        try:
            await client._backend._acquire_mtu()
        except Exception:
            pass
        try:
            logger.info("MTU: %d", client.mtu_size)
        except Exception:
            pass

        await client.start_notify(NOTIFY_UUID, notification_handler)
        logger.info("Notifications enabled")

        # Clean flash — no-response write; do NOT wait for its ACK before
        # sending start command (matches app behaviour in HCI logs)
        logger.info("Sending clean flash...")
        await client.write_gatt_char(WRITE_UUID, make_clean_flash_command(),
                                     response=False)
        await asyncio.sleep(0.05)

        # Start command
        start_cmd = make_start_command(image_index, screen_info, len(image_data))
        logger.info("Sending start command (data size: %d bytes)...",
                    len(image_data))
        await client.write_gatt_char(WRITE_UUID, start_cmd, response=False)

        # Wait specifically for 0x6B 0x01 0x08
        logger.info("Waiting for start ACK...")
        try:
            await asyncio.wait_for(start_ack.wait(), timeout=10.0)
        except asyncio.TimeoutError:
            logger.error("Timeout waiting for start ACK — aborting")
            await client.stop_notify(NOTIFY_UUID)
            return False

        # Send rows; retry from row 0 on any 0xAB transfer error
        logger.info("Transferring image data (%d rows)...", total_rows)
        retry_count = 0
        max_retries = 3

        while current_row < total_rows:
            if error_event.is_set():
                error_event.clear()
                retry_count += 1
                if retry_count > max_retries:
                    logger.error("Max retries (%d) exceeded — aborting",
                                 max_retries)
                    await client.stop_notify(NOTIFY_UUID)
                    return False
                logger.info("Retrying from row 0 (attempt %d/%d)",
                            retry_count, max_retries)
                await asyncio.sleep(2.0)
                continue

            offset = current_row * row_size
            chunk = image_data[offset:offset + row_size]
            packet = make_row_packet(image_index, current_row + 1, chunk)
            await client.write_gatt_char(WRITE_UUID, packet, response=False)

            current_row += 1
            await asyncio.sleep(0.05)  # 50ms — matches app's postDelayed(50L)

            if current_row % 50 == 0 or current_row == total_rows:
                logger.info("  Progress: %.0f%% (%d/%d)",
                            current_row / total_rows * 100,
                            current_row, total_rows)

        # End-of-data marker
        await client.write_gatt_char(
            WRITE_UUID, make_end_of_data_marker(image_index), response=False)
        await asyncio.sleep(0.1)

        # End command — wait for 0x6B 0x01 0x06
        end_cmd = make_end_command(image_index, screen_info)
        logger.info("Sending end command...")
        await client.write_gatt_char(WRITE_UUID, end_cmd, response=False)
        try:
            await asyncio.wait_for(end_ack.wait(), timeout=30.0)
        except asyncio.TimeoutError:
            logger.error("Timeout waiting for end ACK")
            await client.stop_notify(NOTIFY_UUID)
            return False

        # Disable carousel auto-refresh
        await client.write_gatt_char(
            WRITE_UUID, make_image_update_time_command(0), response=False)
        try:
            await asyncio.wait_for(update_time_ack.wait(), timeout=5.0)
        except asyncio.TimeoutError:
            pass

        # Trigger display refresh
        logger.info("Sending display refresh command...")
        await client.write_gatt_char(WRITE_UUID, make_update_image_command(),
                                     response=False)
        await asyncio.sleep(2.0)

        await client.stop_notify(NOTIFY_UUID)
        logger.info("Transfer complete!")

    return True


# ── CLI ──────────────────────────────────────────────────────────────────────

def cmd_convert(args):
    """Handle the 'convert' subcommand."""
    width, height = args.width, args.height
    indices, packed = convert_image(args.input, width, height,
                                     use_dither=not args.no_dither,
                                     brightness=args.brightness,
                                     contrast=args.contrast,
                                     saturation=args.saturation)

    output_base = args.output or Path(args.input).stem + "_eink"

    # Save binary data
    bin_path = output_base + ".bin"
    Path(bin_path).write_bytes(packed)
    logger.info("Binary data saved to %s (%d bytes)", bin_path, len(packed))

    # Save BMP preview
    bmp_path = output_base + ".bmp"
    rgb = indices_to_rgb(indices)
    Image.fromarray(rgb).save(bmp_path)
    logger.info("Preview BMP saved to %s", bmp_path)

    logger.info("Conversion complete.")


def cmd_transfer(args):
    """Handle the 'transfer' subcommand."""
    width, height = args.width, args.height
    indices, packed = convert_image(args.input, width, height,
                                     use_dither=not args.no_dither,
                                     brightness=args.brightness,
                                     contrast=args.contrast,
                                     saturation=args.saturation)

    if args.save:
        output_base = args.save
        Path(output_base + ".bin").write_bytes(packed)
        rgb = indices_to_rgb(indices)
        Image.fromarray(rgb).save(output_base + ".bmp")
        logger.info("Also saved .bin and .bmp to %s.*", output_base)

    asyncio.run(transfer_image(
        mac_address=args.mac,
        image_data=packed,
        screen_info=args.screen_info,
        image_index=args.image_index,
        width=width,
    ))


def cmd_scan(args):
    """Handle the 'scan' subcommand."""
    asyncio.run(scan_devices(timeout=args.timeout))


def main():
    parser = argparse.ArgumentParser(
        description="inkako (粉墨) e-ink screen image converter and BLE uploader",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("-v", "--verbose", action="store_true",
                        help="Enable debug logging")

    sub = parser.add_subparsers(dest="command", required=True)

    # convert
    p_convert = sub.add_parser("convert", help="Convert image for e-ink screen")
    p_convert.add_argument("input", help="Input image file")
    p_convert.add_argument("-o", "--output", help="Output base name (without extension)")
    p_convert.add_argument("-W", "--width", type=int, default=428,
                           help="Screen width (default: 428)")
    p_convert.add_argument("-H", "--height", type=int, default=428,
                           help="Screen height (default: 428)")
    p_convert.add_argument("--no-dither", action="store_true",
                           help="Disable Floyd-Steinberg dithering")
    p_convert.add_argument("--brightness", type=int, default=51,
                           help="Brightness (default: 51, app seekbar value)")
    p_convert.add_argument("--contrast", type=int, default=30,
                           help="Contrast (default: 30, app seekbar value)")
    p_convert.add_argument("--saturation", type=int, default=23,
                           help="Saturation (default: 23, app seekbar value)")
    p_convert.set_defaults(func=cmd_convert)

    # transfer
    p_transfer = sub.add_parser("transfer",
                                help="Convert and transfer image via BLE")
    p_transfer.add_argument("input", help="Input image file")
    p_transfer.add_argument("--mac", required=True,
                            help="BLE device MAC address")
    p_transfer.add_argument("-W", "--width", type=int, default=428,
                            help="Screen width (default: 428)")
    p_transfer.add_argument("-H", "--height", type=int, default=428,
                            help="Screen height (default: 428)")
    p_transfer.add_argument("--no-dither", action="store_true",
                            help="Disable Floyd-Steinberg dithering")
    p_transfer.add_argument("--brightness", type=int, default=51,
                            help="Brightness (default: 51, app seekbar value)")
    p_transfer.add_argument("--contrast", type=int, default=30,
                            help="Contrast (default: 30, app seekbar value)")
    p_transfer.add_argument("--saturation", type=int, default=23,
                            help="Saturation (default: 23, app seekbar value)")
    p_transfer.add_argument("--screen-info", type=int, default=8,
                            help="Screen type byte (default: 8 for 428x428)")
    p_transfer.add_argument("--image-index", type=int, default=1,
                            help="Image slot index (default: 1)")
    p_transfer.add_argument("--save", help="Also save .bin and .bmp output")
    p_transfer.set_defaults(func=cmd_transfer)

    # scan
    p_scan = sub.add_parser("scan", help="Scan for BLE devices")
    p_scan.add_argument("-t", "--timeout", type=float, default=10.0,
                        help="Scan timeout in seconds (default: 10)")
    p_scan.set_defaults(func=cmd_scan)

    args = parser.parse_args()

    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )

    args.func(args)


if __name__ == "__main__":
    main()
