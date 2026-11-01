package sstv

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
)

func TestAppendMMSSTVFSKIDUsesDocumentedBitAndSymbolOrder(t *testing.T) {
	rate := 48_000
	for _, test := range []struct {
		name       string
		initialHz  float64
		narrowBand bool
	}{
		{name: "normal", initialHz: 1500},
		{name: "narrow", initialHz: 1900, narrowBand: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			wave, err := AppendMMSSTVFSKID(nil, rate, "N", test.narrowBand)
			if err != nil {
				t.Fatal(err)
			}
			firstToneN := rate * 300 / 1000
			secondToneN := rate * 100 / 1000
			bitN := int(float64(rate) * fskIDBitMS / 1000)
			if got := bestTone(wave[:firstToneN], rate, 1500, 1900); got != test.initialHz {
				t.Fatalf("initial FSKID tone = %.0f Hz, want %.0f Hz", got, test.initialHz)
			}
			if got := bestTone(wave[firstToneN:firstToneN+secondToneN], rate, 1900, 2100); got != 2100 {
				t.Fatalf("second FSKID tone = %.0f Hz, want 2100 Hz", got)
			}
			p := firstToneN + secondToneN
			if got := bestTone(wave[p:p+bitN], rate, 1900, 2100); got != 1900 {
				t.Fatalf("start bit = %.0f Hz, want 1900 Hz", got)
			}
			p += bitN
			// MMSSTV's actual WriteFSK() emits B0 first. The marker 0x2A
			// is bit-palindromic, so the callsign character is what protects
			// this test from accidentally accepting MSB-first output.
			for _, symbol := range []uint8{0x2A, uint8('N' - 0x20), 0x01, uint8('N' - 0x20)} {
				for bit := 0; bit < 6; bit++ {
					want := 2100.0
					if symbol&(1<<bit) != 0 {
						want = 1900
					}
					if p+bitN > len(wave) {
						t.Fatal("FSKID waveform ended before the expected symbol stream")
					}
					if got := bestTone(wave[p:p+bitN], rate, 1900, 2100); got != want {
						t.Fatalf("symbol 0x%02x bit B%d tone = %.0f Hz, want %.0f Hz", symbol, bit, got, want)
					}
					p += bitN
				}
			}
		})
	}
}

func TestFSKIDDecoderHandlesArbitraryCaptureChunkBoundaries(t *testing.T) {
	for _, rate := range []int{8_000, 12_000, 44_100, 48_000, 96_000} {
		for _, narrow := range []bool{false, true} {
			name := "normal"
			if narrow {
				name = "narrow"
			}
			t.Run(fmt.Sprintf("%d/%s", rate, name), func(t *testing.T) {
				input := make([]int16, 713) // Deliberately offset the FSKID start from a millisecond boundary.
				for i := range input {
					input[i] = int16((i%31 - 15) * 3)
				}
				wave, err := AppendMMSSTVFSKID(nil, rate, "BH2VSQ", narrow)
				if err != nil {
					t.Fatal(err)
				}
				input = append(input, wave...)
				decoder, err := NewFSKIDDecoder(rate)
				if err != nil {
					t.Fatal(err)
				}
				var calls []string
				for offset := 0; offset < len(input); {
					end := min(len(input), offset+1379)
					for _, event := range decoder.Feed(input[offset:end]) {
						calls = append(calls, event.Callsign)
					}
					offset = end
				}
				if len(calls) != 1 || calls[0] != "BH2VSQ" {
					t.Fatalf("decoded callsigns = %q, want exactly [BH2VSQ]", calls)
				}
			})
		}
	}
}

