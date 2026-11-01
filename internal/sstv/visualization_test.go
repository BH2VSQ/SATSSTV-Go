package sstv

import (
	"fmt"
	"math"
	"testing"
)

func TestMMSSTVSpectrumWindowMatchesDefaultResampling(t *testing.T) {
	cases := []struct{ rate, want int }{
		{48_000, 8192},
		{44_100, 8192},
		{24_000, 4096},
		{22_050, 2048},
		{16_000, 2048},
		{8_000, 1024},
	}
	for _, tc := range cases {
		if got := MMSSTVSpectrumWindowSamples(tc.rate); got != tc.want {
			t.Errorf("window at %d Hz = %d samples, want %d", tc.rate, got, tc.want)
		}
	}
}

func TestMMSSTVFrequencyHistogramFindsReferenceTones(t *testing.T) {
	for _, rate := range []int{48_000, 24_000, 16_000, 8_000} {
		for _, toneHz := range []float64{1200, 1500, 1900, 2300} {
			t.Run(fmtToneCase(rate, toneHz), func(t *testing.T) {
				window := MMSSTVSpectrumWindowSamples(rate)
				samples := make([]int16, window)
				for i := range samples {
					v := 0.4 * math.Sin(2*math.Pi*toneHz*float64(i)/float64(rate))
					samples[i] = int16(v * 32767)
				}
				const bins = 2048
				histogram := MMSSTVFrequencyHistogram(samples, rate, bins, 700, 2700)
				peakIndex := 0
				for i := 1; i < len(histogram); i++ {
					if histogram[i] > histogram[peakIndex] {
						peakIndex = i
					}
				}
				peakHz := 700 + float64(peakIndex)*2000/float64(bins)
				tolerance := 12.0
				if rate == 8_000 {
					// At 8 kHz the zero-crossing interpolation has larger
					// quantization error near Nyquist than MMSSTV's normal
					// 11.025/12 kHz discriminator rate.
					tolerance = 35
				}
				if math.Abs(peakHz-toneHz) > tolerance {
					t.Fatalf("peak %.1f Hz, want %.1f Hz (rate %d)", peakHz, toneHz, rate)
				}
				if histogram[peakIndex] < 0.8 {
					t.Fatalf("peak occupancy %.3f is too low at %.1f Hz", histogram[peakIndex], peakHz)
				}
			})
		}
	}
}

func TestMMSSTVFrequencyHistogramSilenceIsBlack(t *testing.T) {
	got := MMSSTVFrequencyHistogram(make([]int16, 8192), 48_000, 2048, 700, 2700)
	for i, value := range got {
		if value != 0 {
			t.Fatalf("silence bin %d = %.3f, want 0", i, value)
		}
	}
}

func fmtToneCase(rate int, tone float64) string {
	return fmt.Sprintf("%dHz_%d", int(tone), rate)
}
