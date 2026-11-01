package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"sync/atomic"
	"testing"
	"time"

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
	cycle := rxFSKIDCycle{}
	cycleID := cycle.Start(rate)
	armed := false
	var activeCycleID uint64
	events := processRXFSKIDWork(decoder, &armed, &activeCycleID, rxFSKIDWork{reset: true, armed: true, cycleID: cycleID, samples: wave})
	if len(events) != 1 || events[0].Callsign != "BH2VSQ" || armed || activeCycleID != cycleID {
		t.Fatalf("first FSKID work = %+v, armed=%v activeCycle=%d; want BH2VSQ and disarmed", events, armed, activeCycleID)
	}
	// Regular queued blocks must not re-arm a successfully decoded cycle.
	events = processRXFSKIDWork(decoder, &armed, &activeCycleID, rxFSKIDWork{armed: true, cycleID: cycleID, samples: wave})
	if len(events) != 0 || armed {
		t.Fatalf("worker re-armed after success: events=%+v armed=%v", events, armed)
	}
	processRXFSKIDWork(decoder, &armed, &activeCycleID, rxFSKIDWork{reset: true, armed: false, cycleID: cycleID})
	if armed {
		t.Fatal("explicit disarm should stop FSKID listening")
	}
}

func TestRXFSKIDCycleStartsOncePerPictureAndRejectsLateResults(t *testing.T) {
	const rate = 48_000
	var cycle rxFSKIDCycle
	firstID := cycle.Start(rate)
	if !cycle.Searching() || cycle.Take(rate/2) != rate/2 {
		t.Fatal("first image did not start an active FSKID listen window")
	}
	secondID := cycle.Start(rate)
	if secondID == firstID || !cycle.Searching() || cycle.samplesRemaining != rate*8 {
		t.Fatalf("second picture did not create a fresh FSKID cycle: %+v", cycle)
	}
	if cycle.MarkDecoded(firstID) {
		t.Fatal("late result from previous image disarmed the current image's FSKID window")
	}
	if !cycle.MarkDecoded(secondID) || cycle.Searching() || cycle.state != rxFSKIDDecoded {
		t.Fatalf("current cycle result did not complete its own window: %+v", cycle)
	}
}

func TestProcessRXFSKIDWorkIgnoresStalePreviousPictureBatches(t *testing.T) {
	const rate = 48_000
	decoder, err := sstv.NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := sstv.AppendMMSSTVFSKID(nil, rate, "BH2VSQ", false)
	if err != nil {
		t.Fatal(err)
	}
	var cycle rxFSKIDCycle
	firstID := cycle.Start(rate)
	secondID := cycle.Start(rate)
	armed := false
	var activeID uint64
	processRXFSKIDWork(decoder, &armed, &activeID, rxFSKIDWork{reset: true, armed: true, cycleID: secondID})
	// A delayed sample batch from the first picture must not be appended to the
	// newly reset decoder or emit a stale identity while the second image listens.
	if events := processRXFSKIDWork(decoder, &armed, &activeID, rxFSKIDWork{armed: true, cycleID: firstID, samples: wave}); len(events) != 0 {
		t.Fatalf("stale previous-picture batch emitted identity: %+v", events)
	}
	if events := processRXFSKIDWork(decoder, &armed, &activeID, rxFSKIDWork{armed: true, cycleID: secondID, samples: wave}); len(events) != 1 || events[0].Callsign != "BH2VSQ" {
		t.Fatalf("current picture FSKID = %+v, want BH2VSQ", events)
	}
}

