package sstv

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	// MMSSTV source defines FSKINTVAL as an integer 22 milliseconds; do not
	// derive this as 1000/45.45 because that slowly shifts symbol boundaries.
	fskIDBitMS      = 22.0
	fskIDMarkHz     = 1900.0
	fskIDSpaceHz    = 2100.0
	fskIDMarkerCode = 0x2A
	fskIDEndCode    = 0x01
)

// AppendMMSSTVFSKID appends the MMSSTV-compatible station identification
// sequence after a completed analog SSTV picture. Symbols are emitted in the
// same LSB-first order used by MMSSTV's WriteFSK() implementation; its decoder
// reconstructs bytes by shifting right and inserting each incoming bit at B5.
// Empty callsigns are omitted.
func AppendMMSSTVFSKID(samples []int16, sampleRate int, callsign string, narrowBand bool) ([]int16, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" {
		return append([]int16(nil), samples...), nil
	}
	if !utf8.ValidString(callsign) {
		return nil, errors.New("FSKID callsign must be valid UTF-8")
	}
	if len(callsign) > 16 {
		return nil, errors.New("FSKID callsign cannot exceed 16 ASCII characters")
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
	var exactPosition float64
	writtenSamples := 0
	appendTone := func(frequency, durationMS float64) error {
		return appendMMSSTVTone(generator, frequency, durationMS, &exactPosition, &writtenSamples)
	}
	initialTone := 1500.0
	if narrowBand {
		initialTone = fskIDMarkHz
	}
	if err := appendTone(initialTone, 300); err != nil {
		return nil, err
	}
	if err := appendTone(fskIDSpaceHz, 100); err != nil {
		return nil, err
	}
	// FSKID has one 1900 Hz start tone before the six-bit symbol stream.
	if err := appendTone(fskIDMarkHz, fskIDBitMS); err != nil {
		return nil, err
	}
	for _, symbol := range symbols {
		for bit := 0; bit < 6; bit++ { // MMSSTV WriteFSK(): B0 through B5.
			frequency := fskIDSpaceHz
			if (symbol>>bit)&1 != 0 {
				frequency = fskIDMarkHz
			}
			if err := appendTone(frequency, fskIDBitMS); err != nil {
				return nil, err
			}
		}
	}

	out := make([]int16, 0, len(samples)+len(generator.samples))
	out = append(out, samples...)
	out = append(out, generator.samples...)
	return out, nil
}

// appendMMSSTVTone mirrors CSSTVMOD::Write(frequency, milliseconds):
// MMSSTV accumulates a floating sample position and emits samples while the
// integer sample cursor is below int(m_dPos), i.e. floor of the cumulative
// position. ToneGenerator intentionally rounds each segment and is therefore
// not an exact substitute at rates such as 44.1 kHz for repeated 22 ms symbols.
func appendMMSSTVTone(g *ToneGenerator, frequencyHz, durationMS float64, exactPosition *float64, writtenSamples *int) error {
	if g == nil || exactPosition == nil || writtenSamples == nil {
		return errors.New("invalid MMSSTV FSK tone generator")
	}
	if frequencyHz <= 0 || frequencyHz >= float64(g.SampleRate)/2 || durationMS < 0 || math.IsNaN(durationMS) || math.IsInf(durationMS, 0) {
		return errors.New("invalid MMSSTV FSK tone parameters")
	}
	*exactPosition += durationMS * float64(g.SampleRate) / 1000
	targetSamples := int(*exactPosition) // C++ int(m_dPos), truncating toward zero.
	count := targetSamples - *writtenSamples
	if count <= 0 {
		return nil
	}
	start := len(g.samples)
	g.samples = append(g.samples, make([]int16, count)...)
	phaseStep := 2 * math.Pi * frequencyHz / float64(g.SampleRate)
	scale := g.Amplitude * 32767
	for i := start; i < len(g.samples); i++ {
		g.samples[i] = int16(math.Round(math.Sin(g.phase) * scale))
		g.phase += phaseStep
		if g.phase >= 2*math.Pi || g.phase <= -2*math.Pi {
			g.phase = math.Mod(g.phase, 2*math.Pi)
		}
	}
	*writtenSamples = targetSamples
	return nil
}

// FSKIDEvent reports a station identity recovered independently of the SSTV
// image raster. It is delivered once the terminator and XOR checksum validate.
type FSKIDEvent struct{ Callsign string }

