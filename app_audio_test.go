package main

import (
	"math"
	"testing"
)

func TestMakeSpectrumDetects1500HzTone(t *testing.T) {
	const sampleRate = 48_000
	samples := make([]float32, 4096)
	for i := range samples {
		samples[i] = float32(0.5 * math.Sin(2*math.Pi*1500*float64(i)/sampleRate))
	}
	spectrum := makeSpectrum(samples, sampleRate)
	if len(spectrum) != 72 {
		t.Fatalf("makeSpectrum returned %d bins, want 72", len(spectrum))
	}
	peakIndex := 0
	for i := 1; i < len(spectrum); i++ {
		if spectrum[i] > spectrum[peakIndex] {
			peakIndex = i
		}
	}
	peakFrequency := 900.0 + float64(peakIndex)*(1900.0/float64(len(spectrum)-1))
	if math.Abs(peakFrequency-1500) > 20 {
		t.Fatalf("detected peak at %.1f Hz, want approximately 1500 Hz", peakFrequency)
	}
	if spectrum[peakIndex] < 0.8 {
		t.Fatalf("1500 Hz peak is too weak after normalization: %.3f", spectrum[peakIndex])
	}
}

func TestMakeSpectrumHandlesEmptyInput(t *testing.T) {
	spectrum := makeSpectrum(nil, 48_000)
	if len(spectrum) != 72 {
		t.Fatalf("makeSpectrum returned %d bins for empty input, want 72", len(spectrum))
	}
	for i, value := range spectrum {
		if value != 0 {
			t.Fatalf("empty input bin %d = %f, want 0", i, value)
		}
	}
}
