//go:build linux

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// 支持的驱动芯片，两者命令集不完全兼容。
const (
	ChipSSD1306 = "ssd1306"
	ChipSH1106  = "sh1106"
)

// 每次数据事务的首字节为控制字节，其余为 GDDRAM 数据。
const (
	ctrlCommand = 0x00
	ctrlData    = 0x40
	maxDataLen  = CH341MaxData - 1
)

// Display 维护一块单色 OLED 的帧缓冲，按页组织，每字节表示同一列的 8 行像素。
type Display struct {
	bus    *CH341
	addr   uint8
	chip   string
	width  int
	height int
	pages  int
	colOff int
	buf    []byte
}

// NewDisplay 创建显示实例，colOff 为负时按芯片选用默认列偏移。
func NewDisplay(bus *CH341, addr uint8, chip string, width, height, colOff int) (*Display, error) {
	if chip != ChipSSD1306 && chip != ChipSH1106 {
		return nil, fmt.Errorf("unsupported chip %q, want %q or %q", chip, ChipSSD1306, ChipSH1106)
	}
	if width <= 0 || height <= 0 || height%8 != 0 {
		return nil, fmt.Errorf("invalid resolution %dx%d", width, height)
	}
	if colOff < 0 {
		// SH1106 内部 RAM 有 132 列，128 列的面板居中放置，故偏移 2。
		colOff = 0
		if chip == ChipSH1106 {
			colOff = 2
		}
	}
	return &Display{
		bus:    bus,
		addr:   addr,
		chip:   chip,
		width:  width,
		height: height,
		pages:  height / 8,
		colOff: colOff,
		buf:    make([]byte, width*height/8),
	}, nil
}

// ParseSize 解析形如 128x32 的分辨率描述。
func ParseSize(s string) (width, height int, err error) {
	parts := strings.SplitN(strings.ToLower(s), "x", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid size %q, want like 128x32", s)
	}
	if width, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if height, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return width, height, nil
}

func (d *Display) Chip() string { return d.chip }
func (d *Display) Width() int   { return d.width }
func (d *Display) Height() int  { return d.height }
func (d *Display) Addr() uint8  { return d.addr }
func (d *Display) ColOff() int  { return d.colOff }

// Command 逐条下发命令字节。
func (d *Display) Command(cmds ...byte) error {
	for _, c := range cmds {
		if err := d.bus.I2CWrite(d.addr, []byte{ctrlCommand, c}); err != nil {
			return err
		}
	}
	return nil
}

// Init 按芯片类型下发初始化序列。
func (d *Display) Init(ctx context.Context) error {
	mux := byte(d.height - 1)
	// 128x32 面板为顺序 COM 排列，128x64 为交替排列加左右反转。
	comPins := byte(0x12)
	if d.height == 32 {
		comPins = 0x02
	}
	LOG.Debugf(ctx, "init %s %dx%d mux=%#02x compins=%#02x", d.chip, d.width, d.height, mux, comPins)

	if d.chip == ChipSH1106 {
		return d.Command(
			0xAE,       // 关闭显示
			0xD5, 0x80, // 时钟分频与振荡频率
			0xA8, mux, // 多路复用比
			0xD3, 0x00, // 显示偏移
			0x40,       // 起始行 0
			0xAD, 0x8B, // 打开内部 DC-DC（SH1106 专有）
			0x32,          // 泵电压 8.0V（SH1106 专有）
			0xA1,          // 列地址反转
			0xC8,          // COM 反向扫描
			0xDA, comPins, // COM 引脚配置
			0x81, 0x80, // 对比度
			0xD9, 0x22, // 预充电周期
			0xDB, 0x35, // VCOMH 电压
			0xA4, // 显示 GDDRAM 内容
			0xA6, // 正常显示（非反色）
			0xAF, // 打开显示
		)
	}
	return d.Command(
		0xAE,       // 关闭显示
		0xD5, 0x80, // 时钟分频与振荡频率
		0xA8, mux, // 多路复用比
		0xD3, 0x00, // 显示偏移
		0x40,       // 起始行 0
		0x8D, 0x14, // 打开电荷泵（SSD1306 专有）
		0x20, 0x00, // 水平寻址模式（SSD1306 专有）
		0xA1,          // 列地址反转
		0xC8,          // COM 反向扫描
		0xDA, comPins, // COM 引脚配置
		0x81, 0xCF, // 对比度
		0xD9, 0xF1, // 预充电周期
		0xDB, 0x40, // VCOMH 电压
		0xA4, // 显示 GDDRAM 内容
		0xA6, // 正常显示（非反色）
		0xAF, // 打开显示
	)
}

// Off 关闭显示但保留 GDDRAM 内容。
func (d *Display) Off() error { return d.Command(0xAE) }

// Show 把帧缓冲写入 GDDRAM。
func (d *Display) Show() error {
	if d.chip == ChipSH1106 {
		// SH1106 没有水平寻址模式，只能逐页设置页地址和列地址。
		for page := range d.pages {
			if err := d.Command(
				0xB0|byte(page),        // 页地址
				byte(d.colOff&0x0F),    // 列地址低 4 位
				0x10|byte(d.colOff>>4), // 列地址高 4 位
			); err != nil {
				return err
			}
			row := d.buf[page*d.width : (page+1)*d.width]
			if err := d.writeData(row); err != nil {
				return err
			}
		}
		return nil
	}
	if err := d.Command(0x21, byte(d.colOff), byte(d.colOff+d.width-1)); err != nil {
		return err // 列地址范围
	}
	if err := d.Command(0x22, 0, byte(d.pages-1)); err != nil {
		return err // 页地址范围
	}
	return d.writeData(d.buf)
}

func (d *Display) writeData(data []byte) error {
	pkt := make([]byte, 0, maxDataLen+1)
	for i := 0; i < len(data); i += maxDataLen {
		end := min(i+maxDataLen, len(data))
		pkt = append(pkt[:0], ctrlData)
		pkt = append(pkt, data[i:end]...)
		if err := d.bus.I2CWrite(d.addr, pkt); err != nil {
			return err
		}
	}
	return nil
}

// Clear 将整屏填充为全灭或全亮。
func (d *Display) Clear(on bool) {
	fill := byte(0x00)
	if on {
		fill = 0xFF
	}
	for i := range d.buf {
		d.buf[i] = fill
	}
}

// Pixel 设置单个像素，越界坐标会被忽略。
func (d *Display) Pixel(x, y int, on bool) {
	if x < 0 || x >= d.width || y < 0 || y >= d.height {
		return
	}
	idx := (y/8)*d.width + x
	if on {
		d.buf[idx] |= 1 << (y % 8)
	} else {
		d.buf[idx] &^= 1 << (y % 8)
	}
}

// Text 以内置 5x7 点阵在指定位置绘制文本，小写字母按大写处理。
func (d *Display) Text(x, y int, s string) {
	for _, r := range strings.ToUpper(s) {
		glyph, ok := font5x7[r]
		if !ok {
			glyph = font5x7['?']
		}
		for col, bits := range glyph {
			for row := range 7 {
				if bits&(1<<row) != 0 {
					d.Pixel(x+col, y+row, true)
				}
			}
		}
		x += len(glyph) + 1 // 字符间留 1 列间隔
	}
}