// FSKIDDecoder scans arbitrary PCM chunks for the MMSSTV FSKID preamble.
// MMSSTV's receiver acquires directly on the 2100 Hz / 100 ms guard that
// follows the optional 1500 Hz (normal) or 1900 Hz (narrow) leader. The guard
// itself supplies the AFC estimate, so clipped leaders do not prevent decode.
type fskIDCandidate struct {
	dataStart     int
	offsetHz      float64
	periodSamples float64
	nextTryLen    int
}

type FSKIDDecoder struct {
	rate     int
	pending  []int16
	scanFrom int
	waiting  []fskIDCandidate
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
	d.waiting = nil
}

// Feed returns newly validated identities. FSKID is only enabled after an SSTV
// frame completes, so this scanner can afford a one-millisecond acquisition
// grid without competing with the real-time image decoder. Tone classification
// uses only two Goertzel bins per payload symbol; it deliberately avoids the
// expensive multi-frequency 300 ms scans used by the earlier implementation.
func (d *FSKIDDecoder) Feed(samples []int16) []FSKIDEvent {
	if d == nil || d.rate <= 0 {
		return nil
	}
	if len(samples) > 0 {
		d.pending = append(d.pending, samples...)
	}
	var events []FSKIDEvent
	step := max(1, d.rate/1000)
	probeN := max(1, int(40*float64(d.rate)/1000))
	sampleAt := func(milliseconds float64) int {
		// Match MMSSTV's cumulative int(m_dPos) conversion exactly. Its C++
		// transmitter uses FSKINTVAL=22 ms, not 1000/45.45 ms.
		return int(milliseconds * float64(d.rate) / 1000)
	}
	// The complete timing preamble includes the 100 ms guard, one start bit,
	// and six alternating STX bits. TxSampOffset scales all durations, so wait
	// for a conservative 280 ms before trying to recover the symbol clock.
	preambleSamples := sampleAt(280)

	// Resume candidates as more samples arrive. Timing was recovered from the
	// fixed MMSSTV start+0x2A sequence, so payload decoding uses that one clock
	// rather than guessing baud from the guard's carrier frequency.
	waiting := d.waiting[:0]
	for _, candidate := range d.waiting {
		if len(d.pending) < candidate.nextTryLen {
			waiting = append(waiting, candidate)
			continue
		}
		callsign, consumed, status := d.decodeFSKIDPayload(candidate.dataStart, candidate.offsetHz, candidate.periodSamples, sampleAt)
		switch status {
		case fskIDParseIncomplete:
			candidate.nextTryLen = len(d.pending) + max(1, d.rate/200) // Retry every 5 ms.
			waiting = append(waiting, candidate)
		case fskIDParseValid:
			events = append(events, FSKIDEvent{Callsign: callsign})
			if consumed < len(d.pending) {
				d.pending = append([]int16(nil), d.pending[consumed:]...)
			} else {
				d.pending = nil
			}
			d.scanFrom = 0
			d.waiting = nil
			return events
		}
	}
	d.waiting = waiting
	if len(d.waiting) > 0 {
		return events
	}

	// MMSSTV's receiver acquires on the 2100 Hz / 100 ms guard. The optional
	// 1500 Hz (normal) or 1900 Hz (narrow) leader may already have been consumed
	// by the SSTV frame boundary, so it is deliberately not required here.
	for d.scanFrom+preambleSamples <= len(d.pending) {
		start := d.scanFrom
		if start+probeN > len(d.pending) {
			break
		}
		probe := d.pending[start : start+probeN]
		observed := estimateToneFrequency(probe, d.rate)
		if observed <= 0 || math.Abs(observed-fskIDSpaceHz) > 120 {
			d.scanFrom += step
			continue
		}

		// Refine the leading edge of the 2100 Hz guard. The coarse 40 ms scan
		// can begin just before the transition; this short search removes most
		// of its window bias. The timing fit below absorbs the remaining phase.
		start = refineFSKToneStart(d.pending, start, fskIDSpaceHz, d.rate, sampleAt)
		guardProbeStart := start + sampleAt(10)
		guardProbeEnd := start + sampleAt(80)
		if guardProbeStart < 0 || guardProbeEnd > len(d.pending) || guardProbeEnd <= guardProbeStart {
			d.scanFrom += step
			continue
		}
		stableGuard := d.pending[guardProbeStart:guardProbeEnd]
		guardFrequency := estimateToneFrequency(stableGuard, d.rate)
		if guardFrequency <= 0 || math.Abs(guardFrequency-fskIDSpaceHz) > 120 {
			d.scanFrom += step
			continue
		}
		offset := guardFrequency - fskIDSpaceHz
		if classifyFSKTone(stableGuard, d.rate, fskIDMarkHz+offset, fskIDSpaceHz+offset) != fskIDSpaceHz+offset {
			d.scanFrom += step
			continue
		}

		dataStart, periodSamples, timingOK := d.findMMSSTVFSKTiming(start, offset, sampleAt)
		if !timingOK {
			// A complete 280 ms candidate was examined; skip its guard and
			// preamble instead of repeatedly running the timing fit on the same
			// long 2100 Hz region.
			d.scanFrom = max(start+step, start+preambleSamples)
			continue
		}

		callsign, consumed, status := d.decodeFSKIDPayload(dataStart, offset, periodSamples, sampleAt)
		switch status {
		case fskIDParseValid:
			events = append(events, FSKIDEvent{Callsign: callsign})
			if consumed < len(d.pending) {
				d.pending = append([]int16(nil), d.pending[consumed:]...)
			} else {
				d.pending = nil
			}
			d.scanFrom = 0
			d.waiting = nil
			return events
		case fskIDParseIncomplete:
			if len(d.waiting) < 8 {
				d.waiting = append(d.waiting, fskIDCandidate{
					dataStart: dataStart, offsetHz: offset, periodSamples: periodSamples,
					nextTryLen: dataStart + max(1, sampleAt(800)),
				})
			}
		}
		d.scanFrom = max(start+step, start+preambleSamples)
	}

	// Retain at most eight seconds, enough for a 16-character callsign plus
	// optional post-ID data. Keep all incomplete candidate offsets valid.
	if d.scanFrom > d.rate*2 {
		trim := d.scanFrom - d.rate
		for _, candidate := range d.waiting {
			if candidate.dataStart < trim {
				trim = candidate.dataStart
			}
		}
		if trim > 0 {
			d.pending = append([]int16(nil), d.pending[trim:]...)
			d.scanFrom -= trim
			for i := range d.waiting {
				d.waiting[i].dataStart -= trim
				d.waiting[i].nextTryLen = max(0, d.waiting[i].nextTryLen-trim)
			}
		}
	}
	if len(d.pending) > d.rate*8 {
		trim := len(d.pending) - d.rate*8
		d.pending = append([]int16(nil), d.pending[trim:]...)
		d.scanFrom = max(0, d.scanFrom-trim)
		kept := d.waiting[:0]
		for _, candidate := range d.waiting {
			if candidate.dataStart >= trim {
				candidate.dataStart -= trim
				candidate.nextTryLen = max(0, candidate.nextTryLen-trim)
				kept = append(kept, candidate)
			}
		}
		d.waiting = kept
	}
	return events
}