func TestAppendMMSSTVFSKIDRejectsUnsupportedCallsAndSkipsEmpty(t *testing.T) {
	base := []int16{1, 2, 3}
	got, err := AppendMMSSTVFSKID(base, 48_000, "  ", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(base) || got[0] != base[0] {
		t.Fatalf("empty callsign should leave audio unchanged: %v", got)
	}
	if _, err := AppendMMSSTVFSKID(nil, 48_000, "BAD!\u4e2d", false); err == nil || !strings.Contains(err.Error(), "unsupported character") {
		t.Fatalf("expected unsupported character error, got %v", err)
	}
}

func TestFSKIDMalformedCandidateDoesNotBlockLaterValidCallsign(t *testing.T) {
	rate := 12_000
	valid, err := AppendMMSSTVFSKID(nil, rate, "BH2VSQ", false)
	if err != nil {
		t.Fatal(err)
	}
	bitN := int(float64(rate) * fskIDBitMS / 1000)
	markerEnd := rate*300/1000 + rate*100/1000 + bitN + 6*bitN
	malformed := append([]int16(nil), valid[:markerEnd]...)
	malformed = append(malformed, make([]int16, 6*bitN)...)
	input := append(malformed, valid...)
	decoder, err := NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for offset := 0; offset < len(input); {
		end := min(len(input), offset+997)
		for _, event := range decoder.Feed(input[offset:end]) {
			got = append(got, event.Callsign)
		}
		offset = end
	}
	if len(got) != 1 || got[0] != "BH2VSQ" {
		t.Fatalf("valid FSKID after malformed candidate was not recovered: %v", got)
	}
}

func TestFSKIDDecoderResetClearsPartialCandidate(t *testing.T) {
	const rate = 8_000
	wave, err := AppendMMSSTVFSKID(nil, rate, "BH2VSQ", false)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	if events := decoder.Feed(wave[:rate/2]); len(events) != 0 {
		t.Fatalf("partial FSKID unexpectedly produced events: %+v", events)
	}
	if len(decoder.pending) == 0 {
		t.Fatal("partial FSKID candidate was not buffered")
	}
	decoder.Reset()
	if len(decoder.pending) != 0 || decoder.scanFrom != 0 || len(decoder.waiting) != 0 {
		t.Fatalf("reset left stale FSKID state: pending=%d scanFrom=%d waiting=%d", len(decoder.pending), decoder.scanFrom, len(decoder.waiting))
	}
	if events := decoder.Feed(wave); len(events) != 1 || events[0].Callsign != "BH2VSQ" {
		t.Fatalf("FSKID after reset = %+v, want BH2VSQ", events)
	}
}

// makeMMSSTVReferenceFSKID follows the original C++ implementation literally:
// OutputFSKID writes 2100 Hz for 100 ms, a 1900 Hz start bit, then WriteFSK
// emits c&1 and shifts c right for six LSB-first bits per symbol.
func makeMMSSTVReferenceFSKID(t *testing.T, rate int, callsign string, narrow bool) []int16 {
	t.Helper()
	return makeMMSSTVReferenceFSKIDWithOffset(t, rate, callsign, narrow, 0)
}

func makeMMSSTVReferenceFSKIDWithOffset(t *testing.T, rate int, callsign string, narrow bool, offsetHz float64) []int16 {
	t.Helper()
	return makeMMSSTVReferenceFSKIDWithPhase(t, rate, callsign, narrow, offsetHz, 0)
}

func makeMMSSTVReferenceFSKIDWithPhase(t *testing.T, rate int, callsign string, narrow bool, offsetHz, initialPhase float64) []int16 {
	t.Helper()
	var samples []int16
	var exactPosition float64
	written := 0
	phase := initialPhase
	appendTone := func(frequency, durationMS float64) {
		exactPosition += durationMS * float64(rate) / 1000
		end := int(exactPosition) // C++ int(m_dPos): cumulative floor.
		count := end - written
		start := len(samples)
		samples = append(samples, make([]int16, count)...)
		step := 2 * math.Pi * (frequency + offsetHz) / float64(rate)
		for i := start; i < len(samples); i++ {
			samples[i] = int16(math.Round(math.Sin(phase) * DefaultLevel * 32767))
			phase += step
			if phase >= 2*math.Pi || phase <= -2*math.Pi {
				phase = math.Mod(phase, 2*math.Pi)
			}
		}
		written = end
	}
	leader := 1500.0
	if narrow {
		leader = 1900
	}
	appendTone(leader, 300)
	appendTone(2100, 100)
	appendTone(1900, 22)
	symbols := []uint8{0x2A}
	var checksum uint8
	for _, r := range strings.ToUpper(callsign) {
		v := uint8(r - 0x20)
		symbols = append(symbols, v)
		checksum ^= v
	}
	symbols = append(symbols, 0x01, checksum)
	for _, symbol := range symbols {
		for bit := 0; bit < 6; bit++ {
			hz := 2100.0
			if symbol&(1<<bit) != 0 {
				hz = 1900
			}
			appendTone(hz, 22)
		}
	}
	return samples
}

func TestAppendMMSSTVFSKIDMatchesCumulativeCXXSampleClock(t *testing.T) {
	for _, rate := range []int{8_000, 12_000, 44_100, 48_000, 96_000} {
		for _, narrow := range []bool{false, true} {
			name := "normal"
			if narrow {
				name = "narrow"
			}
			t.Run(fmt.Sprintf("%d/%s", rate, name), func(t *testing.T) {
				prefix := []int16{17, -23, 31, -47}
				got, err := AppendMMSSTVFSKID(prefix, rate, "BH2VSQ", narrow)
				if err != nil {
					t.Fatal(err)
				}
				want := append(append([]int16(nil), prefix...), makeMMSSTVReferenceFSKID(t, rate, "BH2VSQ", narrow)...)
				if len(got) != len(want) {
					t.Fatalf("MMSSTV waveform length = %d, want %d", len(got), len(want))
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("MMSSTV waveform differs at sample %d: got %d, want %d", i, got[i], want[i])
					}
				}
			})
		}
	}
}

