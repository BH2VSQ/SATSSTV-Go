package sstv

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	fskIDBaud       = 45.45
	fskIDBitMS      = 1000.0 / fskIDBaud
	fskIDMarkHz     = 1900.0
	fskIDSpaceHz    = 2100.0
	fskIDMarkerCode = 0x2A
	fskIDEndCode    = 0x01
)

// AppendMMSSTVFSKID appends the MMSSTV-compatible station identification
// sequence after a completed analog SSTV picture. The six-bit symbols are
// transmitted most-significant bit first, as specified by MMSSTV FSKID.TXT.
// Empty callsigns are omitted.
func AppendMMSSTVFSKID(samples []int16, sampleRate int, callsign string, narrowBand bool) ([]int16, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" {
		return append([]int16(nil), samples...), nil
	}
	if !utf8.ValidString(callsign) {
		return nil, errors.New("FSKID callsign must be valid UTF-8")
	}
	if len(callsign) > 24 {
		return nil, errors.New("FSKID callsign cannot exceed 24 ASCII characters")
	}
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("invalid FSKID sample rate %d", sampleRate)
	}

	symbols := make([]uint8, 0, len(callsign)+3)
	symbols = append(symbols, fskIDMarkerCode)
	var checksum uint8
	for _, r := range callsign {
		if r < 0x20 || r > 0x5f {
			return nil, fmt.Errorf("FSKID callsign contains unsupported character %q; use ASCII 0x20..0x5f", r)
		}
		value := uint8(r - 0x20)
		symbols = append(symbols, value)
		checksum ^= value
	}
	symbols = append(symbols, fskIDEndCode, checksum)

	generator, err := NewToneGenerator(sampleRate, DefaultLevel)
	if err != nil {
		return nil, err
	}
	initialTone := 1500.0
	if narrowBand {
		initialTone = fskIDMarkHz
	}
	if err := generator.ToneMS(initialTone, 300); err != nil {
		return nil, err
	}
	if err := generator.ToneMS(fskIDSpaceHz, 100); err != nil {
		return nil, err
	}
	// FSKID has one 1900 Hz start tone before the six-bit symbol stream.
	if err := generator.ToneMS(fskIDMarkHz, fskIDBitMS); err != nil {
		return nil, err
	}
	for _, symbol := range symbols {
		for bit := 5; bit >= 0; bit-- { // B5 through B0.
			frequency := fskIDSpaceHz
			if (symbol>>bit)&1 != 0 {
				frequency = fskIDMarkHz
			}
			if err := generator.ToneMS(frequency, fskIDBitMS); err != nil {
				return nil, err
			}
		}
	}

	out := make([]int16, 0, len(samples)+len(generator.samples))
	out = append(out, samples...)
	out = append(out, generator.Samples()...)
	return out, nil
}

// FSKIDEvent reports a station identity recovered independently of the SSTV
// image raster. It is delivered once the terminator and XOR checksum validate.
type FSKIDEvent struct{ Callsign string }

// FSKIDDecoder scans arbitrary PCM chunks for the MMSSTV FSKID preamble. It
// accepts the normal 1500 Hz leader and the narrow-band 1900 Hz leader.
type FSKIDDecoder struct {
	rate     int
	pending  []int16
	scanFrom int
}

func NewFSKIDDecoder(sampleRate int) (*FSKIDDecoder, error) {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("invalid FSKID sample rate %d", sampleRate)
	}
	return &FSKIDDecoder{rate: sampleRate}, nil
}

// Reset discards an incomplete station-ID candidate after an input stream gap.
func (d *FSKIDDecoder) Reset() {
	if d == nil {
		return
	}
	d.pending = nil
	d.scanFrom = 0
}

