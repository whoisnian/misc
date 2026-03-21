# inkako (粉墨) — Image Processing Algorithm & BLE Protocol

Reverse-engineered from the inkako Android app (`com.zhishudongli.inkako`).

## Image Processing Pipeline

The app converts images for a 4-color e-ink display (black, white, red, yellow).

### Pipeline (BLE path)

```
Input Image
    │
    ▼
┌─────────────────────────┐
│  Color Adjustment        │  ColorMatrixPhotoView
│  (Saturation/Contrast/   │  Seekbar → ColorMatrix → draw(canvas)
│   Brightness)            │
└──────────┬──────────────┘
           │
           ▼
┌─────────────┐
│   Resize     │  Scale to device dimensions (e.g. 428×428)
└──────┬──────┘
       │
       ▼
┌─────────────────────────┐
│  Floyd-Steinberg Dither  │  Native libimage-lib.so PicDither()
│  4-color BWRY palette    │  with luma-weighted color distance
└──────────┬──────────────┘
           │
           ▼
┌─────────────────────────┐
│  HSV Color Classification│  EInkImageConverter
│  + 2-bit Pixel Packing   │  .convertBitmapToEInkDataOptimized()
└──────────┬──────────────┘
           │
           ▼
     Binary Data (.bin)
```

### Color Adjustment (ColorMatrix)

Before dithering, the image is rendered through Android's `ColorMatrixPhotoView`
which applies a composite color matrix (brightness → contrast → saturation) via
`setColorFilter()`.  The Java code calls `postConcat` in the order sat, contrast,
brightness, which with Android's right-multiply `postConcat` semantics means the
pixel transformation order is brightness first, then contrast, then saturation.
The adjustments are baked into the pixel data when the view
is captured via `photoView.draw(canvas)`.

BLE default seekbar values (all display as "1.00" to the user):

| Parameter  | Seekbar | Factor | Formula                                        |
|------------|---------|--------|------------------------------------------------|
| Brightness | 51      | 1.02   | Additive offset: `(factor - 1) × 255`         |
| Contrast   | 30      | 1.50   | Scale + offset: `factor × pixel + (1 - factor) × 127.5` |
| Saturation | 23      | 1.15   | Custom: R × `1 + 0.3(s-1)`, G × `1 + 0.15(s-1)`, B unchanged |

The saturation matrix is non-standard — it boosts R and G channels independently
rather than using luminance-preserving desaturation. The combined matrix for BLE defaults
(numerically equivalent for both transformation orders at these parameters):

```
R_out = 1.5675 × R - 58.65
G_out = 1.5338 × G - 58.65
B_out = 1.5000 × B - 58.65
```

### Resize

The input image is resized to match the e-ink display resolution.

In the app's BLE path, two resizes occur:
1. `Bitmap.createScaledBitmap(bitmap, W, H, false)` — **nearest-neighbor** (no filtering)
2. `ImageUtils.getMyImg(bitmap, W, H)` — uses `Matrix.postScale()` with bilinear filtering, but since dimensions already match, this is effectively a no-op

### Color Palette (BLE Encoding)

| BLE Index | Color  | RGB             |
|-----------|--------|-----------------|
| 0         | Black  | (0, 0, 0)      |
| 1         | White  | (255, 255, 255) |
| 2         | Yellow | (255, 255, 0)  |
| 3         | Red    | (255, 0, 0)    |

### Floyd-Steinberg Dithering (Native)

Implemented in native `libimage-lib.so` (`PicDither` at 0x1bf00, `applyErrorDiffusion` at 0x1b640).

The Java wrapper (`EInkImageProcessor.applyDithering`) calls:

```java
ImageUtils.PicDither(bitmap, paletteType=3, useDither=true, w1=7, w2=1, w3=5, w4=3, width, height)
```

The Java wrapper reorders arguments before the JNI call:
```java
// JNI signature: PicDither(int[] pixels, int width, int height, int paletteType, int useDither, int w1, int w2, int w3, int w4)
new ImageUtils().PicDither(rgb, width, height, paletteType, useDither ? 1 : 0, w1, w2, w3, w4)
```