func TestFSKIDDecoderReadsMMSSTVReferenceLSBFirst(t *testing.T) {
	for _, rate := range []int{12_000, 44_100, 48_000} {
		for _, narrow := range []bool{false, true} {
			name := "normal"
			if narrow {
				name = "narrow"
			}
			t.Run(fmt.Sprintf("%d/%s", rate, name), func(t *testing.T) {
				input := append(make([]int16, 713), makeMMSSTVReferenceFSKID(t, rate, "BH2VSQ", narrow)...)
				decoder, err := NewFSKIDDecoder(rate)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for offset := 0; offset < len(input); {
					end := min(len(input), offset+1379)
					for _, event := range decoder.Feed(input[offset:end]) {
						got = append(got, event.Callsign)
					}
					offset = end
				}
				if len(got) != 1 || got[0] != "BH2VSQ" {
					t.Fatalf("MMSSTV reference FSKID decoded %q, want [BH2VSQ]", got)
				}
			})
		}
	}
}

func TestFSKIDDecoderToleratesReceiverFrequencyOffset(t *testing.T) {
	for _, offsetHz := range []float64{-80, -50, 50, 80} {
		for _, narrow := range []bool{false, true} {
			name := "normal"
			if narrow {
				name = "narrow"
			}
			t.Run(fmt.Sprintf("offset_%+.0fHz/%s", offsetHz, name), func(t *testing.T) {
				const rate = 48_000
				input := append(make([]int16, 713), makeMMSSTVReferenceFSKIDWithOffset(t, rate, "BH2VSQ", narrow, offsetHz)...)
				decoder, err := NewFSKIDDecoder(rate)
				if err != nil {
					t.Fatal(err)
				}
				var events []FSKIDEvent
				for position := 0; position < len(input); {
					end := min(len(input), position+1024)
					events = append(events, decoder.Feed(input[position:end])...)
					position = end
				}
				if len(events) != 1 || events[0].Callsign != "BH2VSQ" {
					t.Fatalf("FSKID at %+0.f Hz offset (narrow=%v) decoded %+v, want BH2VSQ", offsetHz, narrow, events)
				}
			})
		}
	}
}

