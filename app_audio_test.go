package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/BH2VSQ/SATSSTV-Go/internal/sstv"
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

func TestProcessRXFSKIDWorkDisarmsAfterIdentity(t *testing.T) {
	const rate = 48_000
	decoder, err := sstv.NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := sstv.AppendMMSSTVFSKID(nil, rate, "BH2VSQ", false)
	if err != nil {
		t.Fatal(err)
	}
	armed := false
	events := processRXFSKIDWork(decoder, &armed, rxFSKIDWork{reset: true, armed: true, samples: wave})
	if len(events) != 1 || events[0].Callsign != "BH2VSQ" || armed {
		t.Fatalf("first FSKID work = %+v, armed=%v; want BH2VSQ and disarmed", events, armed)
	}
	// Regular queued blocks carry the desired listening state for overflow
	// recovery, but must not re-arm after a successful decode unless reset=true.
	events = processRXFSKIDWork(decoder, &armed, rxFSKIDWork{armed: true, samples: wave})
	if len(events) != 0 || armed {
		t.Fatalf("worker re-armed after a successful FSKID: events=%+v armed=%v", events, armed)
	}
	processRXFSKIDWork(decoder, &armed, rxFSKIDWork{reset: true, armed: false})
	if armed {
		t.Fatal("discontinuity reset should disarm FSKID")
	}
}

func TestEnqueueRXFSKIDWorkResetsOnlySideChannelOnOverflow(t *testing.T) {
	queue := make(chan rxFSKIDWork, 2)
	queue <- rxFSKIDWork{armed: true, samples: []int16{1}}
	queue <- rxFSKIDWork{armed: true, samples: []int16{2}}
	enqueueRXFSKIDWork(queue, rxFSKIDWork{armed: true, samples: []int16{3}})
	work := <-queue
	if !work.reset || !work.armed || len(work.samples) != 1 || work.samples[0] != 3 {
		t.Fatalf("overflow recovery work = %+v, want reset+armed with newest sample", work)
	}
	if len(queue) != 0 {
		t.Fatalf("stale FSKID work remains queued: %d", len(queue))
	}
}

func TestMaterializeRXFrameEventDecodesDeferredRaster(t *testing.T) {
	const rate = 48_000
	source := image.NewNRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < source.Bounds().Dy(); y++ {
		for x := 0; x < source.Bounds().Dx(); x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	wave, err := sstv.NewEncoder(rate).Encode(context.Background(), source, sstv.Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := sstv.NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	decoder.SetDeferredFrameDecode(true)
	frames, err := decoder.Feed(context.Background(), wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].Image != nil || frames[0].DecodeRequest == nil {
		t.Fatalf("Feed returned unexpected deferred frame: %+v", frames)
	}
	materialized, err := materializeRXFrameEvent(context.Background(), frames[0])
	if err != nil {
		t.Fatalf("materialize frame event: %v", err)
	}
	if materialized.Image == nil || materialized.Image.Bounds().Dx() != materialized.Mode.Width || materialized.Image.Bounds().Dy() != materialized.Mode.Height {
		t.Fatalf("materialized image bounds = %v, want %dx%d", materialized.Image.Bounds(), materialized.Mode.Width, materialized.Mode.Height)
	}
}
