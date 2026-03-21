package main

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"

	"golang.org/x/image/draw"
)

// 4-color palette: Index 0=Black, 1=White, 2=Yellow, 3=Red
// Matches the BLE encoding order in EInkImageConverter.
type paletteColor struct{ r, g, b int32 }

var palette = [4]paletteColor{
	{0, 0, 0},       // 0 = Black
	{255, 255, 255}, // 1 = White
	{255, 255, 0},   // 2 = Yellow
	{255, 0, 0},     // 3 = Red
}

var paletteNames = [4]string{"Black", "White", "Yellow", "Red"}

// colorAdjustParams holds the three seekbar values from the Android app.
type colorAdjustParams struct {
	Brightness int // default 51
	Contrast   int // default 30
	Saturation int // default 23
}

func defaultColorAdjust() colorAdjustParams {
	return colorAdjustParams{51, 30, 23}
}

// applyColorAdjustments replicates Android ColorMatrixPhotoView.
//
// Transform order (pixel space): brightness -> contrast -> saturation
// Combined formula:
//
//	R' = c*(1+0.3*(s-1))*R + (1-c)*127.5 + (b-1)*255  (clamped 0-255)
//	G' = c*(1+0.15*(s-1))*G + same offset
//	B' = c*B + same offset
//
// where b = brightness/50, c = contrast/20, s = saturation/20.
func applyColorAdjustments(src image.Image, params colorAdjustParams) *image.NRGBA {
	b := float64(params.Brightness) / 50.0
	c := float64(params.Contrast) / 20.0
	s := float64(params.Saturation) / 20.0

	d := s - 1.0
	satR := 1.0 + 0.3*d
	satG := 1.0 + 0.15*d

	totalOffset := (1.0-c)*0.5*255.0 + (b-1.0)*255.0
	scaleR := c * satR
	scaleG := c * satG
	scaleB := c

	bounds := src.Bounds()
	dst := image.NewNRGBA(bounds)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cr, cg, cb, ca := src.At(x, y).RGBA()
			// RGBA() returns [0, 65535]; convert to [0, 255]
			rf := float64(cr>>8)*scaleR + totalOffset
			gf := float64(cg>>8)*scaleG + totalOffset
			bf := float64(cb>>8)*scaleB + totalOffset
			dst.SetNRGBA(x, y, color.NRGBA{
				R: clampUint8(rf),
				G: clampUint8(gf),
				B: clampUint8(bf),
				A: uint8(ca >> 8),
			})
		}
	}
	return dst
}

func clampUint8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

// intTruncDiv16 implements C-style truncation-toward-zero arithmetic-shift-right by 4.
// Matches native applyErrorDiffusion: (x < 0) ? (x+15)>>4 : x>>4
func intTruncDiv16(v int32) int32 {
	if v < 0 {
		return -(-v >> 4)
	}
	return v >> 4
}