// findMMSSTVFSKTiming recovers the symbol clock from MMSSTV's known FSK
// preamble. TxSampOffset changes the duration of every Write() call but does
// not change CVCO's 1900/2100 Hz frequencies. The preamble contains seven
// known alternating tones (start bit + six LSB-first bits of 0x2A). We locate
// the seven tone transitions from the zero crossing of their narrowband energy
// ratio, then line-fit their sample positions to recover phase and bit period.
func (d *FSKIDDecoder) findMMSSTVFSKTiming(guardStart int, offsetHz float64, sampleAt func(float64) int) (int, float64, bool) {
	if guardStart < 0 || guardStart >= len(d.pending) || d.rate <= 0 {
		return 0, 0, false
	}
	expected := [...]float64{
		fskIDMarkHz, fskIDSpaceHz, fskIDMarkHz, fskIDSpaceHz,
		fskIDMarkHz, fskIDSpaceHz, fskIDMarkHz,
	}
	markHz, spaceHz := fskIDMarkHz+offsetHz, fskIDSpaceHz+offsetHz
	windowN := sampleAt(6)
	searchRadius := sampleAt(13)
	searchStep := max(1, d.rate/10_000) // approximately 0.1 ms
	if windowN < 16 || searchRadius < 1 {
		return 0, 0, false
	}
	contrast := func(center int, previousHz, nextHz float64) (float64, bool) {
		from := center - windowN/2
		to := from + windowN
		if from < 0 || to > len(d.pending) || to <= from {
			return 0, false
		}
		segment := d.pending[from:to]
		previousPower := goertzelPowerHann(segment, d.rate, previousHz)
		nextPower := goertzelPowerHann(segment, d.rate, nextHz)
		if previousPower <= 1e-20 || nextPower <= 1e-20 {
			return 0, false
		}
		return math.Log(nextPower / previousPower), true
	}
	// Each transition is expected near the nominal 100+22*n millisecond
	// position. The ±13 ms search covers the common TxSampOffset range while
	// staying closer to its own boundary than either neighbouring bit edge.
	boundaries := make([]float64, 0, 7)
	for boundary := 0; boundary < 7; boundary++ {
		previousHz := spaceHz
		if boundary > 0 {
			previousHz = expected[boundary-1] + offsetHz
		}
		nextHz := expected[boundary] + offsetHz
		nominal := guardStart + sampleAt(100+float64(boundary)*fskIDBitMS)
		searchFrom := nominal - searchRadius
		searchTo := nominal + searchRadius
		var previousContrast float64
		previousPosition := 0
		havePrevious := false
		bestCrossing := 0.0
		bestDistance := math.Inf(1)
		for center := searchFrom; center <= searchTo; center += searchStep {
			value, ok := contrast(center, previousHz, nextHz)
			if !ok {
				continue
			}
			if havePrevious && previousContrast <= 0 && value >= 0 {
				denominator := value - previousContrast
				fraction := 0.5
				if math.Abs(denominator) > 1e-9 {
					fraction = -previousContrast / denominator
				}
				crossing := float64(previousPosition) + fraction*float64(center-previousPosition)
				distance := math.Abs(crossing - float64(nominal))
				if distance < bestDistance {
					bestCrossing, bestDistance = crossing, distance
				}
			}
			previousContrast, previousPosition, havePrevious = value, center, true
		}
		if bestDistance == math.Inf(1) {
			return 0, 0, false
		}
		boundaries = append(boundaries, bestCrossing-float64(guardStart))
	}

	// Least-squares fit y = intercept + boundaryIndex*period. The first point
	// is the guard/start-bit transition at nominal 100 ms; six further points
	// come from the alternating start+0x2A sequence.
	const count = 7.0
	meanX, meanY := 3.0, 0.0
	for _, value := range boundaries {
		meanY += value / count
	}
	var covariance, variance float64
	for i, value := range boundaries {
		x := float64(i)
		covariance += (x - meanX) * (value - meanY)
		variance += (x - meanX) * (x - meanX)
	}
	if variance <= 0 {
		return 0, 0, false
	}
	periodSamples := covariance / variance
	nominalPeriod := float64(d.rate) * fskIDBitMS / 1000
	if periodSamples < nominalPeriod*0.95 || periodSamples > nominalPeriod*1.05 {
		return 0, 0, false
	}
	intercept := meanY - periodSamples*meanX
	if intercept < float64(sampleAt(90)) || intercept > float64(sampleAt(110)) {
		return 0, 0, false
	}

	// Validate the recovered preamble at the fitted timing before accepting a
	// possible station-ID candidate.
	matched := 0
	for i, want := range expected {
		from := guardStart + int(math.Round(intercept+float64(i)*periodSamples)) + sampleAt(5)
		to := from + sampleAt(12)
		if from < 0 || to > len(d.pending) || to <= from {
			return 0, 0, false
		}
		tone := classifyFSKTone(d.pending[from:to], d.rate, markHz, spaceHz)
		if tone == want+offsetHz {
			matched++
		}
	}
	if matched < 6 {
		return 0, 0, false
	}
	dataStart := guardStart + int(math.Round(intercept+periodSamples))
	return dataStart, periodSamples, dataStart >= 0
}

