package sstv

import (
	"math"
	"testing"
)

func TestSampleClockSyncFindsRaw1200HzPulse(t *testing.T) {
	const rate = 48_000
	generator, err := NewToneGenerator(rate, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(1_800, 20); err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(HorizontalSync, 9); err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(1_800, 15); err != nil {
		t.Fatal(err)
	}

	pcm := generator.Samples()
	actualStart := 20 * rate / 1000
	pulseSamples := 9 * rate / 1000
	clock := sampleClock{
		rate:     rate,
		pos:      actualStart + 3*rate/1000,
		samples:  pcm,
		syncSeen: true,
	}
	if !clock.sync(9, 8) {
		t.Fatal("raw 1200 Hz line-sync pulse was not detected")
	}
	maxError := rate / 3_000 // about 0.33 ms
	if delta := absSampleDelta(clock.pos - (actualStart + pulseSamples)); delta > maxError {
		t.Fatalf("sync ended at sample %d, want %d (error %d samples; tolerance %d)",
			clock.pos, actualStart+pulseSamples, delta, maxError)
	}
	if clock.fractional != 0 {
		t.Fatalf("sync did not re-anchor fractional sample carry: %f", clock.fractional)
	}
}

func TestSampleClockSyncFirstFindsScottieMidLinePulse(t *testing.T) {
	const rate = 48_000
	const syncMS = 4.862
	generator, err := NewToneGenerator(rate, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(1_800, 20); err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(HorizontalSync, syncMS); err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(1_800, 15); err != nil {
		t.Fatal(err)
	}

	pcm := generator.Samples()
	actualStart := 20 * rate / 1000
	pulseSamples := int(math.Round(syncMS / 1000 * rate))
	clock := sampleClock{
		rate:    rate,
		pos:     actualStart + 3*rate/1000,
		samples: pcm,
	}
	if !clock.syncFirst(syncMS, 8) {
		t.Fatal("first Scottie mid-line 1200 Hz sync pulse was not detected")
	}
	maxError := rate / 3_000
	if delta := absSampleDelta(clock.pos - (actualStart + pulseSamples)); delta > maxError {
		t.Fatalf("Scottie sync ended at sample %d, want %d (error %d samples; tolerance %d)",
			clock.pos, actualStart+pulseSamples, delta, maxError)
	}
}

func TestSampleClockKeepsVISAdjacentFirstSyncAnchor(t *testing.T) {
	const rate = 48_000
	generator, err := NewToneGenerator(rate, 0.55)
	if err != nil {
		t.Fatal(err)
	}
	// VIS stop tone and first line sync are both 1200 Hz and contiguous.
	if err := generator.ToneMS(HorizontalSync, 39); err != nil {
		t.Fatal(err)
	}
	if err := generator.ToneMS(1_800, 15); err != nil {
		t.Fatal(err)
	}

	bodyStart := 30 * rate / 1000
	clock := sampleClock{rate: rate, pos: bodyStart, samples: generator.Samples()}
	if clock.sync(9, 12) {
		t.Fatal("the VIS-adjacent first sync should use the header-derived body offset")
	}
	if want := bodyStart + 9*rate/1000; clock.pos != want {
		t.Fatalf("first sync end = %d, want %d", clock.pos, want)
	}
}

func absSampleDelta(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
