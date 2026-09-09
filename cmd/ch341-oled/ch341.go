//go:build linux

package main

import (
	"context"
	"fmt"
)

const (
	CH341VendorID  = 0x1A86 // QinHeng Electronics
	CH341ProductID = 0x5512 // CH341 in EPP/MEM/I2C mode

	ch341Interface = 0    // I2C 模式下只有接口 0
	ch341EpOut     = 0x02 // 批量输出端点
	ch341EpIn      = 0x82 // 批量输入端点
	ch341PacketLen = 32   // 端点最大包长
	ch341Timeout   = 1000 // 毫秒
)

// CH341 的 I2C 命令流，每个包以 CMD_I2C_STREAM 开头、以 STM_END 结尾。
const (
	cmdI2CStream = 0xAA // 命令流起始
	stmSTA       = 0x74 // 产生 START
	stmSTO       = 0x75 // 产生 STOP
	stmOUT       = 0x80 // 低 5 位为待写字节数
	stmIN        = 0xC0 // 低 5 位为待读字节数
	stmSET       = 0x60 // 低 2 位为速率档位
	stmEND       = 0x00 // 命令流结束
)

// 单包结构为 AA STA OUT|n <n 字节> STO END，共 n+5 字节，故 n 最大 27。
// 其中首字节是 I2C 地址，所以一次事务最多携带 26 字节数据。
const (
	ch341MaxPayload = ch341PacketLen - 5
	CH341MaxData    = ch341MaxPayload - 1
)

// I2C 速率档位。
const (
	SpeedLow    = 0 // 20kHz
	SpeedStd    = 1 // 100kHz
	SpeedFast   = 2 // 400kHz
	SpeedHigh   = 3 // 750kHz
	SpeedMaxIdx = 3
)

// CH341 把 USB 转 I2C 主机的能力封装为字节级读写。
type CH341 struct {
	dev *USBDevice
	buf [ch341PacketLen]byte // 复用发送缓冲，避免每次事务分配
}

func NewCH341(ctx context.Context, dev *USBDevice, speed int) (*CH341, error) {
	c := &CH341{dev: dev}
	if err := c.SetSpeed(ctx, speed); err != nil {
		return nil, err
	}
	return c, nil
}

// SetSpeed 设置 I2C 时钟速率档位。
func (c *CH341) SetSpeed(ctx context.Context, speed int) error {
	if speed < 0 || speed > SpeedMaxIdx {
		return fmt.Errorf("invalid i2c speed index %d", speed)
	}
	pkt := []byte{cmdI2CStream, stmSET | byte(speed), stmEND}
	LOG.Debugf(ctx, "ch341 set speed write % X", pkt)
	return c.write(pkt)
}

// I2CWrite 以 addr 为从机地址发起一次完整的写事务。
func (c *CH341) I2CWrite(addr uint8, data []byte) error {
	if len(data) > CH341MaxData {
		return fmt.Errorf("payload %d exceeds %d bytes per transaction", len(data), CH341MaxData)
	}
	pkt := c.buf[:0]
	pkt = append(pkt, cmdI2CStream, stmSTA, stmOUT|byte(len(data)+1), addr<<1)
	pkt = append(pkt, data...)
	pkt = append(pkt, stmSTO, stmEND)
	return c.write(pkt)
}

// I2CProbe 只发地址不发数据，根据返回字节的最高位判断从机是否 ACK。
func (c *CH341) I2CProbe(addr uint8) bool {
	// stmOUT 不带长度时 CH341 会回送应答状态，bit7 置位表示 NAK。
	pkt := []byte{cmdI2CStream, stmSTA, stmOUT, addr<<1 | 1, stmSTO, stmEND}
	if err := c.write(pkt); err != nil {
		return false
	}
	in := make([]byte, ch341PacketLen)
	n, err := c.dev.BulkTransfer(ch341EpIn, in, ch341Timeout)
	if err != nil || n < 1 {
		return false
	}
	return in[0]&0x80 == 0
}

// I2CScan 遍历有效的 7 位地址段，返回所有应答的从机地址。
func (c *CH341) I2CScan() []uint8 {
	var found []uint8
	for addr := uint8(0x08); addr < 0x78; addr++ {
		if c.I2CProbe(addr) {
			found = append(found, addr)
		}
	}
	return found
}

func (c *CH341) write(pkt []byte) error {
	n, err := c.dev.BulkTransfer(ch341EpOut, pkt, ch341Timeout)
	if err != nil {
		return err
	} else if n != len(pkt) {
		return fmt.Errorf("incomplete write: % X", pkt[:n])
	}
	return nil
}
