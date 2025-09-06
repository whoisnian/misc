package main

import (
	"context"
	"fmt"

	"github.com/whoisnian/misc/pkg/serial"
)

// 打开开关
func requestTurnOn(ctx context.Context, port *serial.Port) error {
	// 发送: A0 01 01 A2
	//   A0: 起始标识
	//   01: 开关地址码
	//   01: 打开
	//   A2: 校验和
	cmd := []byte{0xA0, 0x01, 0x01, 0xA2}
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestTurnOn write % X", cmd)
	return nil
}

func requestTurnOff(ctx context.Context, port *serial.Port) error {
	// 发送: A0 01 00 A1
	//   A0: 起始标识
	//   01: 开关地址码
	//   00: 关闭
	//   A1: 校验和
	cmd := []byte{0xA0, 0x01, 0x00, 0xA1}
	if n, err := port.Write(cmd); err != nil {
		return err
	} else if n != len(cmd) {
		return fmt.Errorf("incomplete write: % X", cmd[:n])
	}
	LOG.Debugf(ctx, "requestTurnOff write % X", cmd)
	return nil
}