// goertzelPowerHann applies a Hann window before measuring one FSK bin. This
// suppresses leakage from the adjacent 200 Hz tone when the short sliding
// windows straddle a symbol transition.
func goertzelPowerHann(samples []int16, sampleRate int, frequency float64) float64 {
	if len(samples) < 2 || sampleRate <= 0 {
		return 0
	}
	omega := 2 * math.Pi * frequency / float64(sampleRate)
	coeff := 2 * math.Cos(omega)
	s1, s2 := 0.0, 0.0
	denominator := float64(len(samples) - 1)
	for i, sample := range samples {
		window := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/denominator)
		x := float64(sample) / 32768 * window
		s0 := x + coeff*s1 - s2
		s2, s1 = s1, s0
	}
	return s1*s1 + s2*s2 - coeff*s1*s2
}

// refineFSKToneStart removes the leading-window bias from the 40 ms coarse
// detector. The search uses the expected 2100 Hz guard rather than the mixed
// coarse-window estimate, so a window straddling the 1900/2100 transition is
// not mistaken for a stable guard. A small correction places the start just
// after the measured transition.
func refineFSKToneStart(samples []int16, approximate int, targetHz float64, sampleRate int, sampleAt func(float64) int) int {
	if sampleRate <= 0 || approximate < 0 || approximate >= len(samples) {
		return approximate
	}
	window := max(1, sampleAt(5))
	limit := min(len(samples)-window, approximate+sampleAt(45))
	step := max(1, sampleRate/20_000)
	tolerance := math.Max(90, targetHz*0.02)
	for pos := approximate; pos <= limit; pos += step {
		frequency := estimateToneFrequency(samples[pos:pos+window], sampleRate)
		if frequency > 0 && math.Abs(frequency-targetHz) <= tolerance {
			return pos + max(1, sampleAt(0.5))
		}
	}
	return approximate
}