func clampInt32(v int32) int32 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// floydSteinbergDither performs Floyd-Steinberg dithering matching the native
// libimage-lib.so PicDither() + applyErrorDiffusion(). Pixel arithmetic is
// int32 (matching the Java int[] JNI layer) and error distribution uses
// C-style truncation-toward-zero division by 16.
//
// Returns an HxW slice of palette indices (0-3).
func floydSteinbergDither(src *image.NRGBA) [][]uint8 {
	bounds := src.Bounds()
	w := bounds.Max.X - bounds.Min.X
	h := bounds.Max.Y - bounds.Min.Y

	// Working pixel buffer [h*w][3]int32
	buf := make([][3]int32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.NRGBAAt(bounds.Min.X+x, bounds.Min.Y+y)
			buf[y*w+x] = [3]int32{int32(c.R), int32(c.G), int32(c.B)}
		}
	}

	result := make([][]uint8, h)
	for y := range result {
		result[y] = make([]uint8, w)
	}

	// Luma weights (same as native float32 values for 0.299, 0.587, 0.114)
	const wr, wg, wb = 0.299, 0.587, 0.114

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px := buf[y*w+x]
			r0, g0, b0 := px[0], px[1], px[2]

			// Find nearest palette color (float64 weighted Euclidean distance).
			// Strict < means first tie keeps index 0 (matches native).
			bestIdx := 0
			bestDist := math.MaxFloat64
			for i := 0; i < 4; i++ {
				dr := (float64(r0) - float64(palette[i].r)) * wr
				dg := (float64(g0) - float64(palette[i].g)) * wg
				db := (float64(b0) - float64(palette[i].b)) * wb
				d := dr*dr + dg*dg + db*db
				if d < bestDist {
					bestDist = d
					bestIdx = i
				}
			}
			result[y][x] = uint8(bestIdx)

			// Error = original - palette color
			er := r0 - palette[bestIdx].r
			eg := g0 - palette[bestIdx].g
			eb := b0 - palette[bestIdx].b

			// Distribute error (Floyd-Steinberg weights: 7, 3, 5, 1)
			// Right (x+1, y): weight 7/16
			if x+1 < w {
				p := &buf[y*w+x+1]
				p[0] = clampInt32(p[0] + intTruncDiv16(er*7))
				p[1] = clampInt32(p[1] + intTruncDiv16(eg*7))
				p[2] = clampInt32(p[2] + intTruncDiv16(eb*7))
			}
			if y+1 < h {
				// Below-left (x-1, y+1): weight 3/16
				if x > 0 {
					p := &buf[(y+1)*w+x-1]
					p[0] = clampInt32(p[0] + intTruncDiv16(er*3))
					p[1] = clampInt32(p[1] + intTruncDiv16(eg*3))
					p[2] = clampInt32(p[2] + intTruncDiv16(eb*3))
				}
				// Below (x, y+1): weight 5/16
				{
					p := &buf[(y+1)*w+x]
					p[0] = clampInt32(p[0] + intTruncDiv16(er*5))
					p[1] = clampInt32(p[1] + intTruncDiv16(eg*5))
					p[2] = clampInt32(p[2] + intTruncDiv16(eb*5))
				}
				// Below-right (x+1, y+1): weight 1/16
				if x+1 < w {
					p := &buf[(y+1)*w+x+1]
					p[0] = clampInt32(p[0] + intTruncDiv16(er))
					p[1] = clampInt32(p[1] + intTruncDiv16(eg))
					p[2] = clampInt32(p[2] + intTruncDiv16(eb))
				}
			}
		}
	}

	return result
}

// hsvClassifyPixel maps an RGB pixel to a 4-color palette index using HSV
// thresholds. Matches EInkImageConverter.convertPixelToColorOptimized().
func hsvClassifyPixel(r, g, b uint8) uint8 {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0

	maxC := math.Max(rf, math.Max(gf, bf))
	minC := math.Min(rf, math.Min(gf, bf))
	delta := maxC - minC
	v := maxC

	var s, hDeg float64
	if maxC != 0 {
		s = delta / maxC
	}
	if delta != 0 {
		switch maxC {
		case rf:
			hDeg = 60.0 * math.Mod((gf-bf)/delta, 6.0)
		case gf:
			hDeg = 60.0 * ((bf-rf)/delta + 2.0)
		default:
			hDeg = 60.0 * ((rf-gf)/delta + 4.0)
		}
		if hDeg < 0 {
			hDeg += 360.0
		}
	}

	switch {
	case v < 0.3:
		return 0 // Black
	case v > 0.85 && s < 0.2:
		return 1 // White
	case (hDeg <= 15.0 || hDeg >= 345.0) && s > 0.6:
		return 3 // Red
	case hDeg >= 45.0 && hDeg <= 65.0 && s > 0.5:
		return 2 // Yellow
	case v > 0.5:
		return 1 // White fallback
	default:
		return 0 // Black fallback
	}
}

