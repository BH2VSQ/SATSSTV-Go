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
	peakFrequency := spectrumMinHz + float64(peakIndex)*(spectrumMaxHz-spectrumMinHz)/float64(len(spectrum)-1)
	if math.Abs(peakFrequency-1500) > 8 {
		t.Fatalf("detected peak at %.1f Hz, want approximately 1500 Hz", peakFrequency)
	}
	if spectrum[peakIndex] < 0.8 {
		t.Fatalf("1500 Hz frequency-occupancy peak is too weak: %.3f", spectrum[peakIndex])
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

func TestMakeSpectrumUsesMMSSTVStyleAGC(t *testing.T) {
	const sampleRate = 48_000
	makeTone := func(amplitude float64) []float32 {
		samples := make([]float32, spectrumFFTSize)
		for i := range samples {
			samples[i] = float32(amplitude * math.Sin(2*math.Pi*1500*float64(i)/sampleRate))
		}
		return samples
	}
	loud := makeSpectrum(makeTone(0.5), sampleRate)
	quiet := makeSpectrum(makeTone(0.025), sampleRate)
	peak := func(values []float64) float64 {
		maximum := 0.0
		for _, value := range values {
			if value > maximum {
				maximum = value
			}
		}
		return maximum
	}
	if peak(loud) < 0.8 || peak(quiet) < 0.8 {
		t.Fatalf("MMSSTV-style AGC failed to reveal weak or strong tone: loud %.3f, quiet %.3f", peak(loud), peak(quiet))
	}
	if math.Abs(peak(loud)-peak(quiet)) > 0.15 {
		t.Fatalf("AGC should produce similar frequency occupancy for both levels: loud %.3f, quiet %.3f", peak(loud), peak(quiet))
	}
}

func TestEnqueueRXBlockDropsStaleAudioWithDiscontinuity(t *testing.T) {
	queue := make(chan []float32, 2)
	queue <- []float32{1}
	queue <- []float32{2}
	if dropped := enqueueRXBlock(queue, []float32{3}); dropped != 2 {
		t.Fatalf("discarded %d stale blocks, want 2", dropped)
	}
	if marker := <-queue; marker != nil {
		t.Fatalf("first queued item = %v, want discontinuity marker", marker)
	}
	if latest := <-queue; len(latest) != 1 || latest[0] != 3 {
		t.Fatalf("latest queued block = %v, want [3]", latest)
	}
}

func TestEnqueueRXTelemetryPreservesStreamBoundary(t *testing.T) {
	queue := make(chan rxTelemetryBlock, 2)
	queue <- rxTelemetryBlock{Samples: []float32{1}}
	queue <- rxTelemetryBlock{Samples: []float32{2}}
	enqueueRXTelemetry(queue, rxTelemetryBlock{Samples: []float32{3}})
	first := <-queue
	if !first.Discontinuity {
		t.Fatalf("first telemetry item = %+v, want stream boundary", first)
	}
	latest := <-queue
	if latest.Discontinuity || len(latest.Samples) != 1 || latest.Samples[0] != 3 {
		t.Fatalf("latest telemetry item = %+v, want latest sample block [3]", latest)
	}
}

func TestCaptureDumpCircularBufferKeepsNewestSamplesInOrder(t *testing.T) {
	app := &App{}
	app.appendCaptureDump([]float32{0, 1, 2}, 5)
	app.appendCaptureDump([]float32{3, 4, 5}, 5)
	got := app.captureDumpSnapshot()
	want := []float32{1, 2, 3, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("snapshot has %d samples, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot[%d] = %v, want %v (whole: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCaptureDumpCircularBufferHandlesOversizedBlock(t *testing.T) {
	app := &App{}
	app.appendCaptureDump([]float32{0, 1, 2, 3, 4, 5, 6}, 4)
	got := app.captureDumpSnapshot()
	want := []float32{3, 4, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("snapshot has %d samples, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSetRXModePublishesCanonicalRequestWithoutDecoderLock(t *testing.T) {
	app := &App{}
	if err := app.setRXMode(" ROBOT36 "); err != nil {
		t.Fatalf("set RX mode: %v", err)
	}
	requested := app.rxModeRequest.Load()
	if requested == nil || *requested != "robot36" {
		t.Fatalf("published mode = %v, want robot36", requested)
	}
	if err := app.setRXMode("not-a-real-mode"); err == nil {
		t.Fatal("invalid RX mode should be rejected")
	}
	if got := app.rxModeRequest.Load(); got == nil || *got != "robot36" {
		t.Fatalf("invalid request changed active request: %v", got)
	}
	if err := app.setRXMode(""); err != nil {
		t.Fatalf("restore auto RX mode: %v", err)
	}
	if got := app.rxModeRequest.Load(); got == nil || *got != "" {
		t.Fatalf("auto mode request = %v, want empty ID", got)
	}
}
