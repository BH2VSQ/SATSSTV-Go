package sstv

import (
	"context"
	"fmt"
	"math"
	"testing"
)

func TestDecodeRobustToNoise(t *testing.T) {
	const rate = 48_000
	encoded, _ := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	scale := 0.03 / 0.8
	for _, snrDB := range []float64{15, 10, 6, 3, 0, -3, -6} {
		noiseAmp := 0.03 / math.Pow(10, snrDB/20)
		samples := make([]int16, len(encoded))
		state := uint32(0x12345678)
		for i := range encoded {
			state = state*1664525 + 1013904223
			n := int32((state>>16)&0x1ff) - 256
			v := float64(encoded[i])*scale + float64(n)*noiseAmp/256.0*32768
			if v > 32767 {
				v = 32767
			}
			if v < -32768 {
				v = -32768
			}
			samples[i] = int16(v)
		}
		_, _, _, err := DetectModeHeader(samples, rate)
		if err != nil {
			fmt.Printf("SNR %3.0f dB: header FAIL\n", snrDB)
			continue
		}
		if _, err := Decode(context.Background(), samples, rate); err != nil {
			fmt.Printf("SNR %3.0f dB: decode FAIL\n", snrDB)
			continue
		}
		fmt.Printf("SNR %3.0f dB: OK\n", snrDB)
	}
}
