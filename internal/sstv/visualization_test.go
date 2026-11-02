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


func TestCorrectedDisplayTrackerMovesRetunedToneBackToNominalFrequency(t *testing.T) {
	const rate = 48_000
	const offset = 300.0
	samples := make([]int16, 8_192)
	for i := range samples {
		samples[i] = int16(0.4 * math.Sin(2*math.Pi*2_200*float64(i)/rate) * 32767)
	}
	tracker := NewCorrectedDisplayFrequencyTracker(rate)
	if got := tracker.SampleRate(); got < 10_000 || got > 16_000 {
		t.Fatalf("display discriminator rate = %d Hz, want approximately 12 kHz", got)
	}
	track := tracker.Process(samples, offset)
	if len(track) < len(samples)/5 || len(track) > len(samples)/3 {
		t.Fatalf("decimated frequency track has %d points from %d input samples; want approximately one quarter", len(track), len(samples))
	}
	var sum float64
	count := 0
	for _, f := range track[1_000:] {
		if f > 1_700 && f < 2_100 {
			sum += f
			count++
		}
	}
	if count == 0 {
		t.Fatal("no corrected frequency samples were produced")
	}
	mean := sum / float64(count)
	if math.Abs(mean-1_900) > 25 {
		t.Fatalf("corrected frequency mean = %.1f Hz, want 1900 ±25 Hz", mean)
	}
}

func TestFrequencyHistogramFromCorrectedTrackAlignsNominalTones(t *testing.T) {
	const rate = 48_000
	track := make([]float64, 8_192)
	for i := range track {
		track[i] = 1_900
	}
	histogram := FrequencyHistogramFromTrack(track, rate, 2_048, 700, 2_700)
	peakIndex := 0
	for i := 1; i < len(histogram); i++ {
		if histogram[i] > histogram[peakIndex] {
			peakIndex = i
		}
	}
	peakHz := 700 + float64(peakIndex)*2_000/float64(len(histogram)-1)
	if math.Abs(peakHz-1_900) > 10 {
		t.Fatalf("corrected histogram peak = %.1f Hz, want 1900 ±10 Hz", peakHz)
	}
}

func TestBuildAFCFrameWaterfallUsesPerLineOffsetHistory(t *testing.T) {
	const rate = 10_000
	spec := ModeSpec{
		Family: FamilyRobot, Width: 320, Height: 4,
		SyncMS: 1, SyncPorchMS: 1, LumaMS: 4,
		SeparatorMS: 1, ColorPorchMS: 1, ChromaMS: 2,
	}
	const bodyStart = 100
	frequency := make([]float64, bodyStart+rate)
	for i := bodyStart; i < bodyStart+100; i++ {
		frequency[i] = 2_200 // Receiver is 300 Hz high on the first raster line.
	}
	for i := bodyStart + 100; i < bodyStart+400; i++ {
		frequency[i] = 1_500
	}
	offsets := []AFCOffsetPoint{
		{SampleIndex: 0, OffsetHz: 300},
		{SampleIndex: bodyStart + 100, OffsetHz: 0},
	}
	waterfall := BuildAFCFrameWaterfall(frequency, rate, spec, bodyStart, offsets, 201, 700, 2_700)
	if waterfall == nil || waterfall.Bounds().Dx() != 201 || waterfall.Bounds().Dy() != 4 {
		t.Fatalf("unexpected waterfall dimensions: %v", waterfall)
	}
	// 1900 Hz maps to x=120; 1500 Hz maps to x=80 in a 201-bin 700..2700 plot.
	if got := waterfall.GrayAt(120, 0).Y; got < 200 {
		t.Fatalf("first line was not shifted from 2200 to 1900 Hz (pixel intensity %d)", got)
	}
	if got := waterfall.GrayAt(80, 1).Y; got < 200 {
		t.Fatalf("second line did not retain its 1500 Hz frequency (pixel intensity %d)", got)
	}
	if got := waterfall.GrayAt(150, 0).Y; got > 10 {
		t.Fatalf("uncorrected 2200 Hz bin remains bright on first line (intensity %d)", got)
	}
}