**Parameters:**
- `paletteType=3`: 4-color palette (BWRY in native code)
- `useDither=1`: Selects the hardcoded weights path in `applyErrorDiffusion` (arg9==1). Despite w1–w4 being passed, they are **ignored** — weights are hardcoded in the native code.

**Color distance** uses weighted Euclidean distance with standard luma coefficients (float32):

$$d = (0.299 \cdot \Delta R)^2 + (0.587 \cdot \Delta G)^2 + (0.114 \cdot \Delta B)^2$$

**Error diffusion** uses standard Floyd-Steinberg coefficients (hardcoded):

```
              pixel    7/16 →
         3/16 ↙  5/16 ↓  1/16 ↘
```

**Native implementation details:**
- Input: Flat `int32` RGB array (`[R0, G0, B0, R1, G1, B1, ...]`), extracted via `getPixel()` + `Color.red/green/blue()`
- Scan direction: Always left-to-right, top-to-bottom (**no serpentine**)
- Integer division: C truncation toward zero — `(x < 0) ? (x + 15) >> 4 : x >> 4`
- Clamping: `[0, 255]` per channel
- Palette color matching: Index 0 checked first, then loop 1..3 with strict less-than for update
- Output: Flat `int32` RGB array with each pixel snapped to the nearest palette color
- Output bitmap: `Bitmap.Config.RGB_565` (lossless for pure palette colors 0 and 255)

#### Native Palette Order vs BLE Encoding

The native `PicDither` uses BWRY order internally (rodata at 0x104d8):

| Native Index | Color  |
|-------------|--------|
| 0           | Black  |
| 1           | White  |
| 2           | Red    |
| 3           | Yellow |

However, `EInkImageConverter.convertBitmapToEInkDataOptimized` classifies output pixels by
their actual RGB values (not native indices), producing BLE encoding BWYR:

| BLE Index | Color  |
|----------|--------|
| 0        | Black  |
| 1        | White  |
| 2        | Yellow |
| 3        | Red    |

Since palette colors have unique RGB values (0 or 255 per channel), this remapping is unambiguous.

### HSV-Based Color Classification

`EInkImageConverter.convertBitmapToEInkDataOptimized` maps each dithered pixel's RGB value to a
2-bit color index using HSV thresholds. After dithering, pixels are already pure palette colors,
so the classification is deterministic. Without dithering, this serves as the primary color mapping.

| Condition | Result |
|-----------|--------|
| V < 0.3 | Black (0) |
| V > 0.85 and S < 0.2 | White (1) |
| (H ≤ 15° or H ≥ 345°) and S > 0.6 | Red (3) |
| 45° ≤ H ≤ 65° and S > 0.5 | Yellow (2) |
| V > 0.5 (fallback) | White (1) |
| V ≤ 0.5 (fallback) | Black (0) |

Uses a 4096-entry lookup table (4-bit quantized R, G, B) for performance.

### 2-Bit Pixel Packing

4 pixels packed into each byte, MSB first:

```
byte = (pixel0 << 6) | (pixel1 << 4) | (pixel2 << 2) | pixel3
```

Output size: `⌈(width × height) / 4⌉` bytes. For 428×428 = 183,184 pixels → **45,796 bytes**.

---

## BLE Communication Protocol

### Service & Characteristics

| Role | UUID |
|------|------|
| Service | `79223401-1a11-21e1-8300-0940a1146603` |
| Write | `79223402-1a11-21e1-8300-0940a1146603` |
| Notify | `79223403-1a11-21e1-8300-0940a1146603` |

MTU is set to **247** after connection.

### Command Format

All commands start with header byte `0x6A`. Responses from the device start with `0x6B`.

Every command ends with a **checksum byte**: `sum(all_preceding_bytes) & 0xFF`.

### Command Reference

#### Clean Flash (`0x6A 0x16`)

Erases stored image data on the device.

```
TX: [0x6A, 0x16, 0x01, 0x01, checksum]
```

#### Start Image Push (`0x6A 0x01 0x08`)

Initiates an image transfer. Includes the image slot index, display type, and total data length (4 bytes big-endian).

