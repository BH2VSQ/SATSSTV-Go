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
	var samples []int16
	var exactPosition float64
	written := 0
	phase := 0.0
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