func TestFSKIDDecoderToleratesReceiverNoiseAndReducedLevel(t *testing.T) {
	const rate = 48_000
	reference := makeMMSSTVReferenceFSKID(t, rate, "BH2VSQ", false)
	rng := rand.New(rand.NewSource(0x5A7))
	input := make([]int16, 337)
	for _, sample := range reference {
		// Model a reduced receiver audio level plus deterministic broadband noise
		// without changing the MMSSTV symbol clock or tone frequencies.
		value := float64(sample)*0.38 + (rng.Float64()*2-1)*420
		if value > math.MaxInt16 {
			value = math.MaxInt16
		} else if value < math.MinInt16 {
			value = math.MinInt16
		}
		input = append(input, int16(math.Round(value)))
	}
	decoder, err := NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	var callsigns []string
	for offset := 0; offset < len(input); {
		end := min(len(input), offset+683)
		for _, event := range decoder.Feed(input[offset:end]) {
			callsigns = append(callsigns, event.Callsign)
		}
		offset = end
	}
	if len(callsigns) != 1 || callsigns[0] != "BH2VSQ" {
		t.Fatalf("noisy/reduced-level MMSSTV FSKID decoded %q, want [BH2VSQ]", callsigns)
	}
}

func TestFSKIDDecoderAcquiresAtMMSSTV2100HzGuardWithoutLeader(t *testing.T) {
	for _, rate := range []int{8_000, 12_000, 44_100, 48_000, 96_000} {
		t.Run(fmt.Sprintf("%d", rate), func(t *testing.T) {
			reference := makeMMSSTVReferenceFSKID(t, rate, "BH2VSQ", false)
			leaderSamples := int(math.Round(.300 * float64(rate)))
			if len(reference) <= leaderSamples {
				t.Fatal("reference waveform is shorter than its leader")
			}
			// Match MMSSTV DecodeFSK(), which acquires from the 2100 Hz guard.
			// This also covers captures whose first 300 ms leader was consumed by
			// the preceding SSTV frame boundary or missed when RX was armed.
			input := append(make([]int16, 317), reference[leaderSamples:]...)
			decoder, err := NewFSKIDDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for offset := 0; offset < len(input); {
				end := min(len(input), offset+733)
				for _, event := range decoder.Feed(input[offset:end]) {
					got = append(got, event.Callsign)
				}
				offset = end
			}
			if len(got) != 1 || got[0] != "BH2VSQ" {
				t.Fatalf("guard-acquired FSKID decoded %q, want [BH2VSQ]", got)
			}
		})
	}
}

func TestFSKIDDecoderTrimsMMSSTVCallsignPadding(t *testing.T) {
	const rate = 48_000
	input := makeMMSSTVReferenceFSKID(t, rate, "  BH2VSQ  ", false)
	decoder, err := NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	var events []FSKIDEvent
	for offset := 0; offset < len(input); {
		end := min(len(input), offset+2048)
		events = append(events, decoder.Feed(input[offset:end])...)
		offset = end
	}
	if len(events) != 1 || events[0].Callsign != "BH2VSQ" {
		t.Fatalf("padded MMSSTV callsign decoded as %+v, want BH2VSQ", events)
	}
}

func TestFSKIDAfterCompletedSSTVFrameUsesOnlyPostFrameTail(t *testing.T) {
	const rate = 48_000
	picture, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	id := makeMMSSTVReferenceFSKID(t, rate, "BH2VSQ", false)
	stream := append(append([]int16(nil), picture...), id...)

	for _, modeID := range []string{"", Robot36ID} {
		name := modeID
		if name == "" {
			name = "auto"
		}
		t.Run(name, func(t *testing.T) {
			streamDecoder, err := NewStreamDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			if err := streamDecoder.SetMode(modeID); err != nil {
				t.Fatal(err)
			}
			// Exercise the same deferred-final-decode path used by the Wails RX
			// worker. Frame completion must expose the exact post-raster PCM tail.
			streamDecoder.SetDeferredFrameDecode(true)
			fskidDecoder, err := NewFSKIDDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			armed := false
			var frameCount int
			var callsigns []string
			for offset := 0; offset < len(stream); {
				end := min(len(stream), offset+1024)
				block := stream[offset:end]
				frames, feedErr := streamDecoder.Feed(context.Background(), block)
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				frameCount += len(frames)
				if len(frames) > 0 {
					if frames[0].Image != nil || frames[0].DecodeRequest == nil {
						t.Fatalf("deferred stream event = %+v, want decode request", frames[0])
					}
					// The frame-completing block may already contain the first
					// samples of FSKID. Seed from only the true unconsumed tail.
					for _, event := range fskidDecoder.Feed(streamDecoder.PendingAudio()) {
						callsigns = append(callsigns, event.Callsign)
					}
					armed = true
				} else if armed {
					if _, active := streamDecoder.ActiveMode(); active {
						t.Fatal("FSKID trailer falsely activated another SSTV frame")
					}
					for _, event := range fskidDecoder.Feed(block) {
						callsigns = append(callsigns, event.Callsign)
					}
				}
				offset = end
			}
			if frameCount != 1 {
				t.Fatalf("decoded SSTV frames = %d, want 1", frameCount)
			}
			if len(callsigns) != 1 || callsigns[0] != "BH2VSQ" {
				t.Fatalf("post-frame MMSSTV FSKID callsigns = %q, want [BH2VSQ]", callsigns)
			}
		})
	}
}