func TestRXFSKIDCycleCanDecodeOneIDForEachConsecutivePicture(t *testing.T) {
	const rate = 48_000
	decoder, err := sstv.NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := sstv.AppendMMSSTVFSKID(nil, rate, "BH2VSQ", false)
	if err != nil {
		t.Fatal(err)
	}
	var cycle rxFSKIDCycle
	armed := false
	var activeID uint64
	for picture := 0; picture < 3; picture++ {
		cycleID := cycle.Start(rate)
		events := processRXFSKIDWork(decoder, &armed, &activeID, rxFSKIDWork{reset: true, armed: true, cycleID: cycleID, samples: wave})
		if len(events) != 1 || events[0].Callsign != "BH2VSQ" {
			t.Fatalf("picture %d FSKID events = %+v, want BH2VSQ", picture+1, events)
		}
		if !cycle.MarkDecoded(cycleID) {
			t.Fatalf("picture %d FSKID cycle did not complete", picture+1)
		}
		armed = false
	}
}

func TestRXFSKIDCycleExpiresByCapturedSamplesAndKeepsWindowAcrossGap(t *testing.T) {
	const rate = 48_000
	var cycle rxFSKIDCycle
	id := cycle.Start(rate)
	if !cycle.ResetAfterCaptureGap() {
		t.Fatal("capture gap should preserve the active picture's FSKID listener")
	}
	if taken := cycle.Take(rate * 3); taken != rate*3 || cycle.samplesRemaining != rate*5 {
		t.Fatalf("sample budget after gap = taken:%d remaining:%d", taken, cycle.samplesRemaining)
	}
	if taken := cycle.Take(rate * 10); taken != rate*5 || cycle.state != rxFSKIDDraining {
		t.Fatalf("FSKID window did not enter draining at eight seconds: taken=%d cycle=%+v", taken, cycle)
	}
	if !cycle.MarkDecoded(id) || cycle.state != rxFSKIDDecoded {
		t.Fatal("cycle must still accept a valid result while the worker drains queued PCM")
	}
	if cycle.MarkFinished(id) {
		t.Fatal("a decoded cycle must not transition to expired")
	}

	var second rxFSKIDCycle
	secondID := second.Start(rate)
	second.Take(rate * 8)
	if second.state != rxFSKIDDraining || !second.MarkFinished(secondID) || second.state != rxFSKIDExpired {
		t.Fatalf("ordered finish acknowledgement did not expire the cycle: %+v", second)
	}
	if second.MarkDecoded(secondID) {
		t.Fatal("expired cycle accepted a late decoded result")
	}
}

func TestEnqueueRXFSKIDWorkResetsOnlySideChannelOnOverflow(t *testing.T) {
	queue := make(chan rxFSKIDWork, 2)
	queue <- rxFSKIDWork{armed: true, cycleID: 2, samples: []int16{1}}
	queue <- rxFSKIDWork{armed: true, cycleID: 2, samples: []int16{2}}
	enqueueRXFSKIDWork(queue, rxFSKIDWork{armed: true, cycleID: 9, samples: []int16{3}})
	work := <-queue
	if !work.reset || !work.armed || work.cycleID != 9 || len(work.samples) != 1 || work.samples[0] != 3 {
		t.Fatalf("overflow recovery work = %+v, want reset+armed with newest sample", work)
	}
	if len(queue) != 0 {
		t.Fatalf("stale FSKID work remains queued: %d", len(queue))
	}
}

func TestRXCaptureDiscontinuityKeepsPostPictureFSKIDWindowArmed(t *testing.T) {
	queue := make(chan rxFSKIDWork, 4)
	batch := []int16{1, 2, 3, 4}
	var cycle rxFSKIDCycle
	cycleID := cycle.Start(48_000)
	cycle.Take(48_000 * 3)
	decoded := &atomic.Uint64{}
	resetRXFSKIDAfterCaptureDiscontinuity(queue, &cycle, &batch, decoded)
	if !cycle.Searching() || cycle.samplesRemaining != 48_000*5 {
		t.Fatal("capture discontinuity disarmed or reset the per-picture listening window")
	}
	if len(batch) != 0 {
		t.Fatalf("partial pre-gap batch was not discarded: %v", batch)
	}
	work := <-queue
	if !work.reset || !work.armed || work.cycleID != cycleID || len(work.samples) != 0 {
		t.Fatalf("FSKID discontinuity work = %+v, want reset+armed for same cycle", work)
	}
}

