package sstv

import (
	"fmt"
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
			// FSKID.TXT: $2A, ('N' - $20), $01, XOR checksum.
			for _, symbol := range []uint8{0x2A, uint8('N' - 0x20), 0x01, uint8('N' - 0x20)} {
				for bit := 5; bit >= 0; bit-- {
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
	if len(decoder.pending) != 0 || decoder.scanFrom != 0 {
		t.Fatalf("reset left stale FSKID state: pending=%d scanFrom=%d", len(decoder.pending), decoder.scanFrom)
	}
	if events := decoder.Feed(wave); len(events) != 1 || events[0].Callsign != "BH2VSQ" {
		t.Fatalf("FSKID after reset = %+v, want BH2VSQ", events)
	}
}