// directClassify classifies every pixel by HSV thresholds without dithering.
func directClassify(src *image.NRGBA) [][]uint8 {
	bounds := src.Bounds()
	w := bounds.Max.X - bounds.Min.X
	h := bounds.Max.Y - bounds.Min.Y
	result := make([][]uint8, h)
	for y := 0; y < h; y++ {
		result[y] = make([]uint8, w)
		for x := 0; x < w; x++ {
			c := src.NRGBAAt(bounds.Min.X+x, bounds.Min.Y+y)
			result[y][x] = hsvClassifyPixel(c.R, c.G, c.B)
		}
	}
	return result
}

// packEInkData packs 2-bit color indices into bytes (4 pixels per byte, MSB
// first) matching EInkImageConverter.convertBitmapToEInkDataOptimized():
//
//	byte = (px0<<6) | (px1<<4) | (px2<<2) | px3
//
// Remaining pixels in the last byte are padded with White (1).
func packEInkData(indices [][]uint8) []byte {
	h := len(indices)
	if h == 0 {
		return nil
	}
	w := len(indices[0])
	nPixels := h * w
	nBytes := (nPixels + 3) / 4
	out := make([]byte, nBytes)

	i := 0
	byteIdx := 0
	var acc uint8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			shift := uint(6 - (i%4)*2)
			acc |= (indices[y][x] & 0x03) << shift
			i++
			if i%4 == 0 {
				out[byteIdx] = acc
				byteIdx++
				acc = 0
			}
		}
	}
	// Handle partial last byte: pad remaining slots with White (1)
	if i%4 != 0 {
		remaining := 4 - (i % 4)
		for k := 0; k < remaining; k++ {
			shift := uint((remaining - 1 - k) * 2)
			acc |= (1 & 0x03) << shift
		}
		out[byteIdx] = acc
	}

	return out
}

// indicesToRGBA converts a palette index array to an RGBA image for preview export.
func indicesToRGBA(indices [][]uint8) *image.NRGBA {
	h := len(indices)
	if h == 0 {
		return image.NewNRGBA(image.Rectangle{})
	}
	w := len(indices[0])
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := palette[indices[y][x]]
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(p.r),
				G: uint8(p.g),
				B: uint8(p.b),
				A: 255,
			})
		}
	}
	return img
}

// ConvertResult holds the output of the image conversion pipeline.
type ConvertResult struct {
	Indices [][]uint8
	Packed  []byte
	Width   int
	Height  int
}

// convertImage runs the full image conversion pipeline:
//  1. Load and resize to target dimensions (Catmull-Rom)
//  2. Apply brightness/contrast/saturation adjustments
//  3. Floyd-Steinberg dither (or direct HSV classification)
//  4. Pack to 2-bit packed binary
func convertImage(inputPath string, width, height int, useDither bool, params colorAdjustParams) (*ConvertResult, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// Resize using Catmull-Rom (highest quality in golang.org/x/image/draw)
	dstRect := image.Rect(0, 0, width, height)
	resized := image.NewNRGBA(dstRect)
	draw.CatmullRom.Scale(resized, dstRect, src, src.Bounds(), draw.Over, nil)

	adjusted := applyColorAdjustments(resized, params)

	var indices [][]uint8
	if useDither {
		indices = floydSteinbergDither(adjusted)
	} else {
		indices = directClassify(adjusted)
	}

	packed := packEInkData(indices)

	expectedSize := (width*height + 3) / 4
	if len(packed) != expectedSize {
		return nil, fmt.Errorf("data size mismatch: got %d, expected %d", len(packed), expectedSize)
	}

	return &ConvertResult{
		Indices: indices,
		Packed:  packed,
		Width:   width,
		Height:  height,
	}, nil
}

// printColorDistribution prints the count and percentage of each palette color.
func printColorDistribution(indices [][]uint8) {
	counts := [4]int{}
	total := 0
	for _, row := range indices {
		for _, v := range row {
			counts[v]++
			total++
		}
	}
	fmt.Println("Color distribution:")
	for i, name := range paletteNames {
		fmt.Printf("  %s: %d (%.1f%%)\n", name, counts[i], float64(counts[i])/float64(total)*100)
	}
}
