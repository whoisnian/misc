package main

import (
	"context"
	"fmt"

	"github.com/whoisnian/misc/pkg/serial"
)

// 转换温度到寄存器
func requestConvert(ctx context.Context, port *serial.Port) error {
	// 发送: FC 00 93 11 A0
	cmd := []byte{0xFC, 0x00, 0x93, 0x11, 0xA0}
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestConvert write % X", cmd)

	// 成功: FC 00
	// 失败: FC FF 或无返回
	buf := make([]byte, 2)
	if n, err := port.Read(buf); err != nil {
		return err
	} else if n != 2 || buf[0] != 0xFC || buf[1] != 0x00 {
		return fmt.Errorf("invalid response: % X", buf[:n])
	}
	LOG.Debugf(ctx, "requestConvert read  % X", buf)
	return nil
}

// QT18B20 温度分辨率配置
type Precision byte

var (
	Precision9bit  = Precision(0b00011111) // 最大转换时间 93.75ms
	Precision10bit = Precision(0b00111111) // 最大转换时间 187.5ms
	Precision11bit = Precision(0b01011111) // 最大转换时间 375ms
	Precision12bit = Precision(0b01111111) // 最大转换时间 750ms
)

// like binary.LittleEndian.Uint16()
func DecodeTemperature(raw []byte, p Precision) float64 {
	_ = raw[1] // bounds check
	switch p {
	case Precision9bit:
		return float64(int16(uint16(raw[1])<<8|uint16(raw[0]&0xF8))) * 0.5
	case Precision10bit:
		return float64(int16(uint16(raw[1])<<8|uint16(raw[0]&0xFC))) * 0.25
	case Precision11bit:
		return float64(int16(uint16(raw[1])<<8|uint16(raw[0]&0xFE))) * 0.125
	case Precision12bit:
		return float64(int16(uint16(raw[1])<<8|uint16(raw[0]&0xFF))) * 0.0625
	default:
		return 0
	}
}

// 写入DS18B20配置
func requestWriteConfig(ctx context.Context, port *serial.Port, precision Precision) error {
	// 发送: FC 05 93 70 D1 D2 D3 D4 D5 XX
	//   D1: TH/USER1 高温报警阈值
	//   D2: TL/USER2 低温报警阈值
	//   D3: 配置字
	//   D4: 保留/USER3
	//   D5: 保留/USER4
	//   XX: 校验和
	cmd := []byte{0xFC, 0x05, 0x93, 0x70, 0x7F, 0x80, byte(precision), 0x00, 0x00, 0x00}
	cmd[len(cmd)-1] = checksum(cmd[:len(cmd)-1])
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestWriteConfig write % X", cmd)

	// 成功: FC 00
	// 失败: FC FF (未接传感器) 或无返回 (通讯不正常)
	buf := make([]byte, 2)
	if n, err := port.Read(buf); err != nil {
		return err
	} else if n != 2 || buf[0] != 0xFC || buf[1] != 0x00 {
		return fmt.Errorf("invalid response: % X", buf[:n])
	}
	LOG.Debugf(ctx, "requestWriteConfig read  % X", buf)
	return nil
}

// 读取DS18B20配置
func requestReadConfig(ctx context.Context, port *serial.Port) ([]byte, error) {
	// 发送: FC 00 93 71 00
	cmd := []byte{0xFC, 0x00, 0x93, 0x71, 0x00}
	if n, err := port.Write(cmd); err != nil {
		return nil, err
	} else if n != len(cmd) {
		return nil, fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestReadConfig write % X", cmd)

	// 成功: FC 08 D1 D2 D3 D4 D5 D6 D7 D8 XX
	//   D1~D2: 温度值
	//   D3: TH/USER1
	//   D4: TL/USER2
	//   D5: 配置字
	//   D6: 保留
	//   D7: 保留/USER3
	//   D8: 保留/USER4
	// 失败: FC FF (未接传感器) 或无返回 (通讯不正常)
	buf := make([]byte, 11)
	if n, err := port.Read(buf); err != nil {
		return nil, err
	} else if n != 11 || buf[0] != 0xFC || buf[1] != 0x08 {
		return nil, fmt.Errorf("invalid response: % X", buf[:n])
	} else if checksum(buf[:n-1]) != buf[n-1] {
		return nil, fmt.Errorf("invalid checksum: % X", buf[:n])
	}
	LOG.Debugf(ctx, "requestReadConfig read  % X", buf)
	return buf, nil
}

// 读取DS18B20唯一ID
func requestReadID(ctx context.Context, port *serial.Port) ([]byte, error) {
	// 发送: FC 00 93 72 01
	cmd := []byte{0xFC, 0x00, 0x93, 0x72, 0x01}
	if n, err := port.Write(cmd); err != nil {
		return nil, err
	} else if n != len(cmd) {
		return nil, fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestReadID write % X", cmd)

	// 成功: FC 08 D1 D2 D3 D4 D5 D6 D7 D8 XX
	//   D1~D8: DS18B20 内部 ROM 值，共 64 位
	// 失败: FC FF (未接传感器) 或无返回 (通讯不正常)
	buf := make([]byte, 11)
	if n, err := port.Read(buf); err != nil {
		return nil, err
	} else if n != 11 || buf[0] != 0xFC || buf[1] != 0x08 {
		return nil, fmt.Errorf("invalid response: % X", buf[:n])
	} else if checksum(buf[:n-1]) != buf[n-1] {
		return nil, fmt.Errorf("invalid checksum: % X", buf[:n])
	}
	LOG.Debugf(ctx, "requestReadID read  % X", buf)
	return buf, nil
}

func checksum(data []byte) (result byte) {
	for _, v := range data {
		result += v
	}
	return result
}