// makeMMSSTVClockOffsetFSKID emulates MMSSTV's TxSampOffset behavior: the
// output sample count (and therefore all tone durations) is scaled, while the
// VCO continues to generate the nominal 1900/2100 Hz audio frequencies.
func makeMMSSTVClockOffsetFSKID(t *testing.T, rate int, callsign string, clockScale float64) []int16 {
	t.Helper()
	if clockScale <= 0 {
		t.Fatal("clock scale must be positive")
	}
	var samples []int16
	var exactPosition float64
	written := 0
	phase := 0.0
	appendTone := func(frequency, durationMS float64) {
		exactPosition += durationMS * float64(rate) / 1000 * clockScale
		end := int(exactPosition)
		count := end - written
		start := len(samples)
		samples = append(samples, make([]int16, count)...)
		step := 2 * math.Pi * frequency / float64(rate)
		for i := start; i < len(samples); i++ {
			samples[i] = int16(math.Round(math.Sin(phase) * DefaultLevel * 32767))
			phase += step
			if phase >= 2*math.Pi || phase <= -2*math.Pi {
				phase = math.Mod(phase, 2*math.Pi)
			}
		}
		written = end
	}
	appendTone(1500, 300)
	appendTone(2100, 100)
	appendTone(1900, 22)
	symbols := []uint8{0x2A}
	var checksum uint8
	for _, r := range strings.ToUpper(callsign) {
		v := uint8(r - 0x20)
		symbols = append(symbols, v)
		checksum ^= v
	}
	symbols = append(symbols, 0x01, checksum)
	for _, symbol := range symbols {
		for bit := 0; bit < 6; bit++ {
			frequency := 2100.0
			if symbol&(1<<bit) != 0 {
				frequency = 1900
			}
			appendTone(frequency, 22)
		}
	}
	// A live sound-card stream continues after FSKID ends; provide a short
	// trailing interval so all symbol-phase candidates can finish their window.
	samples = append(samples, make([]int16, rate/10)...)
	return samples
}

func TestFSKIDDecoderTracksMMSSTVTxSampleClockOffset(t *testing.T) {
	const rate = 48_000
	for _, scale := range []float64{0.985, 0.990, 0.995, 1.005, 1.010, 1.015} {
		t.Run(fmt.Sprintf("clock-%.3f", scale), func(t *testing.T) {
			input := makeMMSSTVClockOffsetFSKID(t, rate, "BH2VSQ", scale)
			decoder, err := NewFSKIDDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			var callsigns []string
			for offset := 0; offset < len(input); {
				end := min(len(input), offset+733)
				for _, event := range decoder.Feed(input[offset:end]) {
					callsigns = append(callsigns, event.Callsign)
				}
				offset = end
			}
			if len(callsigns) != 1 || callsigns[0] != "BH2VSQ" {
				t.Fatalf("clock-scaled MMSSTV FSKID decoded %q, want [BH2VSQ]; pending=%d scanFrom=%d waiting=%+v", callsigns, len(decoder.pending), decoder.scanFrom, decoder.waiting)
			}
		})
	}
}

