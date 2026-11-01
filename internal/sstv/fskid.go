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
			// Retry as soon as any new PCM arrives. A fixed 40 ms delay can
			// postpone the final checksum decision beyond the end of a capture
			// window or a device callback stream that stops at the packet tail.
			candidate.nextTryLen = len(d.pending) + 1
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
		// Keep the grid origin separate from the refined Guard edge. Refinement
		// is allowed to move backward into the preceding leader; using that
		// value to update scanFrom can otherwise move the scanner backward and
		// repeatedly inspect the same narrow-band leader forever.
		candidateOrigin := d.scanFrom
		start := candidateOrigin
		if start+probeN > len(d.pending) {
			break
		}
		probe := d.pending[start : start+probeN]
		observed := estimateToneFrequency(probe, d.rate)
		if observed <= 0 || math.Abs(observed-fskIDSpaceHz) > 120 {
			d.scanFrom += step
			continue
		}

		// The coarse 40 ms scan intentionally tolerates noisy frequency
		// estimates. Re-identify the stable tone with a DC-rejected Goertzel
		// frequency search before accepting it as a 2100 Hz guard. This is
		// essential in narrow mode: 1900 Hz + AFC must never masquerade as 2100.
		coarseProbeStart := start + sampleAt(10)
		coarseProbeEnd := start + sampleAt(80)
		if coarseProbeStart < 0 || coarseProbeEnd > len(d.pending) || coarseProbeEnd <= coarseProbeStart {
			d.scanFrom += step
			continue
		}
		stableProbe := d.pending[coarseProbeStart:coarseProbeEnd]
		guardFrequency := estimateFSKDominantFrequency(stableProbe, d.rate)
		// MMSSTV receive offset is expected within ±80 Hz. A ±100 Hz stable
		// guard window leaves separation from the shifted 1900 Hz narrow leader.
		if guardFrequency <= 0 || math.Abs(guardFrequency-fskIDSpaceHz) > 100 {
			d.scanFrom += step
			continue
		}
		offset := guardFrequency - fskIDSpaceHz
		start = refineFSKSpaceTransition(d.pending, start, d.rate, fskIDMarkHz+offset, fskIDSpaceHz+offset, sampleAt)
		guardProbeStart := start + sampleAt(10)
		guardProbeEnd := start + sampleAt(80)
		if guardProbeStart < 0 || guardProbeEnd > len(d.pending) || guardProbeEnd <= guardProbeStart {
			d.scanFrom += step
			continue
		}
		stableGuard := d.pending[guardProbeStart:guardProbeEnd]
		confirmedFrequency := estimateFSKDominantFrequency(stableGuard, d.rate)
		if confirmedFrequency <= 0 || math.Abs(confirmedFrequency-fskIDSpaceHz) > 100 {
			d.scanFrom += step
			continue
		}
		offset = confirmedFrequency - fskIDSpaceHz
		if classifyFSKTone(stableGuard, d.rate, fskIDMarkHz+offset, fskIDSpaceHz+offset) != fskIDSpaceHz+offset {
			d.scanFrom += step
			continue
		}
		// A shifted 1900 Hz narrow-band leader can land at 2000 Hz when the
		// receiver AFC is +100 Hz, exactly on the edge of the broad Guard
		// acquisition range. Do not lock on frequency alone: a true 2100 Hz
		// Guard must be followed 100 ms later by the corresponding 1900 Hz
		// Start tone using the SAME AFC estimate. This sequence check rejects
		// the 300 ms narrow-band leader before attempting expensive clock fit.
		if !hasMMSSTVFSKStartAfterGuard(d.pending, start, d.rate, fskIDMarkHz+offset, fskIDSpaceHz+offset, sampleAt) {
			d.scanFrom = candidateOrigin + max(step, sampleAt(5))
			continue
		}

		dataStart, periodSamples, timingOK := d.findMMSSTVFSKTiming(start, offset, sampleAt)
		if !timingOK {
			// The 1 ms acquisition grid can encounter the leading edge of a real
			// guard several milliseconds before the 40/70 ms probes are fully
			// inside it. Such an early candidate can pass the coarse 2100 Hz
			// checks but lack enough clean 100 ms guard for the preamble fit. Do
			// NOT skip the whole 280 ms preamble here: that jumps over the actual
			// guard and makes FSKID reception depend on the capture/grid phase.
			// Advance only one acquisition step and retry the same guard at a
			// slightly later origin.
			d.scanFrom = candidateOrigin + step
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
			// Once the complete Guard/Start/0x2A preamble has validated, retain
			// exactly one payload candidate and stop scanning this same 100 ms
			// Guard at every 1 ms grid point. Re-running the full timing fit for
			// neighbouring origins created many duplicate candidates and could
			// starve the side-channel worker on long/noisy IDs.
			d.waiting = append(d.waiting[:0], fskIDCandidate{
				dataStart: dataStart, offsetHz: offset, periodSamples: periodSamples,
				nextTryLen: len(d.pending) + 1,
			})
			d.scanFrom = max(candidateOrigin+step, dataStart)
			return events
		}
		// A fully validated preamble must not be reconsidered at every
		// neighbouring acquisition-grid point if its payload later proves bad.
		d.scanFrom = max(candidateOrigin+step, dataStart)
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
	// Sound-card capture paths can add a sizeable DC bias. Remove it before
	// windowing; otherwise the finite Hann window leaks that offset into both
	// FSK bins and can move the 1900/2100 energy crossover unpredictably.
	mean := 0.0
	for _, sample := range samples {
		mean += float64(sample)
	}
	mean /= float64(len(samples))
	omega := 2 * math.Pi * frequency / float64(sampleRate)
	coeff := 2 * math.Cos(omega)
	s1, s2 := 0.0, 0.0
	denominator := float64(len(samples) - 1)
	for i, sample := range samples {
		window := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/denominator)
		x := (float64(sample) - mean) / 32768 * window
		s0 := x + coeff*s1 - s2
		s2, s1 = s1, s0
	}
	return s1*s1 + s2*s2 - coeff*s1*s2
}