// estimateToneFrequency measures rising zero crossings after removing DC. It
// is used only on the stable FSKID leader/guard, not on arbitrary image audio.
func estimateToneFrequency(samples []int16, sampleRate int) float64 {
	if len(samples) < 4 || sampleRate <= 0 {
		return 0
	}
	var mean float64
	for _, sample := range samples {
		mean += float64(sample)
	}
	mean /= float64(len(samples))
	var first, last float64
	crossings := 0
	previous := float64(samples[0]) - mean
	for i := 1; i < len(samples); i++ {
		current := float64(samples[i]) - mean
		if previous <= 0 && current > 0 {
			fraction := -previous / (current - previous)
			position := float64(i-1) + fraction
			if crossings == 0 {
				first = position
			} else {
				last = position
			}
			crossings++
		}
		previous = current
	}
	if crossings < 3 || last <= first {
		return 0
	}
	return float64(crossings-1) * float64(sampleRate) / (last - first)
}

// classifyFSKTone returns the stronger of the shifted MMSSTV mark/space tones.
// Goertzel runs only two exact bins per symbol, keeping FSKID CPU use bounded.
func classifyFSKTone(samples []int16, sampleRate int, markHz, spaceHz float64) float64 {
	tone, _ := classifyFSKToneConfidence(samples, sampleRate, markHz, spaceHz)
	return tone
}

func classifyFSKToneConfidence(samples []int16, sampleRate int, markHz, spaceHz float64) (float64, float64) {
	if len(samples) == 0 || sampleRate <= 0 || markHz <= 0 || spaceHz <= 0 || markHz >= float64(sampleRate)/2 || spaceHz >= float64(sampleRate)/2 {
		return 0, 0
	}
	var energy float64
	for _, sample := range samples {
		x := float64(sample) / 32768
		energy += x * x
	}
	if energy/float64(len(samples)) <= 1e-11 {
		return 0, 0
	}
	markPower := goertzelPower(samples, sampleRate, markHz)
	spacePower := goertzelPower(samples, sampleRate, spaceHz)
	best, second := markPower, spacePower
	bestTone := markHz
	if spacePower > markPower {
		best, second = spacePower, markPower
		bestTone = spaceHz
	}
	if best/(energy*float64(len(samples))) < 0.003 || (second > 0 && best/second < 1.025) {
		return 0, 0
	}
	if second <= 1e-20 {
		second = 1e-20
	}
	return bestTone, math.Log(best / second)
}

func symbolWindow(samples []int16, symbolStart int, sampleAt func(float64) int) []int16 {
	from := symbolStart + sampleAt(5)
	to := symbolStart + sampleAt(17)
	if from < 0 || to > len(samples) || to <= from {
		return nil
	}
	return samples[from:to]
}

type fskIDParseStatus uint8

const (
	fskIDParseInvalid fskIDParseStatus = iota
	fskIDParseIncomplete
	fskIDParseValid
)

