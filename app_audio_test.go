package main

import (
	"math"
	"testing"
)

func TestMakeSpectrumDetects1500HzTone(t *testing.T) {
	const sampleRate = 48_000
	samples := make([]float32, spectrumFFTSize)
	for i := range samples {
		samples[i] = float32(0.5 * math.Sin(2*math.Pi*1500*float64(i)/sampleRate))
	}
	spectrum := makeSpectrum(samples, sampleRate)
	if len(spectrum) != spectrumBins {
		t.Fatalf("makeSpectrum returned %d bins, want %d", len(spectrum), spectrumBins)
	}
	peakIndex := 0
	for i := 1; i < len(spectrum); i++ {
		if spectrum[i] > spectrum[peakIndex] {
			peakIndex = i
		}
	}
	peakFrequency := 900.0 + float64(peakIndex)*(1900.0/float64(len(spectrum)-1))
	if math.Abs(peakFrequency-1500) > 5 {
		t.Fatalf("detected peak at %.1f Hz, want approximately 1500 Hz", peakFrequency)
	}
	if spectrum[peakIndex] < 0.8 {
		t.Fatalf("1500 Hz peak is too weak after normalization: %.3f", spectrum[peakIndex])
	}
}

func TestMakeSpectrumHandlesEmptyInput(t *testing.T) {
	spectrum := makeSpectrum(nil, 48_000)
	if len(spectrum) != spectrumBins {
		t.Fatalf("makeSpectrum returned %d bins for empty input, want %d", len(spectrum), spectrumBins)
	}
	for i, value := range spectrum {
		if value != 0 {
			t.Fatalf("empty input bin %d = %f, want 0", i, value)
		}
	}
}

func TestMakeSpectrumPreservesInputLevel(t *testing.T) {
	const sampleRate = 48_000
	makeTone := func(amplitude float64) []float32 {
		samples := make([]float32, spectrumFFTSize)
		for i := range samples {
			samples[i] = float32(amplitude * math.Sin(2*math.Pi*1500*float64(i)/sampleRate))
		}
		return samples
	}
	loud := makeSpectrum(makeTone(0.5), sampleRate)
	quiet := makeSpectrum(makeTone(0.05), sampleRate)
	peak := func(values []float64) float64 {
		maximum := 0.0
		for _, value := range values {
			if value > maximum {
				maximum = value
			}
		}
		return maximum
	}
	if peak(loud)-peak(quiet) < 0.12 {
		t.Fatalf("spectrum appears to normalize away input level: loud %.3f, quiet %.3f", peak(loud), peak(quiet))
	}
}