// estimateFSKDominantFrequency finds the strongest tone in the FSK band using
// DC-rejected Hann Goertzel measurements. A coarse scan followed by a local
// refinement is more stable than counting zero crossings in weak, biased audio.
func estimateFSKDominantFrequency(samples []int16, sampleRate int) float64 {
	if len(samples) < 16 || sampleRate <= 0 {
		return 0
	}
	bestFrequency, bestPower := 0.0, 0.0
	for frequency := 1_800.0; frequency <= 2_300; frequency += 20 {
		power := goertzelPowerHann(samples, sampleRate, frequency)
		if power > bestPower {
			bestFrequency, bestPower = frequency, power
		}
	}
	if bestFrequency == 0 || bestPower <= 1e-14 {
		return 0
	}
	coarseBest := bestFrequency
	for frequency := math.Max(1_780, coarseBest-20); frequency <= math.Min(2_320, coarseBest+20); frequency += 4 {
		power := goertzelPowerHann(samples, sampleRate, frequency)
		if power > bestPower {
			bestFrequency, bestPower = frequency, power
		}
	}
	// A maximum bin always exists, even for broadband noise. Require the
	// selected carrier to explain a meaningful fraction of the AC energy so
	// random noise peaks cannot start expensive timing fits or create a false
	// 2100 Hz Guard candidate before the actual post-picture FSKID.
	var mean float64
	for _, sample := range samples {
		mean += float64(sample)
	}
	mean /= float64(len(samples))
	var energy float64
	for _, sample := range samples {
		x := (float64(sample) - mean) / 32768
		energy += x * x
	}
	if energy <= 1e-18 {
		return 0
	}
	coherentPower := goertzelPowerZeroMean(samples, sampleRate, bestFrequency, mean)
	if coherentPower/(energy*float64(len(samples))) < 0.012 {
		return 0
	}
	return bestFrequency
}

// hasMMSSTVFSKStartAfterGuard cheaply verifies the required 2100 Hz Guard ->
// 1900 Hz Start transition before a candidate is sent to the full timing fit.
// The ±20 ms search absorbs the coarse 1 ms grid and Guard-edge refinement
// uncertainty. Two consecutive confident Mark windows avoid accepting noise.
func hasMMSSTVFSKStartAfterGuard(samples []int16, guardStart, sampleRate int, markHz, spaceHz float64, sampleAt func(float64) int) bool {
	if sampleRate <= 0 || guardStart < 0 {
		return false
	}
	window := max(1, sampleAt(8))
	expected := guardStart + sampleAt(100)
	radius := sampleAt(20)
	step := max(1, sampleRate/500) // 2 ms
	consecutive := 0
	for center := expected - radius; center <= expected+radius; center += step {
		from := center - window/2
		to := from + window
		if from < 0 || to > len(samples) || to <= from {
			continue
		}
		tone, confidence := classifyFSKToneConfidence(samples[from:to], sampleRate, markHz, spaceHz)
		if tone == markHz && confidence >= math.Log(1.05) {
			consecutive++
			if consecutive >= 2 {
				return true
			}
		} else {
			consecutive = 0
		}
	}
	return false
}