```
Byte [0]:    0x6A         — Header
Byte [1]:    0x01         — Command group
Byte [2]:    0x08         — Start sub-command
Byte [3]:    0x01         — Fixed
Byte [4]:    0x04         — Fixed
Byte [5]:    imageIndex   — 1-based image slot
Byte [6]:    screenInfo   — Screen type byte (see Screen Types)
Byte [7-10]: dataLength   — 32-bit big-endian length of image data
Byte [11]:   checksum
Total: 12 bytes
```

#### Image Data Packets

Sent row by row with `0xAA` framing. Each row is `width / 4` bytes of packed pixels. Rows are **streamed continuously** — the device does not send per-row acknowledgments.

```
Byte [0]:         0xAA          — Data header
Byte [1]:         imageIndex    — 1-based (matches Start Command)
Byte [2-3]:       rowNumber     — 1-based, big-endian
Byte [4]:         chunkLength   — Bytes of image data in this packet
Byte [5..5+N-1]:  imageData     — N bytes of packed pixel data
Byte [5+N]:       checksum      — sum(all_preceding_bytes) & 0xFF
```

**Row size:** `width / 4` bytes (107 bytes for 428px width)

**Timing:** 50ms delay between row packets

After the last row, send an **end-of-data marker** (no checksum):

```
TX: [0xAA, imageIndex, 0xFF, 0xFF, 0x00]
```

#### End Image Push (`0x6A 0x01 0x06`)

Signals transfer completion.

```
Byte [0]:    0x6A
Byte [1]:    0x01
Byte [2]:    0x06
Byte [3]:    0x00
Byte [4]:    0x04
Byte [5]:    imageIndex
Byte [6]:    screenInfo
Byte [7-8]:  0x00, 0x00
Byte [9]:    checksum
Total: 10 bytes
```

#### Update (Refresh) Display (`0x6A 0x14 0x10`)

Triggers the e-ink screen to refresh with the uploaded image.

```
TX: [0x6A, 0x14, 0x10, 0x01, 0x00×17, checksum]
Total: 22 bytes
```

#### Get Device Info (`0x6A 0x15`)

```
TX: [0x6A, 0x15, 0x02, 0x00, 0x00, checksum]
RX: [0x6B, 0x15, ..., colorCount, screenInfo, ...]
```

Response fields:
- `data[3]`: colorCount (e.g. 4 = 4-color BWRY, confirmed by HCI log)
- `data[4]`: screenInfo (screen type byte)

#### Image Update Time (`0x6A 0x17`) — Carousel Interval

Sets the auto-refresh interval for image carousel rotation. Sent after End Command, before Update Display.

```
TX: [0x6A, 0x17, 0x02, intervalHi, intervalLo, checksum]
RX: [0x6B, 0x17, 0x02, intervalHi, intervalLo, checksum]
```

| Interval (seconds) | Meaning |
|--------------------|---------|
| `0x0000` | Disabled (no auto-update) |
| `0x0258` (600) | 10 minutes |
| `0x0708` (1800) | 30 minutes |
| `0x0E10` (3600) | 1 hour |
| `0x1C20` (7200) | 2 hours |
| `0x5460` (21600) | 6 hours |
| `0xA8C0` (43200) | 12 hours |
| `0xFFFF` | Query current value |

#### Get Version (`0x6A 0x11`)

```
TX: [0x6A, 0x11, 0x03, 0x00, 0x00, 0x00, checksum]
RX: [0x6B, 0x11, 0x03, major, minor, patch, checksum]
```

Version string: `v{major}.{minor}.{patch}`

#### Get Battery (`0x6A 0x18`)

```
TX: [0x6A, 0x18, 0x04, 0x00, 0x00, 0x00, 0x00, checksum]
RX: [0x6B, 0x18, 0x04, chargingFlag, batteryPercent, ?, ?, checksum]
```

- `data[3]`: `0x01` = charging, `0x00` = not charging
- `data[4]`: Battery percentage (0–100)

#### Delete Image (`0x6A 0x19`)

```
TX: [0x6A, 0x19, 0x01, 0x01, checksum]
```

### Responses

The device sends notifications on the Notify characteristic. Response prefix `0x6B` for command ACKs, `0xAB` for data errors.

