package main

import (
	"context"
	"fmt"

	"github.com/whoisnian/misc/pkg/serial"
)

type SerialBaudrate byte

const (
	SerialBaudrate2400   = SerialBaudrate(0x00)
	SerialBaudrate4800   = SerialBaudrate(0x01)
	SerialBaudrate9600   = SerialBaudrate(0x02)
	SerialBaudrate19200  = SerialBaudrate(0x03)
	SerialBaudrate38400  = SerialBaudrate(0x04)
	SerialBaudrate57600  = SerialBaudrate(0x05)
	SerialBaudrate115200 = SerialBaudrate(0x06)
)

// 写入串口设置
func requestWriteSerialConfig(ctx context.Context, port *serial.Port, baudrate SerialBaudrate, checkResponse bool) error {
	// 发送: FC 04 93 03 01 B1 00 00 XX
	//   B1: 00~06 设置波特率依次为 2400/4800/9600/19200/38400/57600/115200，默认 9600
	//   XX: 校验和
	cmd := []byte{0xFC, 0x04, 0x93, 0x03, 0x01, byte(baudrate), 0x00, 0x00, 0x00}
	cmd[len(cmd)-1] = checksum(cmd[:len(cmd)-1])
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestWriteSerialConfig write % X", cmd)

	if checkResponse {
		// 成功: FC 00
		// 失败: FC FF 或无返回
		buf := make([]byte, 2)
		if n, err := port.Read(buf); err != nil {
			return err
		} else if n != 2 || buf[0] != 0xFC || buf[1] != 0x00 {
			return fmt.Errorf("invalid response: % X", buf[:n])
		}
		LOG.Debugf(ctx, "requestWriteSerialConfig read  % X", buf)
	}
	return nil
}

type WorkMode byte

const (
	WorkModeAuto   = WorkMode(0x00) // 定时自动发送温度模式(默认)
	WorkModeTTL    = WorkMode(0x03) // 串口命令模式
	WorkModeModbus = WorkMode(0x04) // modbus-RTU 从机模式
)

type DataFormat byte

const (
	DataFormatString = DataFormat(0x00) // 字符串格式(默认)
	DataFormatHex    = DataFormat(0x01) // 十六进制格式
	DataFormatNone   = DataFormat(0xFF) // 不返回数据
)

// 写入模式设置
func requestWriteModeConfig(ctx context.Context, port *serial.Port, mode WorkMode, format DataFormat, interval uint16, checkResponse bool) error {
	// 发送: FC 05 93 12 01 B1 B2 B3 B4 XX
	//   B1: 00 工作模式为定时自动发送温度模式(默认)
	//       03 串口命令模式
	//       04 工作模式为 modbus-RTU 从机模式
	//   B2: 00 modbus 模式下固定值 0
	//       00 非 modbus 模式下温度数据格式设为字符串格式(默认)
	//       01 非 modbus 模式下温度数据格式设为十六进制格式
	//   B3 B4: 高位在前，16位数据，单位为秒，范围 0001~0E10，最短1秒，最长1小时。
	//       在定时自动发送温度模式时此值为发送时间间隔。
	//       在 modbus 模式下时此值为温度更新时间间隔。
	//   XX: 校验和
	cmd := []byte{0xFC, 0x05, 0x93, 0x12, 0x01, byte(mode), byte(format), byte(interval >> 8), byte(interval & 0xFF), 0x00}
	cmd[len(cmd)-1] = checksum(cmd[:len(cmd)-1])
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestWriteModeConfig write % X", cmd)

	if checkResponse {
		// 成功: FC 00
		// 失败: FC FF 或无返回
		buf := make([]byte, 2)
		if n, err := port.Read(buf); err != nil {
			return err
		} else if n != 2 || buf[0] != 0xFC || buf[1] != 0x00 {
			return fmt.Errorf("invalid response: % X", buf[:n])
		}
		LOG.Debugf(ctx, "requestWriteModeConfig read  % X", buf)
	}
	return nil
}

