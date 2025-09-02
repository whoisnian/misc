package main

import (
	"math"
	"testing"
)

const epsilon = 1e-9

func TestDecodeTemperature(t *testing.T) {
	var tests = []struct {
		input []byte
		prec  Precision
		want  float64
	}{
		{[]byte{0xD0, 0x07}, Precision12bit, 125},
		{[]byte{0x50, 0x05}, Precision12bit, 85},
		{[]byte{0x91, 0x01}, Precision12bit, 25.0625},
		{[]byte{0xA2, 0x00}, Precision12bit, 10.125},
		{[]byte{0x08, 0x00}, Precision12bit, 0.5},
		{[]byte{0x00, 0x00}, Precision12bit, 0},
		{[]byte{0xF8, 0xFF}, Precision12bit, -0.5},
		{[]byte{0x5E, 0xFF}, Precision12bit, -10.125},
		{[]byte{0x6F, 0xFE}, Precision12bit, -25.0625},
		{[]byte{0x90, 0xFC}, Precision12bit, -55},
	}
	for _, test := range tests {
		if got := DecodeTemperature(test.input, test.prec); math.Abs(got-test.want) > epsilon {
			t.Errorf("DecodeTemperature(%X, %v) = %v; want %v", test.input, test.prec, got, test.want)
		}
	}
}