func TestRXCaptureDiscontinuityDoesNotRearmCompletedFSKID(t *testing.T) {
	queue := make(chan rxFSKIDWork, 4)
	batch := []int16{1, 2, 3, 4}
	var cycle rxFSKIDCycle
	cycleID := cycle.Start(48_000)
	decoded := &atomic.Uint64{}
	decoded.Store(cycleID)
	resetRXFSKIDAfterCaptureDiscontinuity(queue, &cycle, &batch, decoded)
	if cycle.Searching() || cycle.state != rxFSKIDDecoded {
		t.Fatalf("completed FSKID cycle was rearmed: %+v", cycle)
	}
	work := <-queue
	if !work.reset || work.armed || work.cycleID != cycleID {
		t.Fatalf("completed FSKID discontinuity work = %+v, want reset+disarmed", work)
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

func TestAppendRXFSKIDBatchPreservesOrderedPCMAndFlushesTail(t *testing.T) {
	queue := make(chan rxFSKIDWork, 8)
	batch := make([]int16, 0, 8)
	appendRXFSKIDCycleBatch(queue, &batch, []int16{1, 2, 3}, 4, 7)
	if len(queue) != 0 || len(batch) != 3 {
		t.Fatalf("after first partial batch: queued=%d pending=%v", len(queue), batch)
	}
	appendRXFSKIDCycleBatch(queue, &batch, []int16{4, 5, 6, 7, 8, 9}, 4, 7)
	if len(queue) != 2 || len(batch) != 1 || batch[0] != 9 {
		t.Fatalf("after complete batches: queued=%d pending=%v", len(queue), batch)
	}
	flushRXFSKIDCycleBatch(queue, &batch, 7)
	if len(queue) != 3 || len(batch) != 0 {
		t.Fatalf("after tail flush: queued=%d pending=%v", len(queue), batch)
	}
	var got []int16
	for len(queue) > 0 {
		work := <-queue
		if work.reset || !work.armed || work.cycleID != 7 {
			t.Fatalf("unexpected control work in sample batches: %+v", work)
		}
		got = append(got, work.samples...)
	}
	want := []int16{1, 2, 3, 4, 5, 6, 7, 8, 9}
	if len(got) != len(want) {
		t.Fatalf("batched PCM = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("batched PCM[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRXFSKIDWorkerDrainsQueuedAudioBeforeFinishAndIgnoresStaleFinish(t *testing.T) {
	const rate = 48_000
	decoder, err := sstv.NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := sstv.AppendMMSSTVFSKID(nil, rate, "BH2VSQ", false)
	if err != nil {
		t.Fatal(err)
	}
	queue := make(chan rxFSKIDWork, 16)
	var decodedCycle, finishedCycle atomic.Uint64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := &App{}
	go app.runRXFSKIDWorker(ctx, queue, decoder, &decodedCycle, &finishedCycle)

	const activeID = uint64(22)
	queue <- rxFSKIDWork{reset: true, armed: true, cycleID: activeID}
	// Split the reference packet into irregular batches to model native audio
	// callbacks, then queue an old finish marker and the active cycle's marker.
	for offset, chunk := 0, 997; offset < len(wave); chunk = chunk%1703 + 311 {
		end := min(offset+chunk, len(wave))
		queue <- rxFSKIDWork{armed: true, cycleID: activeID, samples: append([]int16(nil), wave[offset:end]...)}
		offset = end
	}
	queue <- rxFSKIDWork{finish: true, cycleID: activeID - 1}
	queue <- rxFSKIDWork{finish: true, cycleID: activeID}

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for finishedCycle.Load() != activeID {
		select {
		case <-deadline.C:
			t.Fatalf("worker did not acknowledge finish after draining queued PCM; decoded=%d finished=%d", decodedCycle.Load(), finishedCycle.Load())
		case <-ticker.C:
		}
	}
	if got := decodedCycle.Load(); got != activeID {
		t.Fatalf("worker acknowledged finish before decoding queued FSKID: decoded cycle=%d, want %d", got, activeID)
	}
}