// 转换温度
func requestConvert(ctx context.Context, port *serial.Port, format DataFormat) ([]byte, error) {
	// 发送: FC 01 93 11 B1 XX
	//   B1: FF 转换后不返回结果数据
	//       00 转换结束后返回字符串格式数据
	//       01 转换结束后返回十六进制格式数据
	//   XX: 校验和
	cmd := []byte{0xFC, 0x01, 0x93, 0x11, byte(format), 0x00}
	cmd[len(cmd)-1] = checksum(cmd[:len(cmd)-1])
	if n, err := port.Write(cmd); err != nil {
		return nil, err
	} else if n != len(cmd) {
		return nil, fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestConvert write % X", cmd)

	// 成功: FC 00 或 B1 对应的数据格式
	// 失败: FC FF 或无返回
	buf := make([]byte, 11)
	n, err := port.Read(buf)
	if err != nil {
		return nil, err
	} else if n < 2 || (buf[0] == 0xFC && buf[1] == 0xFF) {
		return nil, fmt.Errorf("invalid response: % X", buf[:n])
	}
	LOG.Debugf(ctx, "requestConvert read  % X", buf[:n])
	return buf[:n], nil
}

// 读取温度
func requestRead(ctx context.Context, port *serial.Port, format DataFormat) ([]byte, error) {
	// 发送: FC 01 93 10 B1 XX
	//   B1: 00 返回字符串格式数据
	//       01 返回十六进制格式数据
	//   XX: 校验和
	cmd := []byte{0xFC, 0x01, 0x93, 0x10, byte(format), 0x00}
	cmd[len(cmd)-1] = checksum(cmd[:len(cmd)-1])
	if n, err := port.Write(cmd); err != nil {
		return nil, err
	} else if n != len(cmd) {
		return nil, fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestRead write % X", cmd)

	// 成功: B1 对应的数据格式
	// 失败: FC FF 或无返回
	buf := make([]byte, 11)
	n, err := port.Read(buf)
	if err != nil {
		return nil, err
	} else if n < 2 || (buf[0] == 0xFC && buf[1] == 0xFF) {
		return nil, fmt.Errorf("invalid response: % X", buf[:n])
	}
	LOG.Debugf(ctx, "requestRead read  % X", buf[:n])
	return buf[:n], nil
}

// 执行恢复出厂设置
func requestRestoreFactory(ctx context.Context, port *serial.Port, checkResponse bool) error {
	// 发送: FC 00 93 0F 9E
	cmd := []byte{0xFC, 0x00, 0x93, 0x0F, 0x9E}
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestRestoreFactory write % X", cmd)

	if checkResponse {
		// 成功: FC 00
		// 失败: FC FF 或无返回
		buf := make([]byte, 2)
		if n, err := port.Read(buf); err != nil {
			return err
		} else if n != 2 || buf[0] != 0xFC || buf[1] != 0x00 {
			return fmt.Errorf("invalid response: % X", buf[:n])
		}
		LOG.Debugf(ctx, "requestRestoreFactory read  % X", buf)
	}
	return nil
}

// 执行系统复位
func requestReset(ctx context.Context, port *serial.Port, checkResponse bool) error {
	// 发送: FC 00 93 0E 9D
	cmd := []byte{0xFC, 0x00, 0x93, 0x0E, 0x9D}
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestReset write % X", cmd)

	if checkResponse {
		// 成功: FC 00
		// 失败: FC FF 或无返回
		buf := make([]byte, 2)
		if n, err := port.Read(buf); err != nil {
			return err
		} else if n != 2 || buf[0] != 0xFC || buf[1] != 0x00 {
			return fmt.Errorf("invalid response: % X", buf[:n])
		}
		LOG.Debugf(ctx, "requestReset read  % X", buf)
	}
	return nil
}

func checksum(data []byte) (result byte) {
	for _, v := range data {
		result += v
	}
	return result
}