func (d *FSKIDDecoder) decodeFSKIDPayload(dataStart int, offsetHz, periodSamples float64, sampleAt func(float64) int) (string, int, fskIDParseStatus) {
	if dataStart < 0 || periodSamples <= 0 {
		return "", 0, fskIDParseInvalid
	}
	// The timing preamble has already identified both scale and phase. Only a
	// small local phase correction remains for the Goertzel symbol windows;
	// do not infer baud from carrier offset (MMSSTV TxSampOffset does not alter
	// the 1900/2100 Hz VCO frequencies).
	periodMS := periodSamples * 1000 / float64(d.rate)
	if len(d.pending)-dataStart < sampleAt(800*periodMS/fskIDBitMS) {
		return "", 0, fskIDParseIncomplete
	}
	phasesMS := [...]float64{0, -0.5, 0.5, -1, 1}
	bestCallsign := ""
	bestConsumed := 0
	bestConfidence := math.Inf(-1)
	incomplete := false
	for _, phaseMS := range phasesMS {
		phaseSamples := phaseMS * float64(d.rate) / 1000
		callsign, consumed, confidence, status := d.decodeFSKIDPayloadAtTimingScored(
			dataStart, offsetHz, phaseSamples, periodSamples, sampleAt,
		)
		switch status {
		case fskIDParseValid:
			if confidence > bestConfidence {
				bestCallsign, bestConsumed, bestConfidence = callsign, consumed, confidence
			}
		case fskIDParseIncomplete:
			incomplete = true
		}
	}
	// A valid result already contains the full marker, terminator and XOR
	// checksum. Do not let a neighbouring phase that needs a little more tail
	// audio postpone that complete, validated identity indefinitely.
	if bestCallsign != "" {
		return bestCallsign, bestConsumed, fskIDParseValid
	}
	if incomplete {
		return "", 0, fskIDParseIncomplete
	}
	return "", 0, fskIDParseInvalid
}

func (d *FSKIDDecoder) decodeFSKIDPayloadAtTimingScored(dataStart int, offsetHz, phaseSamples, periodSamples float64, sampleAt func(float64) int) (string, int, float64, fskIDParseStatus) {
	dataBitIndex := 0
	decisionOffset := sampleAt(5)
	decisionLength := sampleAt(12)
	confidenceSum := 0.0
	readSymbol := func() (uint8, fskIDParseStatus) {
		var value uint8
		for bit := 0; bit < 6; bit++ {
			bitOffset := int(math.Round(phaseSamples + float64(dataBitIndex)*periodSamples))
			symbolStart := dataStart + bitOffset + decisionOffset
			symbolEnd := symbolStart + decisionLength
			if symbolStart < 0 || symbolEnd > len(d.pending) || symbolEnd <= symbolStart {
				return 0, fskIDParseIncomplete
			}
			tone, confidence := classifyFSKToneConfidence(d.pending[symbolStart:symbolEnd], d.rate, fskIDMarkHz+offsetHz, fskIDSpaceHz+offsetHz)
			if tone == 0 {
				return 0, fskIDParseInvalid
			}
			confidenceSum += confidence
			if tone == fskIDMarkHz+offsetHz {
				value |= 1 << bit // MMSSTV WriteFSK() transmits B0 first.
			}
			dataBitIndex++
		}
		return value, fskIDParseValid
	}

	marker, status := readSymbol()
	if status != fskIDParseValid {
		return "", 0, 0, status
	}
	if marker != fskIDMarkerCode {
		return "", 0, 0, fskIDParseInvalid
	}

	chars := make([]byte, 0, 12)
	var checksum uint8
	terminated := false
	for count := 0; count <= 16; count++ {
		value, status := readSymbol()
		if status != fskIDParseValid {
			return "", 0, 0, status
		}
		if value == fskIDEndCode {
			terminated = true
			break
		}
		if len(chars) >= 16 || value > 0x3f {
			return "", 0, 0, fskIDParseInvalid
		}
		chars = append(chars, byte(value+0x20))
		checksum ^= value
	}
	if !terminated || len(chars) == 0 {
		return "", 0, 0, fskIDParseInvalid
	}
	gotChecksum, status := readSymbol()
	if status != fskIDParseValid {
		return "", 0, 0, status
	}
	if gotChecksum != checksum {
		return "", 0, 0, fskIDParseInvalid
	}
	callsign := strings.TrimSpace(string(chars))
	if callsign == "" {
		return "", 0, 0, fskIDParseInvalid
	}
	consumed := dataStart + int(math.Round(phaseSamples+float64(dataBitIndex)*periodSamples))
	confidence := confidenceSum / float64(dataBitIndex)
	return callsign, consumed, confidence, fskIDParseValid
}
