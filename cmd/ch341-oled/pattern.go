//go:build linux

package main

import (
	"fmt"
	"strings"
	"time"
)

// 可用的测试图案。
const (
	PatternAll     = "all"     // 全屏点亮，用于检查坏点和亮度
	PatternChecker = "checker" // 棋盘格，用于检查像素粒度
	PatternBorder  = "border"  // 边框加对角线，用于校验行列映射
	PatternText    = "text"    // 文字，用于检查可读性
	PatternSweep   = "sweep"   // 竖条来回扫动，用于确认实时刷新
	PatternOff     = "off"     // 关闭显示
)

var patternNames = []string{PatternAll, PatternChecker, PatternBorder, PatternText, PatternSweep, PatternOff}

// Render 绘制指定图案并刷新到屏幕。
func Render(d *Display, pattern string, sweep time.Duration) error {
	switch pattern {
	case PatternAll:
		d.Clear(true)
	case PatternChecker:
		drawChecker(d)
	case PatternBorder:
		drawBorder(d)
	case PatternText:
		drawText(d)
	case PatternSweep:
		if err := drawSweep(d, sweep); err != nil {
			return err
		}
		drawText(d)
	case PatternOff:
		return d.Off()
	default:
		return fmt.Errorf("unknown pattern %q, want one of %s", pattern, strings.Join(patternNames, "|"))
	}
	return d.Show()
}

func drawChecker(d *Display) {
	d.Clear(false)
	for y := range d.Height() {
		for x := range d.Width() {
			if (x/4+y/4)%2 == 1 {
				d.Pixel(x, y, true)
			}
		}
	}
}

// drawBorder 画满屏边框和两条对角线：边框贴合可视区说明多路复用比正确，
// 对角线交于正中说明行列映射和 COM 引脚配置正确。
func drawBorder(d *Display) {
	d.Clear(false)
	w, h := d.Width(), d.Height()
	for x := range w {
		d.Pixel(x, 0, true)
		d.Pixel(x, h-1, true)
		d.Pixel(x, x*(h-1)/(w-1), true)
		d.Pixel(x, h-1-x*(h-1)/(w-1), true)
	}
	for y := range h {
		d.Pixel(0, y, true)
		d.Pixel(w-1, y, true)
	}
}

func drawText(d *Display) {
	d.Clear(false)
	d.Text(2, 1, "CH341 I2C OLED")
	d.Text(2, 11, fmt.Sprintf("%s %dX%d", d.Chip(), d.Width(), d.Height()))
	if d.Height() >= 32 {
		d.Text(2, 21, fmt.Sprintf("ADDR 0X%02X OK", d.Addr()))
	}
	if d.Height() >= 64 {
		d.Text(2, 33, "0123456789")
		d.Text(2, 43, "ABCDEFGHIJKLM")
		d.Text(2, 53, "NOPQRSTUVWXYZ")
	}
}

// drawSweep 让一根竖条来回扫动，肉眼可确认屏幕在实时刷新而非停留在残留画面。
func drawSweep(d *Display, duration time.Duration) error {
	const barWidth = 6
	pos, step := 0, 4
	for deadline := time.Now().Add(duration); time.Now().Before(deadline); {
		d.Clear(false)
		for dx := range barWidth {
			for y := range d.Height() {
				d.Pixel(pos+dx, y, true)
			}
		}
		d.Text(4, d.Height()/2-3, "RUNNING")
		if err := d.Show(); err != nil {
			return err
		}
		pos += step
		if pos <= 0 || pos+barWidth >= d.Width() {
			step = -step
		}
	}
	return nil
}