| Response | Meaning | Action |
|----------|---------|--------|
| `0x6B 0x01 0x08` | Start ACK | Begin sending row data |
| `0x6B 0x01 0x06` | End ACK | Image transfer complete; send next image or update |
| `0x6B 0x01 0x01` | Device busy | Display is refreshing, wait |
| `0xAB xx rH rL xx 0x00` | Transfer error | Retry from row 0 after 2s delay |

In the error response:
- `data[1]`: image index (1-based)
- `data[2-3]`: row number where error occurred (big-endian)

### Image Transfer Flow

```
Client                              Device
  │                                    │
  │──── Connect ─────────────────────►│
  │──── Set MTU 247 ─────────────────►│
  │──── Subscribe to Notify ─────────►│
  │                                    │
  │──── Clean Flash ─────────────────►│
  │◄─── Acknowledgment ──────────────│
  │          (50ms delay)              │
  │──── Start Command ───────────────►│
  │◄─── Start ACK (6B 01 08) ────────│
  │                                    │
  │──── Row 1 (0xAA frame) ─────────►│  (streamed, no per-row ACK)
  │──── Row 2 (0xAA frame) ─────────►│  (50ms delay between rows)
  │         ... repeat ...             │
  │──── Row N (0xAA frame) ─────────►│
  │──── End-of-data marker ─────────►│  AA idx FFFF 00
  │                                    │
  │──── End Command ─────────────────►│
  │◄─── End ACK (6B 01 06) ──────────│
  │                                    │
  │──── Image Update Time ──────────►│  (set carousel interval)
  │◄─── Echo ACK ────────────────────│
  │                                    │
  │──── Update (Refresh) ───────────►│
  │◄─── Echo ACK ────────────────────│
  │                                    │
```

### Multi-Image Support

The protocol supports uploading up to 9 images in sequence (carousel). For each image:
- `pushImagePosition` (0-based) tracks which image is being sent
- Protocol packets use `pushImagePosition + 1` (1-based) as `imageIndex`
- After each image completes (End ACK), the next image starts with a new Clean Flash + Start Command
- After the final image, the Update Image command triggers the display refresh

### Device Info

The device info is received as a JSON string via BLE and parsed into a `DeviceInfo` object.
HCI logs for the 428×428 BWRY device confirm `colorCount=4` and `screenInfo=8` in the
`0x6B 0x15` response:

```json
{
    "width": 428,
    "height": 428,
    "bleScreenInfo": 8,
    "deviceType": 0,
    "colorCount": 4,
    "flipHorizontal": false,
    "flipVertical": false
}
```

### Screen Types

From `DevListActivity` screen type mapping:

| screenInfo | Resolution |
|------------|------------|
| 1, 2       | 200 × 200  |
| 3          | 168 × 384  |
| 4          | 200 × 300  |
| 5          | 528 × 768  |
| 6          | 192 × 176  |
| 7          | 124 × 250  |
| 8          | 428 × 428  |

The `nubia_notes` model uses screen type **8** (428×428, 4-color BWRY).

---

## Source Files

Key source files from the APK decompilation:

| File | Purpose |
|------|---------|
| `EInkImageProcessor.java` | Pipeline orchestrator: dithering → packing |
| `EInkImageConverter.java` | HSV color classification, 2-bit packing (4096-entry LUT) |
| `ImageUtils.java` | JNI wrapper for native `libimage-lib.so` dithering + `getMyImg()` scaler |
| `ColorMatrixPhotoView.java` | Custom `PhotoView` with brightness/contrast/saturation via `ColorMatrix` |
| `ImageBean.java` | Per-image settings bean (holds seekbar values for brightness/contrast/saturation) |
| `BMPConverterUtil.java` | Java Floyd-Steinberg dithering (NFC path only, not used for BLE) |
| `ByteArrayUtils.kt` | BLE command byte construction, checksum |
| `DevImageActivity.kt` | Main activity: view capture via `draw(canvas)`, BLE transfer state machine |
| `DeviceInfo.java` | Device info bean (width, height, screen type) |
| `libimage-lib.so` | Native ARM64: `PicDither` (0x1bf00), `applyErrorDiffusion` (0x1b640), `findClosestColor` (0x1b548), `createTargetColors` (0x1b350) |