func TestFSKIDDecoderKeepsLongCallsignAlignedWithMMSSTVTxSampleClockOffset(t *testing.T) {
	const rate = 48_000
	const callsign = "BH2VSQ1234567890" // MMSSTV supports up to 16 six-bit characters.
	for _, scale := range []float64{0.985, 1.015} {
		t.Run(fmt.Sprintf("clock-%.3f", scale), func(t *testing.T) {
			input := makeMMSSTVClockOffsetFSKID(t, rate, callsign, scale)
			decoder, err := NewFSKIDDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			var callsigns []string
			for offset := 0; offset < len(input); {
				end := min(len(input), offset+733)
				for _, event := range decoder.Feed(input[offset:end]) {
					callsigns = append(callsigns, event.Callsign)
				}
				offset = end
			}
			if len(callsigns) != 1 || callsigns[0] != callsign {
				t.Fatalf("long clock-scaled MMSSTV FSKID decoded %q, want [%s]", callsigns, callsign)
			}
		})
	}
}

func TestFSKIDDecoderStressVariableCaptureConditions(t *testing.T) {
	const rate = 48_000
	const callsign = "BH2VSQ"
	for trial := int64(0); trial < 120; trial++ {
		rng := rand.New(rand.NewSource(0xF5_0000 + trial))
		narrow := trial%2 == 1
		offsetHz := []float64{-80, -55, -25, 0, 25, 55, 80}[trial%7]
		base := makeMMSSTVReferenceFSKIDWithOffset(t, rate, callsign, narrow, offsetHz)
		prefixLen := rng.Intn(rate / 2)
		input := make([]int16, prefixLen)
		for i := range input {
			input[i] = int16(rng.Intn(201) - 100)
		}
		level := []float64{0.04, 0.08, 0.15, 0.30, 0.55, 0.90}[trial%6]
		noise := []float64{0, 40, 100, 180, 300, 500}[trial%6]
		for _, sample := range base {
			v := float64(sample)*level + (rng.Float64()*2-1)*noise
			if v > math.MaxInt16 {
				v = math.MaxInt16
			}
			if v < math.MinInt16 {
				v = math.MinInt16
			}
			input = append(input, int16(math.Round(v)))
		}
		decoder, err := NewFSKIDDecoder(rate)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for position := 0; position < len(input); {
			chunk := 1 + rng.Intn(4096)
			end := min(len(input), position+chunk)
			for _, event := range decoder.Feed(input[position:end]) {
				got = append(got, event.Callsign)
			}
			position = end
		}
		if len(got) != 1 || got[0] != callsign {
			t.Errorf("trial=%d narrow=%v offset=%+.0fHz prefix=%d level=%.2f noise=%.0f chunks random: decoded %q; pending=%d scan=%d waiting=%d", trial, narrow, offsetHz, prefixLen, level, noise, got, len(decoder.pending), decoder.scanFrom, len(decoder.waiting))
		}
	}
}

func TestFSKIDDecoderRandomCarrierInitialPhase(t *testing.T) {
	const rate = 48_000
	const callsign = "BH2VSQ"
	for trial := 0; trial < 96; trial++ {
		phase := 2 * math.Pi * float64(trial) / 96
		narrow := trial%2 == 1
		input := makeMMSSTVReferenceFSKIDWithPhase(t, rate, callsign, narrow, 0, phase)
		// Random capture start and arbitrary block sizes emulate a live device
		// callback that does not align to the transmitter's oscillator phase.
		rng := rand.New(rand.NewSource(int64(0xCA771E + trial)))
		prefix := make([]int16, rng.Intn(rate/20))
		stream := append(prefix, input...)
		decoder, err := NewFSKIDDecoder(rate)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for pos := 0; pos < len(stream); {
			end := min(len(stream), pos+1+rng.Intn(2300))
			for _, event := range decoder.Feed(stream[pos:end]) {
				got = append(got, event.Callsign)
			}
			pos = end
		}
		if len(got) != 1 || got[0] != callsign {
			t.Errorf("trial=%d phase=%.3f narrow=%v decoded %q; scan=%d pending=%d", trial, phase, narrow, got, decoder.scanFrom, len(decoder.pending))
		}
	}
}