// refineFSKSpaceTransition locates the 1900->2100 Hz guard edge by following
// the energy ratio of the already AFC-corrected mark and space tones. The
// search extends before the coarse window because the latter can straddle the
// transition by up to roughly 20 ms.
func refineFSKSpaceTransition(samples []int16, approximate, sampleRate int, markHz, spaceHz float64, sampleAt func(float64) int) int {
	if sampleRate <= 0 || approximate < 0 || approximate >= len(samples) {
		return approximate
	}
	// The initial 40 ms scan is deliberately coarse and can begin up to about
	// 20 ms before the actual guard. A plain mark/space ratio crossing is unsafe
	// here: during MMSSTV's 1500 Hz normal leader neither FSK bin has meaningful
	// energy, so noise/leakage can create a false crossing before the real 2100
	// Hz guard. Instead, locate the first stable window that is confidently the
	// space tone. This also works after the 1900 Hz narrow-band leader.
	window := max(1, sampleAt(8))
	searchFrom := max(0, approximate-sampleAt(25))
	searchTo := min(len(samples)-window, approximate+sampleAt(35))
	step := max(1, sampleRate/4_000) // 0.25 ms
	consecutive := 0
	firstSpaceCenter := 0
	for center := searchFrom + window/2; center <= searchTo+window/2; center += step {
		from := center - window/2
		to := from + window
		if from < 0 || to > len(samples) || to <= from {
			continue
		}
		tone, confidence := classifyFSKToneConfidence(samples[from:to], sampleRate, markHz, spaceHz)
		if tone == spaceHz && confidence >= math.Log(1.05) {
			if consecutive == 0 {
				firstSpaceCenter = center
			}
			consecutive++
			if consecutive >= 2 {
				// The first confidently space-dominant window usually centers
				// within one millisecond of the transition. The timing preamble
				// fit below performs the final sub-millisecond correction.
				return firstSpaceCenter
			}
		} else {
			consecutive = 0
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
	// Remove capture-chain DC bias before detecting crossings. A fixed
	// hysteresis threshold, relative to peak amplitude, prevents noise around
	// zero from being counted as additional cycles on weak receiver audio.
	var mean float64
	for _, sample := range samples {
		mean += float64(sample)
	}
	mean /= float64(len(samples))
	peak := 0.0
	for _, sample := range samples {
		if v := math.Abs(float64(sample) - mean); v > peak {
			peak = v
		}
	}
	if peak < 3 {
		return 0
	}
	threshold := math.Max(2, peak*0.12)
	lastNegative, firstPositive := -1, -1
	var previousCrossing, periodSum float64
	periodCount := 0
	for i, sample := range samples {
		v := float64(sample) - mean
		if v <= -threshold {
			lastNegative = i
			firstPositive = -1
			continue
		}
		if lastNegative < 0 || v <= 0 {
			continue
		}
		if firstPositive < 0 {
			firstPositive = i
		}
		if v >= threshold {
			left := float64(samples[lastNegative]) - mean
			right := float64(samples[firstPositive]) - mean
			position := float64(firstPositive)
			if right != left {
				position = float64(lastNegative) - left/(right-left)*float64(firstPositive-lastNegative)
			}
			if periodCount >= 0 && previousCrossing > 0 {
				period := position - previousCrossing
				if period >= float64(sampleRate)/3_000 && period <= float64(sampleRate)/1_000 {
					periodSum += period
					periodCount++
				}
			}
			previousCrossing = position
			lastNegative = -1
			firstPositive = -1
		}
	}
	if periodCount == 0 || periodSum <= 0 {
		return 0
	}
	return float64(sampleRate) * float64(periodCount) / periodSum
}

// classifyFSKTone returns the stronger of the shifted MMSSTV mark/space tones.
// Goertzel runs only two exact bins per symbol, keeping FSKID CPU use bounded.
func classifyFSKTone(samples []int16, sampleRate int, markHz, spaceHz float64) float64 {
	tone, _ := classifyFSKToneConfidence(samples, sampleRate, markHz, spaceHz)
	return tone
}

// goertzelPowerZeroMean avoids leakage from DC bias without allocating a
// temporary centered PCM buffer for each symbol decision.
func goertzelPowerZeroMean(samples []int16, sampleRate int, frequency, mean float64) float64 {
	if len(samples) < 2 || sampleRate <= 0 {
		return 0
	}
	omega := 2 * math.Pi * frequency / float64(sampleRate)
	coeff := 2 * math.Cos(omega)
	s1, s2 := 0.0, 0.0
	for _, sample := range samples {
		x := (float64(sample) - mean) / 32768
		s0 := x + coeff*s1 - s2
		s2, s1 = s1, s0
	}
	return s1*s1 + s2*s2 - coeff*s1*s2
}

func classifyFSKToneConfidence(samples []int16, sampleRate int, markHz, spaceHz float64) (float64, float64) {
	if len(samples) == 0 || sampleRate <= 0 || markHz <= 0 || spaceHz <= 0 || markHz >= float64(sampleRate)/2 || spaceHz >= float64(sampleRate)/2 {
		return 0, 0
	}
	// Center each decision window before measuring tone energy. Real capture
	// devices can carry a DC offset from the ADC, mixer or discriminator. If
	// that offset is included in the normalization (or Goertzel recurrence),
	// weak but valid FSK symbols can be rejected intermittently.
	mean := 0.0
	for _, sample := range samples {
		mean += float64(sample)
	}
	mean /= float64(len(samples))
	var energy float64
	for _, sample := range samples {
		x := (float64(sample) - mean) / 32768
		energy += x * x
	}
	if energy/float64(len(samples)) <= 1e-11 {
		return 0, 0
	}
	markPower := goertzelPowerZeroMean(samples, sampleRate, markHz, mean)
	spacePower := goertzelPowerZeroMean(samples, sampleRate, spaceHz, mean)
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
