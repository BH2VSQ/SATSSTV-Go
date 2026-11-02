package sstv

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// shiftFrequency shifts a narrowband real signal by shiftHz using the analytic
// signal (Hilbert FIR) so an end-to-end mistuned-receiver scenario can be
// tested without a radio.
func shiftFrequency(samples []int16, rate int, shiftHz float64) []int16 {
	const n = 15
	hilb := make([]float64, n)
	for i := 0; i < n; i++ {
		m := i - n/2
		if m%2 != 0 {
			hilb[i] = 2.0 / (math.Pi * float64(m))
		}
		hilb[i] *= 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	delay := n / 2
	out := make([]int16, len(samples))
	for i := 0; i < len(samples); i++ {
		var xi float64
		if i >= delay {
			xi = float64(samples[i-delay]) / 32768
		}
		var xq float64
		for k := 0; k < n; k++ {
			if idx := i - k; idx >= 0 {
				xq += float64(samples[idx]) / 32768 * hilb[k]
			}
		}
		phi := 2 * math.Pi * shiftHz * float64(i) / float64(rate)
		re := xi*math.Cos(phi) - xq*math.Sin(phi)
		if re > 1 {
			re = 1
		} else if re < -1 {
			re = -1
		}
		out[i] = int16(math.Round(re * 32767))
	}
	return out
}

func TestDecodeToleratesFrequencyOffset(t *testing.T) {
	const rate = 48_000
	for _, offset := range []float64{-120, -60, 0, 60, 120} {
		t.Run(fmt.Sprintf("offset_%+.0fHz", offset), func(t *testing.T) {
			encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
			if err != nil {
				t.Fatal(err)
			}
			shifted := shiftFrequency(encoded, rate, offset)
			detected, vis, _, err := DetectModeHeader(shifted, rate)
			if err != nil {
				t.Fatalf("detect mode header: %v", err)
			}
			if detected.ID != Robot36ID || vis != 8 {
				t.Fatalf("detected %s / VIS %d, want robot36 / 8", detected.ID, vis)
			}
			decoded, err := Decode(context.Background(), shifted, rate)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Mode.ID != Robot36ID {
				t.Fatalf("auto-detected %s", decoded.Mode.ID)
			}
			want := resizeCropToRGB(makeTestSSTVImage(), 320, 240)
			var total float64
			var count int
			for y := 8; y < 232; y += 8 {
				for x := 8; x < 312; x += 8 {
					got := decoded.Image.NRGBAAt(x, y)
					w := want[y][x]
					total += math.Abs(float64(got.R)-float64(w.R)) + math.Abs(float64(got.G)-float64(w.G)) + math.Abs(float64(got.B)-float64(w.B))
					count += 3
				}
			}
			mae := total / float64(count)
			t.Logf("offset %+.0f Hz MAE=%.1f", offset, mae)
			if mae > 45 {
				t.Errorf("offset %+.0f Hz MAE %.1f exceeds 45", offset, mae)
			}
		})
	}
}

func TestDetectModeHeaderSupportsWideAutoFrequencyOffsets(t *testing.T) {
	const rate = 48_000
	for _, offset := range []float64{-300, -240, 240, 300} {
		t.Run(fmt.Sprintf("offset_%+.0fHz", offset), func(t *testing.T) {
			generator, err := NewToneGenerator(rate, 0.35)
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteVIS(generator, 8); err != nil {
				t.Fatal(err)
			}
			samples := append(generator.Samples(), make([]int16, rate)...)
			shifted := shiftFrequency(samples, rate, offset)
			detected, vis, _, err := DetectModeHeader(shifted, rate)
			if err != nil {
				t.Fatalf("detect mode header at %+.0f Hz: %v", offset, err)
			}
			if detected.ID != Robot36ID || vis != 8 {
				t.Fatalf("detected %s / VIS %d at %+.0f Hz, want robot36 / 8", detected.ID, vis, offset)
			}
		})
	}
}

func TestDetectModeHeaderAFCOffsetsForExtendedAndNarrowVIS(t *testing.T) {
	const rate = 48_000
	for _, modeID := range []string{"mp73", "mp73_n"} {
		spec, err := LookupMode(modeID)
		if err != nil {
			t.Fatal(err)
		}
		for _, offset := range []float64{-240, 240} {
			t.Run(fmt.Sprintf("%s_offset_%+.0fHz", modeID, offset), func(t *testing.T) {
				generator, err := NewToneGenerator(rate, 0.35)
				if err != nil {
					t.Fatal(err)
				}
				switch spec.Header {
				case HeaderMMSSTV:
					err = WriteMMSSTVHeader(generator, spec.ExtendedVIS)
				case HeaderNarrowVIS:
					err = WriteNarrowVIS(generator, spec.NarrowVIS)
				default:
					t.Fatalf("unexpected header family %q", spec.Header)
				}
				if err != nil {
					t.Fatal(err)
				}
				samples := append(generator.Samples(), make([]int16, rate)...)
				shifted := shiftFrequency(samples, rate, offset)
				detected, _, _, err := DetectModeHeader(shifted, rate)
				if err != nil {
					t.Fatalf("detect %s header at %+.0f Hz: %v", modeID, offset, err)
				}
				if detected.ID != modeID {
					t.Fatalf("detected %s at %+.0f Hz, want %s", detected.ID, offset, modeID)
				}
			})
		}
	}
}

func TestFixedModePrefixSupportsWideFrequencyOffsets(t *testing.T) {
	const rate = 48_000
	spec, err := LookupMode(Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, offset := range []float64{-300, 300} {
		t.Run(fmt.Sprintf("offset_%+.0fHz", offset), func(t *testing.T) {
			generator, err := NewToneGenerator(rate, 0.35)
			if err != nil {
				t.Fatal(err)
			}
			for _, segment := range []struct{ hz, ms float64 }{{1900, 300}, {1200, 10}, {1900, 300}} {
				if err := generator.ToneMS(segment.hz, segment.ms); err != nil {
					t.Fatal(err)
				}
			}
			// Fixed-mode synchronization only needs a valid family prefix. Pad
			// the rest of the expected VIS window so the detector can scan it.
			samples := append(generator.Samples(), make([]int16, rate)...)
			shifted := shiftFrequency(samples, rate, offset)
			tracker := newFrequencyTracker(rate)
			freq := tracker.Process(shifted)
			_, _, bodyStart, _, _, err := detectFixedModePrefix(shifted, freq, rate, 0, spec)
			if err != nil {
				t.Fatalf("fixed-mode prefix not detected at %+.0f Hz: %v", offset, err)
			}
			want := int(math.Round(.910 * rate))
			if delta := bodyStart - want; delta < -rate/4_000 || delta > rate/4_000 {
				t.Fatalf("body start %d differs from expected %d by more than one fine-search step", bodyStart, want)
			}
		})
	}
}

func TestSampleClockAFCTracksSlowLineSyncDrift(t *testing.T) {
	const rate = 12_000
	frequency := make([]float64, rate*2)
	for i := range frequency {
		frequency[i] = 1_900
	}
	clock := sampleClock{rate: rate, frequency: frequency, offset: 20, afcEnabled: true, afcMaxOffsetHz: 100}
	const pulseSamples = rate * 9 / 1_000
	const lineSamples = 600
	for line := 0; line < 16; line++ {
		expected := 200 + line*lineSamples
		offset := 20 + float64(line)*1.5
		for i := expected; i < expected+pulseSamples; i++ {
			frequency[i] = HorizontalSync + offset
		}
		clock.pos = expected
		if !clock.syncFirst(9, 2) {
			t.Fatalf("line %d: frequency sync was not found", line)
		}
	}
	want := 20 + 15*1.5
	if delta := math.Abs(clock.offset - want); delta > 5 {
		t.Fatalf("tracked offset %.2f Hz, want %.2f Hz (delta %.2f)", clock.offset, want, delta)
	}
}

func TestSampleClockAFCCanBeDisabled(t *testing.T) {
	const rate = 12_000
	frequency := make([]float64, rate/2)
	for i := range frequency {
		frequency[i] = 1_900
	}
	for i := 0; i < rate*9/1_000; i++ {
		frequency[i] = HorizontalSync + 10
	}
	clock := sampleClock{rate: rate, frequency: frequency, offset: 0, afcEnabled: false, afcMaxOffsetHz: 100}
	if !clock.syncFirst(9, 2) {
		t.Fatal("frequency sync was not found with AFC disabled")
	}
	if clock.offset != 0 {
		t.Fatalf("disabled AFC changed offset to %.2f Hz", clock.offset)
	}
}

func TestStreamLeaderOffsetHonorsAFCWindow(t *testing.T) {
	const rate = 48_000
	leader := make([]float64, int(math.Round(0.300*rate)))
	for i := range leader {
		leader[i] = 2_150
	}
	if got := streamLeaderOffset(leader, rate, 300); math.Abs(got-250) > 0.1 {
		t.Fatalf("±300 Hz AFC window offset = %.1f Hz, want 250 Hz", got)
	}
	if got := streamLeaderOffset(leader, rate, 200); got != 0 {
		t.Fatalf("offset outside ±200 Hz window should be rejected, got %.1f Hz", got)
	}
}

func TestLiveHeaderSearchHonorsConfiguredWideAFCWindow(t *testing.T) {
	const rate = 48_000
	for _, offset := range []float64{-600, 600} {
		t.Run(fmt.Sprintf("offset_%+.0fHz", offset), func(t *testing.T) {
			generator, err := NewToneGenerator(rate, 0.35)
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteVIS(generator, 8); err != nil {
				t.Fatal(err)
			}
			samples := append(generator.Samples(), make([]int16, rate)...)
			shifted := shiftFrequency(samples, rate, offset)
			freq := newStreamFrequencyTracker(rate, true).Process(shifted)
			leaderSamples := int(math.Round(0.300 * rate))
			breakSamples := int(math.Round(0.010 * rate))
			leader := meanFreqQuick(freq, 0, leaderSamples, 6)
			breakMean := meanFreqQuick(freq, leaderSamples, breakSamples, 6)
			breakBest := bestTone(shifted[leaderSamples:leaderSamples+breakSamples], rate, 1_200+offset, 1_900+offset)
			breakRobust := headerTone(shifted[leaderSamples:leaderSamples+breakSamples], rate, 1_200+offset, 1_900+offset)
			t.Logf("leader=%.1f offset=%.1f break=%.1f best=%.1f robust=%.1f", leader, leader-1_900, breakMean, breakBest, breakRobust)
			decoded, after, ok := decodeVISHeaderAtPCM(shifted, rate, 0, leader-1_900)
			t.Logf("direct VIS decode: code=%d after=%d ok=%v", decoded, after, ok)
			detected, vis, _, _, _, err := detectModeHeaderFromRange(shifted, freq, rate, 0, 700)
			if err != nil {
				t.Fatalf("live header search failed at %+.0f Hz: %v", offset, err)
			}
			if detected.ID != Robot36ID || vis != 8 {
				t.Fatalf("detected %s / VIS %d at %+.0f Hz", detected.ID, vis, offset)
			}
		})
	}
}

func TestSampleClockAFCLocksSyncBeyondOldNinetyHertzWindow(t *testing.T) {
	const rate = 12_000
	frequency := make([]float64, rate)
	for i := range frequency {
		frequency[i] = 1_900
	}
	const start = 200
	const pulseSamples = rate * 9 / 1_000
	const lineStep = 500
	for line := 0; line < 14; line++ {
		pulseStart := start + line*lineStep
		for i := pulseStart; i < pulseStart+pulseSamples; i++ {
			frequency[i] = HorizontalSync + 260
		}
	}
	clock := sampleClock{rate: rate, pos: start, frequency: frequency, afcEnabled: true, afcMaxOffsetHz: 300}
	for line := 0; line < 14; line++ {
		clock.pos = start + line*lineStep
		if !clock.syncFirst(9, 2) {
			t.Fatalf("AFC did not acquire 1200+260 Hz sync on line %d", line)
		}
	}
	if math.Abs(clock.offset-260) > 8 {
		t.Fatalf("AFC offset after 14 sync lines = %.1f Hz, want approximately 260 Hz", clock.offset)
	}
}