// Feed returns newly validated identities. The one-millisecond search step is
// important: FSKID may begin at any sample offset, and a coarse ten-millisecond
// search can misalign the single start bit and every following data symbol.
func (d *FSKIDDecoder) Feed(samples []int16) []FSKIDEvent {
	if d == nil {
		return nil
	}
	d.pending = append(d.pending, samples...)
	var events []FSKIDEvent

	step := max(1, d.rate/1000)
	probeN := max(1, int(math.Round(.040*float64(d.rate))))
	firstToneN := int(math.Round(.300 * float64(d.rate)))
	secondToneN := int(math.Round(.100 * float64(d.rate)))
	bitN := int(math.Round(fskIDBitMS / 1000 * float64(d.rate)))
	transitionProbeN := max(1, int(math.Round(.005*float64(d.rate))))
	minimum := firstToneN + secondToneN + bitN*4

	for d.scanFrom+minimum <= len(d.pending) {
		start := d.scanFrom
		probe := d.pending[start : start+probeN]
		firstFreq := bestTone(probe, d.rate, 1500, fskIDMarkHz)
		if firstFreq != 1500 && firstFreq != fskIDMarkHz {
			d.scanFrom += step
			continue
		}
		if bestTone(d.pending[start:start+firstToneN], d.rate, 1500, fskIDMarkHz) != firstFreq {
			d.scanFrom += step
			continue
		}
		// Verify both sides of the 300 ms transition. Looking only at the
		// whole 100 ms second tone lets a false candidate shifted by 10 ms
		// pass and can hold the scanner while it waits for a bogus long ID.
		if bestTone(d.pending[start+firstToneN-transitionProbeN:start+firstToneN], d.rate, 1500, fskIDMarkHz) != firstFreq ||
			bestTone(d.pending[start+firstToneN+transitionProbeN:start+firstToneN+2*transitionProbeN], d.rate, fskIDSpaceHz, fskIDMarkHz) != fskIDSpaceHz {
			d.scanFrom += step
			continue
		}

		p := start + firstToneN
		if bestTone(d.pending[p:p+secondToneN], d.rate, fskIDSpaceHz, fskIDMarkHz) != fskIDSpaceHz ||
			bestTone(d.pending[p+secondToneN-transitionProbeN:p+secondToneN], d.rate, fskIDSpaceHz, fskIDMarkHz) != fskIDSpaceHz ||
			bestTone(d.pending[p+secondToneN+transitionProbeN:p+secondToneN+2*transitionProbeN], d.rate, fskIDMarkHz, fskIDSpaceHz) != fskIDMarkHz {
			d.scanFrom += step
			continue
		}
		p += secondToneN
		if bestTone(d.pending[p:p+bitN], d.rate, fskIDMarkHz, fskIDSpaceHz) != fskIDMarkHz {
			d.scanFrom += step
			continue
		}
		p += bitN

		readSymbol := func() (uint8, bool) {
			if p+6*bitN > len(d.pending) {
				return 0, false
			}
			var value uint8
			for bit := 5; bit >= 0; bit-- {
				tone := bestTone(d.pending[p:p+bitN], d.rate, fskIDMarkHz, fskIDSpaceHz)
				if tone == fskIDMarkHz {
					value |= 1 << bit
				} else if tone != fskIDSpaceHz {
					return 0, false
				}
				p += bitN
			}
			return value, true
		}

		if p+6*bitN > len(d.pending) {
			break
		}
		marker, ok := readSymbol()
		if !ok {
			d.scanFrom += step
			continue
		}
		if marker != fskIDMarkerCode {
			d.scanFrom += step
			continue
		}

		chars := make([]byte, 0, 12)
		var checksum uint8
		terminated := false
		needMore := false
		malformed := false
		for count := 0; count <= 24; count++ {
			if p+6*bitN > len(d.pending) {
				needMore = true
				break
			}
			value, valid := readSymbol()
			if !valid {
				malformed = true
				break
			}
			if value == fskIDEndCode {
				terminated = true
				break
			}
			if value > 0x3f || len(chars) >= 24 {
				break
			}
			chars = append(chars, byte(value+0x20))
			checksum ^= value
		}
		if needMore {
			break
		}
		if malformed || !terminated {
			d.scanFrom += step
			continue
		}
		if p+6*bitN > len(d.pending) {
			break
		}
		gotChecksum, valid := readSymbol()
		if !valid {
			d.scanFrom += step
			continue
		}
		if len(chars) > 0 && gotChecksum == checksum {
			events = append(events, FSKIDEvent{Callsign: string(chars)})
			d.pending = append([]int16(nil), d.pending[p:]...)
			d.scanFrom = 0
			continue
		}
		d.scanFrom += step
	}

	// Keep one second of history so preambles split across callback blocks are
	// still detected, while bounding memory during long receiver idle periods.
	if d.scanFrom > d.rate*2 {
		trim := d.scanFrom - d.rate
		d.pending = append([]int16(nil), d.pending[trim:]...)
		d.scanFrom -= trim
	}
	if len(d.pending) > d.rate*8 {
		trim := len(d.pending) - d.rate*8
		d.pending = append([]int16(nil), d.pending[trim:]...)
		d.scanFrom = max(0, d.scanFrom-trim)
	}
	return events
}