func TestFSKIDDecoderStressRandomGainNoiseOffsetAndChunking(t *testing.T) {
	rates := []int{12_000, 44_100, 48_000, 96_000}
	levels := []float64{0.008, 0.015, 0.03, 0.08, 0.35, 0.9}
	noiseLevels := []float64{0, 4, 10, 20, 40, 80}
	for trial := int64(0); trial < 96; trial++ {
		rng := rand.New(rand.NewSource(0xA0D10 + trial))
		rate := rates[trial%int64(len(rates))]
		level := levels[(trial/3)%int64(len(levels))]
		noise := noiseLevels[(trial/5)%int64(len(noiseLevels))]
		offset := []float64{-100, -70, -35, 0, 35, 70, 100}[trial%7]
		narrow := trial%2 == 1
		phase := rng.Float64() * 2 * math.Pi
		base := makeMMSSTVReferenceFSKIDWithPhase(t, rate, "BH2VSQ", narrow, offset, phase)
		prefix := rng.Intn(rate / 10)
		stream := make([]int16, prefix)
		for i := range stream {
			stream[i] = int16(rng.Intn(81) - 40)
		}
		// A small DC bias and independent noise approximate different audio
		// capture paths; clamp exactly as a 16-bit sound-card input would.
		dc := float64(rng.Intn(2001) - 1000)
		for _, sample := range base {
			v := float64(sample)*level + dc + (rng.Float64()*2-1)*noise
			if v > math.MaxInt16 {
				v = math.MaxInt16
			}
			if v < math.MinInt16 {
				v = math.MinInt16
			}
			stream = append(stream, int16(math.Round(v)))
		}
		decoder, err := NewFSKIDDecoder(rate)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for pos := 0; pos < len(stream); {
			// Exercise realistic variable audio callback sizes (20..125 ms).
			// Single-sample and very small boundaries are already covered by the
			// dedicated arbitrary-chunk and random-phase tests above; doing them
			// for every noisy/high-rate combination needlessly multiplies runtime.
			minChunk := max(1, rate*20/1000)
			maxExtra := max(1, rate/8-minChunk+1)
			end := min(len(stream), pos+minChunk+rng.Intn(maxExtra))
			for _, event := range decoder.Feed(stream[pos:end]) {
				got = append(got, event.Callsign)
			}
			pos = end
		}
		if len(got) != 1 || got[0] != "BH2VSQ" {
			t.Errorf("trial=%d rate=%d narrow=%v level=%.3f noise=%.1f dc=%.0f offset=%+.0f phase=%.2f decoded=%q scan=%d pending=%d", trial, rate, narrow, level, noise, dc, offset, phase, got, decoder.scanFrom, len(decoder.pending))
		}
	}
}

func TestFSKIDIsReceivedIndependentlyAfterEveryConsecutivePicture(t *testing.T) {
	const rate = 12_000
	picture, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	idA := makeMMSSTVReferenceFSKID(t, rate, "BH2VSQ", false)
	idB := makeMMSSTVReferenceFSKID(t, rate, "JA1ABC", true)
	stream := make([]int16, 0, 2*(len(picture)+len(idA)))
	stream = append(stream, picture...)
	stream = append(stream, idA...)
	stream = append(stream, picture...)
	stream = append(stream, idB...)

	streamDecoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	streamDecoder.SetDeferredFrameDecode(true)
	fskidDecoder, err := NewFSKIDDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	armed := false
	var decoded []string
	frameCount := 0
	for offset := 0; offset < len(stream); {
		end := min(len(stream), offset+997) // deliberately irregular capture blocks
		block := stream[offset:end]
		frames, feedErr := streamDecoder.Feed(context.Background(), block)
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		if len(frames) > 0 {
			frameCount += len(frames)
			fskidDecoder.Reset()
			armed = true // a new independent FSKID cycle starts for every image
			for _, event := range fskidDecoder.Feed(streamDecoder.PendingAudio()) {
				decoded = append(decoded, event.Callsign)
				armed = false
			}
		} else if armed {
			for _, event := range fskidDecoder.Feed(block) {
				decoded = append(decoded, event.Callsign)
				armed = false
			}
		}
		offset = end
	}
	if frameCount != 2 {
		t.Fatalf("decoded pictures = %d, want 2", frameCount)
	}
	if len(decoded) != 2 || decoded[0] != "BH2VSQ" || decoded[1] != "JA1ABC" {
		t.Fatalf("per-picture FSKID sequence = %q, want [BH2VSQ JA1ABC]", decoded)
	}
}
