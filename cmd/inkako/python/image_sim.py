#!/usr/bin/env python3
"""Calculate image similarity metrics between two images.

Metrics: MSE, PSNR, SSIM, Histogram Correlation, pHash, LPIPS.
"""

import argparse
import sys
from pathlib import Path

import numpy as np
from PIL import Image


def load_image(path: str) -> Image.Image:
    img = Image.open(path).convert("RGB")
    return img


def to_grayscale(img: Image.Image) -> np.ndarray:
    return np.array(img.convert("L"), dtype=np.float64)


def to_array(img: Image.Image) -> np.ndarray:
    return np.array(img, dtype=np.float64)


def calc_mse(a: np.ndarray, b: np.ndarray) -> float:
    """Mean Squared Error between two arrays."""
    return float(np.mean((a - b) ** 2))


def calc_psnr(mse: float, max_val: float = 255.0) -> float:
    """Peak Signal-to-Noise Ratio derived from MSE."""
    if mse == 0:
        return float("inf")
    return float(10.0 * np.log10(max_val**2 / mse))


def calc_ssim(a: np.ndarray, b: np.ndarray) -> float:
    """Structural Similarity Index (grayscale, full-image mean)."""
    C1 = (0.01 * 255) ** 2
    C2 = (0.03 * 255) ** 2

    mu_a = np.mean(a)
    mu_b = np.mean(b)
    sigma_a_sq = np.var(a)
    sigma_b_sq = np.var(b)
    sigma_ab = np.mean((a - mu_a) * (b - mu_b))

    num = (2 * mu_a * mu_b + C1) * (2 * sigma_ab + C2)
    den = (mu_a**2 + mu_b**2 + C1) * (sigma_a_sq + sigma_b_sq + C2)
    return float(num / den)


def calc_histogram_correlation(a: Image.Image, b: Image.Image) -> float:
    """Histogram correlation (Pearson) on grayscale histograms."""
    hist_a = np.array(a.convert("L").histogram(), dtype=np.float64)
    hist_b = np.array(b.convert("L").histogram(), dtype=np.float64)
    ha = hist_a - np.mean(hist_a)
    hb = hist_b - np.mean(hist_b)
    denom = np.sqrt(np.sum(ha**2) * np.sum(hb**2))
    if denom == 0:
        return 0.0
    return float(np.sum(ha * hb) / denom)


def dct2(block: np.ndarray) -> np.ndarray:
    """Simple 2D DCT via matrix multiply (for small NxN blocks)."""
    N = block.shape[0]
    n = np.arange(N)
    k = n.reshape(-1, 1)
    cos_table = np.cos(np.pi * (2 * n + 1) * k / (2 * N))
    return cos_table @ block @ cos_table.T


def calc_phash(img: Image.Image, hash_size: int = 8) -> int:
    """Perceptual hash using DCT."""
    resized = img.convert("L").resize((hash_size * 4, hash_size * 4), Image.LANCZOS)
    pixels = np.array(resized, dtype=np.float64)
    dct = dct2(pixels)
    low_freq = dct[:hash_size, :hash_size]
    median = np.median(low_freq)
    bits = (low_freq > median).flatten()
    h = 0
    for b in bits:
        h = (h << 1) | int(b)
    return h


def hamming_distance(h1: int, h2: int) -> int:
    return bin(h1 ^ h2).count("1")


def calc_phash_similarity(img_a: Image.Image, img_b: Image.Image, hash_size: int = 8) -> tuple[int, float]:
    """Return (hamming distance, normalised similarity 0-1)."""
    h1 = calc_phash(img_a, hash_size)
    h2 = calc_phash(img_b, hash_size)
    total_bits = hash_size * hash_size
    dist = hamming_distance(h1, h2)
    similarity = 1.0 - dist / total_bits
    return dist, similarity


def calc_lpips(path_a: str, path_b: str) -> float | None:
    """LPIPS distance (AlexNet). Returns None if lpips/torch unavailable."""
    try:
        import lpips
    except ImportError:
        return None
    loss_fn = lpips.LPIPS(net="alex", verbose=False)
    img0 = lpips.im2tensor(lpips.load_image(path_a))
    img1 = lpips.im2tensor(lpips.load_image(path_b))
    dist = loss_fn.forward(img0, img1)
    return float(dist)


def main():
    parser = argparse.ArgumentParser(
        description="Calculate image similarity metrics between two images.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("image_a", help="Path to the first image")
    parser.add_argument("image_b", help="Path to the second image")
    parser.add_argument("--no-lpips", action="store_true", help="Skip LPIPS (avoids torch dependency)")
    args = parser.parse_args()

    for p in (args.image_a, args.image_b):
        if not Path(p).is_file():
            print(f"Error: {p} not found", file=sys.stderr)
            sys.exit(1)

    img_a = load_image(args.image_a)
    img_b = load_image(args.image_b)

    if img_a.size != img_b.size:
        print(f"Warning: resizing image B from {img_b.size} to {img_a.size}")
        img_b = img_b.resize(img_a.size, Image.LANCZOS)

    gray_a = to_grayscale(img_a)
    gray_b = to_grayscale(img_b)

    mse = calc_mse(gray_a, gray_b)
    psnr = calc_psnr(mse)
    ssim = calc_ssim(gray_a, gray_b)
    hist_corr = calc_histogram_correlation(img_a, img_b)
    phash_dist, phash_sim = calc_phash_similarity(img_a, img_b)

    lpips_val = None
    if not args.no_lpips:
        lpips_val = calc_lpips(args.image_a, args.image_b)

    w = 20

    print(f"\nImage A: {args.image_a}")
    print(f"Image B: {args.image_b}")
    print(f"Size:    {img_a.size[0]}×{img_a.size[1]}")
    print()
    print(f"{'Metric':<{w}} {'Value':>12}  {'Range':<16} {'Better'}")
    print(f"{'─' * w} {'─' * 12}  {'─' * 16} {'─' * 8}")
    print(f"{'MSE':<{w}} {mse:>12.4f}  {'[0, 65025]':<16} {'lower'}")
    psnr_str = f"{psnr:.4f}" if psnr != float("inf") else "inf"
    print(f"{'PSNR (dB)':<{w}} {psnr_str:>12}  {'[0, inf]':<16} {'higher'}")
    print(f"{'SSIM':<{w}} {ssim:>12.4f}  {'[-1, 1]':<16} {'higher'}")
    print(f"{'Hist Correlation':<{w}} {hist_corr:>12.4f}  {'[-1, 1]':<16} {'higher'}")
    print(f"{'pHash Distance':<{w}} {phash_dist:>12d}  {'[0, 64]':<16} {'lower'}")
    print(f"{'pHash Similarity':<{w}} {phash_sim:>12.4f}  {'[0, 1]':<16} {'higher'}")
    if lpips_val is not None:
        print(f"{'LPIPS':<{w}} {lpips_val:>12.4f}  {'[0, 1]':<16} {'lower'}")
    elif not args.no_lpips:
        print(f"{'LPIPS':<{w}} {'(skipped)':>12}  {'[0, 1]':<16} {'lower  (install lpips)'}")


if __name__ == "__main__":
    main()
