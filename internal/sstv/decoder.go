package sstv

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

// DecodeResult contains one reconstructed SSTV frame and the detected mode.
type DecodeResult struct {
	Image     *image.NRGBA
	Mode      ModeSpec
	VIS       uint8
	BodyStart int
}

// DetectVIS searches the beginning of an audio buffer for a standard 7-bit
// VIS header. It returns the code and the sample offset immediately after the
// 910 ms header. Input is signed mono PCM.
func DetectVIS(samples []int16, sampleRate int) (uint8, int, error) {
	freq := instantaneousFrequency(samples, sampleRate)
	code, bodyStart, _, err := detectVISFrom(samples, freq, sampleRate, 0)
	return code, bodyStart, err
}

// detectVISFrom searches only start offsets at or after startAt. nextStart is
// the first aligned offset that could not yet be checked because a complete
// 910 ms header was unavailable, or the next offset after this search window.
// StreamDecoder uses it to avoid rescanning the same idle audio on every feed.
func detectVISFrom(samples []int16, freq []float64, sampleRate, startAt int) (code uint8, bodyStart, nextStart int, retErr error) {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return 0, 0, startAt, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	step := max(1, sampleRate/500) // Coarse search every 2 ms.
	leaderN := int(math.Round(0.300 * float64(sampleRate)))
	headerN := int(math.Round(0.910 * float64(sampleRate)))
	if len(samples) < headerN || len(freq) < headerN {
		return 0, 0, startAt, errors.New("audio is too short to contain a VIS header")
	}
	maxStart := min(len(samples)-headerN, sampleRate*3)
	start := max(0, startAt)
	if remainder := start % step; remainder != 0 {
		start += step - remainder
	}

	fineStep := max(1, sampleRate/4000)
	for coarse := start; coarse <= maxStart; coarse += step {
		leaderFreq := meanFreqQuick(freq, coarse, leaderN, max(1, sampleRate/8_000))
		// SSB tuning can move the entire audio passband by several hundred
		// hertz. A fixed-mode receiver already accepted this broader AFC range;
		// Auto must search the same practical window before rejecting VIS.
		if leaderFreq < 1_550 || leaderFreq > 2_250 {
			continue
		}
		offset := leaderFreq - 1_900
		low := max(startAt, coarse-step)
		if low < 0 {
			low = 0
		}
		high := min(maxStart, coarse+step)
		for candidate := low; candidate <= high; candidate += fineStep {
			// The discriminator track is useful for image pixels, but real radio
			// audio can contain noise, multipath and receiver filtering that move
			// individual zero crossings. Confirm VIS against narrowband energy in
			// the original PCM first, retaining the discriminator as a fallback.
			breakStart := candidate + leaderN
			breakN := int(math.Round(0.010 * float64(sampleRate)))
			breakEnd := breakStart + breakN
			if breakEnd > len(samples) {
				continue
			}
			breakMean := meanFreqQuick(freq, breakStart, breakN, max(1, sampleRate/8_000))
			if breakMean < 1_100+offset || breakMean > 1_300+offset {
				continue
			}
			if bestTone(samples[breakStart:breakEnd], sampleRate, 1_200+offset, 1_900+offset) != 1_200+offset {
				continue
			}
			if decoded, after, ok := decodeVISHeaderAtPCM(samples, sampleRate, candidate, offset); ok {
				return decoded, after, candidate, nil
			}
			// Several SSTV transmitters and real audio paths occasionally damage
			// only the VIS parity symbol. The seven data bits still identify the
			// mode, so accept a structurally valid header when every data symbol,
			// both leaders, start and stop symbols are clear but parity alone fails.
			if decoded, after, ok := decodeVISHeaderAtPCMRelaxed(samples, sampleRate, candidate, offset); ok {
				return decoded, after, candidate, nil
			}
			if decoded, after, ok := decodeVISHeaderAtFreq(freq, sampleRate, candidate, offset); ok {
				return decoded, after, candidate, nil
			}
		}
	}
	return 0, 0, maxStart + 1, errors.New("standard SSTV VIS header not detected")
}

// decodeVISHeaderAtPCM validates a standard 7-bit VIS header using narrowband
// energy measured directly from captured PCM. This is separate from the image
// discriminator because noise, multipath and receiver filtering can move
// individual zero crossings while leaving the FSK symbols detectable.
func decodeVISHeaderAtPCM(samples []int16, sampleRate, start int, offset float64) (uint8, int, bool) {
	return decodeVISHeaderAtPCMWithParity(samples, sampleRate, start, offset, true)
}

func decodeVISHeaderAtPCMRelaxed(samples []int16, sampleRate, start int, offset float64) (uint8, int, bool) {
	return decodeVISHeaderAtPCMWithParity(samples, sampleRate, start, offset, false)
}

func decodeVISHeaderAtPCMWithParity(samples []int16, sampleRate, start int, offset float64, requireParity bool) (uint8, int, bool) {
	if sampleRate <= 0 {
		return 0, 0, false
	}
	leaderN := int(math.Round(0.300 * float64(sampleRate)))
	breakN := int(math.Round(0.010 * float64(sampleRate)))
	bitN := int(math.Round(0.030 * float64(sampleRate)))
	headerN := leaderN + breakN + leaderN + 10*bitN
	if start < 0 || start+headerN > len(samples) {
		return 0, 0, false
	}
	p := start
	classify := func(n int, centers ...float64) float64 {
		if p+n > len(samples) {
			return 0
		}
		tone := headerTone(samples[p:p+n], sampleRate, centers...)
		p += n
		return tone
	}
	if classify(leaderN, 1_900+offset) != 1_900+offset ||
		classify(breakN, 1_200+offset) != 1_200+offset ||
		classify(leaderN, 1_900+offset) != 1_900+offset ||
		classify(bitN, 1_200+offset) != 1_200+offset {
		return 0, 0, false
	}
	var decoded uint8
	ones := 0
	for bit := 0; bit < 7; bit++ {
		tone := classify(bitN, 1_100+offset, 1_300+offset)
		switch tone {
		case 1_100 + offset:
			decoded |= 1 << bit
			ones++
		case 1_300 + offset:
		default:
			return 0, 0, false
		}
	}
	if requireParity {
		parityTone := classify(bitN, 1_100+offset, 1_300+offset)
		if parityTone == 1_100+offset {
			ones++
		} else if parityTone != 1_300+offset {
			return 0, 0, false
		}
		if ones%2 != 0 {
			return 0, 0, false
		}
	} else {
		p += bitN // parity is deliberately ignored; the seven data bits remain strict
	}
	if classify(bitN, 1_200+offset) != 1_200+offset {
		return 0, 0, false
	}
	return decoded, start + headerN, true
}

// decodeVISHeaderAtFreq reads a standard 910 ms VIS header from an already
// demodulated frequency track, compensating the receiver's offset (AFC).
func decodeVISHeaderAtFreq(freq []float64, sampleRate, start int, offset float64) (uint8, int, bool) {
	leaderN := int(math.Round(0.300 * float64(sampleRate)))
	breakN := int(math.Round(0.010 * float64(sampleRate)))
	bitN := int(math.Round(0.030 * float64(sampleRate)))
	headerN := leaderN + breakN + leaderN + 10*bitN
	if start < 0 || start+headerN > len(freq) {
		return 0, 0, false
	}
	p := start
	if !toneNear(freq[p:p+leaderN], 1_900+offset, 100) {
		return 0, 0, false
	}
	p += leaderN
	if !toneNear(freq[p:p+breakN], 1_200+offset, 130) {
		return 0, 0, false
	}
	p += breakN
	if !toneNear(freq[p:p+leaderN], 1_900+offset, 100) {
		return 0, 0, false
	}
	p += leaderN
	if !toneNear(freq[p:p+bitN], 1_200+offset, 100) {
		return 0, 0, false
	}
	p += bitN
	var decoded uint8
	ones := 0
	mid := 1_200 + offset
	for bit := 0; bit < 7; bit++ {
		f := meanFreq(freq, p, bitN)
		p += bitN
		if f < 1_000+offset || f > 1_400+offset {
			return 0, 0, false
		}
		if f <= mid {
			decoded |= 1 << bit
			ones++
		}
	}
	parityOne := meanFreq(freq, p, bitN) <= mid
	p += bitN
	if (ones+boolInt(parityOne))%2 != 0 {
		return 0, 0, false
	}
	if !toneNear(freq[p:p+bitN], 1_200+offset, 100) {
		return 0, 0, false
	}
	return decoded, start + headerN, true
}

// meanFreq averages the demodulated frequency over [start, start+n), skipping
// out-of-band samples that carry no tone information.
func meanFreq(freq []float64, start, n int) float64 {
	end := min(start+n, len(freq))
	sum, count := 0.0, 0
	for i := start; i < end; i++ {
		if freq[i] > 500 && freq[i] < 3_500 {
			sum += freq[i]
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// meanFreqQuick is a coarse candidate gate for long idle-search windows. The
// discriminator output changes on millisecond-scale SSTV symbols, so sampling
// it at 6-8 kHz preserves leader classification while avoiding a full 300 ms
// scan for every 2 ms candidate offset. Exact header checks still use the full
// frequency track once a plausible 1900 Hz leader is found.
func meanFreqQuick(freq []float64, start, n, stride int) float64 {
	if stride < 1 {
		stride = 1
	}
	if start < 0 {
		start = 0
	}
	end := min(start+n, len(freq))
	sum := 0.0
	count := 0
	for i := start; i < end; i += stride {
		if f := freq[i]; f > 500 && f < 3_500 {
			sum += f
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// toneNear reports whether a segment's average frequency is within tolerance
// of the expected tone.
func toneNear(seg []float64, expected, tolerance float64) bool {
	sum, count := 0.0, 0
	for _, f := range seg {
		if f > 500 && f < 3_500 {
			sum += f
			count++
		}
	}
	if count == 0 {
		return false
	}
	return math.Abs(sum/float64(count)-expected) <= tolerance
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func bestTone(samples []int16, sampleRate int, candidates ...float64) float64 {
	if len(samples) == 0 || sampleRate <= 0 || len(candidates) == 0 {
		return 0
	}
	best, bestPower := 0.0, -1.0
	var energy float64
	for candidateIndex, frequency := range candidates {
		omega := 2 * math.Pi * frequency / float64(sampleRate)
		coeff := 2 * math.Cos(omega)
		s1, s2 := 0.0, 0.0
		for _, sample := range samples {
			x := float64(sample) / 32768
			if candidateIndex == 0 {
				energy += x * x
			}
			s0 := x + coeff*s1 - s2
			s2, s1 = s1, s0
		}
		power := s1*s1 + s2*s2 - coeff*s1*s2
		if power > bestPower {
			best, bestPower = frequency, power
		}
	}
	// Goertzel returns equal zero power for every candidate on digital
	// silence. Without an energy gate that tie is resolved in favour of the
	// first frequency and silence can look exactly like a valid VIS header.
	// 1e-12 mean-square is -120 dBFS, below any meaningful 16-bit capture.
	if energy/float64(len(samples)) <= 1e-12 {
		return 0
	}
	return best
}

// headerTone classifies SSTV header symbols while allowing a modest receiver
// frequency offset. Each expected tone is searched over a small band; a
// confidence margin rejects ambiguous/noisy symbols instead of guessing.
func headerTone(samples []int16, sampleRate int, centers ...float64) float64 {
	if len(samples) == 0 || sampleRate <= 0 || len(centers) == 0 {
		return 0
	}
	var energy float64
	for _, sample := range samples {
		x := float64(sample) / 32768
		energy += x * x
	}
	if energy/float64(len(samples)) <= 1e-11 {
		return 0
	}
	bestCenter, bestPower, secondPower := 0.0, -1.0, -1.0
	// Use finer frequency bins for long leaders and wider bins for short
	// symbols whose FFT-equivalent resolution is inherently coarser.
	frequencyStep := math.Max(5, math.Min(25, float64(sampleRate)/float64(len(samples))*0.75))
	for _, center := range centers {
		centerPower := 0.0
		for offset := -90.0; offset <= 90.0; offset += frequencyStep {
			f := center + offset
			if f <= 0 || f >= float64(sampleRate)/2 {
				continue
			}
			power := goertzelPower(samples, sampleRate, f)
			if power > centerPower {
				centerPower = power
			}
		}
		if centerPower > bestPower {
			secondPower = bestPower
			bestPower = centerPower
			bestCenter = center
		} else if centerPower > secondPower {
			secondPower = centerPower
		}
	}
	// Goertzel power is normalized against segment energy. A clean tone has a
	// much stronger narrowband peak than broadband speech/noise; reject weak
	// peaks even when one candidate happens to win by a small margin. The
	// thresholds are deliberately permissive: a false-positive header is caught
	// later by the symbol-by-symbol parity check, while a marginally weak but
	// genuine tone must still be accepted on noisy or mistuned receivers.
	if bestPower <= 0 || bestPower/(energy*float64(len(samples))) < 0.025 || (secondPower > 0 && bestPower/secondPower < 1.025) {
		return 0
	}
	return bestCenter
}

func goertzelPower(samples []int16, sampleRate int, frequency float64) float64 {
	omega := 2 * math.Pi * frequency / float64(sampleRate)
	coeff := 2 * math.Cos(omega)
	s1, s2 := 0.0, 0.0
	for _, sample := range samples {
		x := float64(sample) / 32768
		s0 := x + coeff*s1 - s2
		s2, s1 = s1, s0
	}
	return s1*s1 + s2*s2 - coeff*s1*s2
}

// frameOffset measures the receiver's frequency offset from the VIS leader so
// mistuned SSB audio maps back onto the standard video band. It reads the
// leader's average frequency from an already demodulated track (zero cost) and
// returns the offset in hertz, or zero when no clean leader is available.
func frameOffset(freq []float64, sampleRate int, spec ModeSpec, bodyStart int) float64 {
	headerMS := 910.0
	switch spec.Header {
	case HeaderMMSSTV:
		headerMS = 1_150
	case HeaderNarrowVIS:
		headerMS = 950
	case HeaderAVT:
		return 0
	}
	leaderN := int(math.Round(0.300 * float64(sampleRate)))
	leaderStart := bodyStart - int(math.Round(headerMS/1000*float64(sampleRate)))
	if leaderStart < 0 || leaderStart+leaderN > len(freq) {
		return 0
	}
	f := meanFreq(freq, leaderStart, leaderN)
	if f < 1_700 || f > 2_100 {
		return 0
	}
	return f - 1_900
}

// Decode performs standard VIS detection followed by mode-specific image
// reconstruction. It is a whole-frame reference decoder; the live RX worker
// can use the same per-line demodulation functions incrementally.
func Decode(ctx context.Context, samples []int16, sampleRate int) (*DecodeResult, error) {
	spec, vis, bodyStart, err := DetectModeHeader(samples, sampleRate)
	if err != nil {
		return nil, err
	}
	img, err := DecodeMode(ctx, samples, sampleRate, spec.ID, bodyStart)
	if err != nil {
		return nil, err
	}
	return &DecodeResult{Image: img, Mode: spec, VIS: vis, BodyStart: bodyStart}, nil
}

// DecodeMode decodes a known mode from a body offset. bodyStart is measured in
// samples and should point immediately after the VIS stop bit. Pass 0 to use
// the standard 910 ms VIS header offset.
func DecodeMode(ctx context.Context, samples []int16, sampleRate int, modeID string, bodyStart int) (*image.NRGBA, error) {
	spec, err := LookupMode(modeID)
	if err != nil {
		return nil, err
	}
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	if bodyStart == 0 {
		headerMS := 910.0
		switch spec.Header {
		case HeaderMMSSTV:
			headerMS = 1_150
		case HeaderNarrowVIS:
			headerMS = 950
		case HeaderAVT:
			headerMS = avt90HeaderMS
		}
		bodyStart = int(math.Round(headerMS / 1000 * float64(sampleRate)))
	}
	if bodyStart < 0 || bodyStart >= len(samples) {
		return nil, errors.New("invalid SSTV image body offset")
	}
	freq := instantaneousFrequency(samples, sampleRate)
	offset := frameOffset(freq, sampleRate, spec, bodyStart)
	return decodeModeFromFrequency(ctx, freq, sampleRate, spec, bodyStart, samples, offset)
}

// decodeModeFromFrequency reconstructs the raster from an already demodulated
// frequency track. StreamDecoder uses it to refresh a live preview without
// re-running the PLL over the audio received so far.
func decodeModeFromFrequency(ctx context.Context, freq []float64, sampleRate int, spec ModeSpec, bodyStart int, pcm []int16, offset float64) (*image.NRGBA, error) {
	return decodeModeRowsFromFrequency(ctx, freq, sampleRate, spec, bodyStart, pcm, offset, spec.Height)
}

// decodeModeRowsFromFrequency decodes only the requested leading raster rows,
// while keeping the returned canvas at the complete mode resolution. This is
// used by live RX previews: unreceived rows remain transparent instead of
// being filled with a fabricated centre-frequency grey image. The final frame
// still goes through decodeModeFromFrequency with the full row count and PCM
// sync recovery.
func decodeModeRowsFromFrequency(ctx context.Context, freq []float64, sampleRate int, spec ModeSpec, bodyStart int, pcm []int16, offset float64, rows int) (*image.NRGBA, error) {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	if bodyStart < 0 || bodyStart >= len(freq) {
		return nil, errors.New("invalid SSTV image body offset")
	}
	fullHeight := spec.Height
	if fullHeight <= 0 {
		return nil, errors.New("invalid SSTV image height")
	}
	rows = max(0, min(rows, fullHeight))
	if spec.Family == FamilyPD || spec.Family == FamilyMP ||
		(spec.Family == FamilyRobot && !spec.Monochrome && !spec.RobotSequential) {
		rows -= rows % 2
	}
	if rows <= 0 {
		return image.NewNRGBA(image.Rect(0, 0, spec.Width, fullHeight)), nil
	}

	// BodyDurationMS uses Height for all supported protocol families. Compute
	// the required sample tail for just the preview rows rather than padding
	// and decoding the entire future raster on every preview refresh.
	previewSpec := spec
	previewSpec.Height = rows
	bodySamples := int(math.Round(BodyDurationMS(previewSpec) * float64(sampleRate) / 1000))
	tail := sampleRate / 20 // 50 ms of slack for nominal preview timing.
	if need := bodyStart + bodySamples + tail; need > len(freq) {
		pad := need - len(freq)
		freq = append(freq, make([]float64, pad)...)
		for i := len(freq) - pad; i < len(freq); i++ {
			freq[i] = VideoCenterHz
		}
		if pcm != nil {
			pcm = append(pcm, make([]int16, pad)...)
		}
	}
	clock := sampleClock{rate: sampleRate, pos: bodyStart, samples: pcm, offset: offset}
	img := image.NewNRGBA(image.Rect(0, 0, spec.Width, fullHeight))

	switch spec.Family {
	case FamilyRobot:
		if err := decodeRobot(ctx, freq, &clock, img, previewSpec); err != nil {
			return nil, err
		}
	case FamilyMartin, FamilyScottie, FamilyWraase, FamilyPasokon:
		if err := decodeRGB(ctx, freq, &clock, img, previewSpec); err != nil {
			return nil, err
		}
	case FamilyPD:
		if err := decodePD(ctx, freq, &clock, img, previewSpec); err != nil {
			return nil, err
		}
	case FamilyMP:
		if err := decodeMP(ctx, freq, &clock, img, previewSpec); err != nil {
			return nil, err
		}
	case FamilyMRML:
		if err := decodeMRML(ctx, freq, &clock, img, previewSpec); err != nil {
			return nil, err
		}
	case FamilyMCN, FamilyAVT:
		if err := decodeRGBProfile(ctx, freq, &clock, img, previewSpec); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("decoder for family %q is not implemented", spec.Family)
	}
	return img, nil
}

type sampleClock struct {
	rate       int
	pos        int
	fractional float64
	samples    []int16
	syncSeen   bool
	offset     float64
}

func (c *sampleClock) skip(ms float64) {
	exact := ms/1000*float64(c.rate) + c.fractional
	n := int(math.Round(exact))
	c.fractional = exact - float64(n)
	c.pos += n
}

// sync searches around the predicted line boundary for the actual 1200 Hz
// horizontal-sync pulse. The first sync for most modes is adjacent to the VIS
// stop bit, which is also 1200 Hz; its boundary is therefore kept at the
// accurately detected VIS body offset. Scottie has its first sync mid-line and
// calls syncFirst so that pulse is measured as well.
func (c *sampleClock) sync(durationMS, searchMS float64) bool {
	return c.syncInternal(durationMS, searchMS, false)
}

func (c *sampleClock) syncFirst(durationMS, searchMS float64) bool {
	return c.syncInternal(durationMS, searchMS, true)
}

func (c *sampleClock) syncInternal(durationMS, searchMS float64, detectFirst bool) bool {
	if c == nil || c.rate <= 0 || durationMS <= 0 {
		return false
	}
	if !c.syncSeen {
		c.syncSeen = true
		if !detectFirst {
			c.skip(durationMS)
			return false
		}
	}
	if len(c.samples) == 0 {
		c.skip(durationMS)
		return false
	}

	pulseSamples := int(math.Round(durationMS / 1000 * float64(c.rate)))
	if pulseSamples <= 0 || pulseSamples > len(c.samples) {
		c.skip(durationMS)
		return false
	}
	expected := c.pos
	searchSamples := int(math.Round(math.Max(0, searchMS) / 1000 * float64(c.rate)))
	minStart := max(0, expected-searchSamples)
	maxStart := min(len(c.samples)-pulseSamples, expected+searchSamples)
	if maxStart < minStart {
		c.skip(durationMS)
		return false
	}

	// Decimate the coarse pass to 12 kHz, safely above twice the 1200 Hz
	// sync tone. A full-rate local refinement then recovers the exact edge.
	scoreAt := func(start, sampleStride int) float64 {
		effectiveRate := float64(c.rate) / float64(sampleStride)
		omega := 2 * math.Pi * (HorizontalSync + c.offset) / effectiveRate
		coeff := 2 * math.Cos(omega)
		s1, s2, energy, count := 0.0, 0.0, 0.0, 0
		for index := start; index < start+pulseSamples; index += sampleStride {
			x := float64(c.samples[index]) / 32768
			energy += x * x
			s0 := x + coeff*s1 - s2
			s2, s1 = s1, s0
			count++
		}
		if count == 0 || energy/float64(count) <= 1e-12 {
			return 0
		}
		power := s1*s1 + s2*s2 - coeff*s1*s2
		if power < 0 {
			power = 0
		}
		return power / (energy * float64(count))
	}

	absDistance := func(a, b int) int {
		if a >= b {
			return a - b
		}
		return b - a
	}
	bestStart := expected
	bestScore := -1.0
	bestDistance := int(^uint(0) >> 1)
	consider := func(start, sampleStride int) {
		score := scoreAt(start, sampleStride)
		distance := absDistance(start, expected)
		if score > bestScore+1e-9 || (math.Abs(score-bestScore) <= 1e-9 && distance < bestDistance) {
			bestScore = score
			bestStart = start
			bestDistance = distance
		}
	}

	coarseStride := max(1, c.rate/12_000)
	coarseStep := max(1, c.rate/3_000) // about 0.33 ms between candidates
	for start := minStart; start <= maxStart; start += coarseStep {
		consider(start, coarseStride)
	}
	coarseBestStart := bestStart
	// Reset the score because the refinement uses a different decimation rate.
	bestScore = -1
	bestDistance = int(^uint(0) >> 1)
	fineStride := max(1, c.rate/48_000)
	refineStart := max(minStart, coarseBestStart-coarseStep)
	refineEnd := min(maxStart, coarseBestStart+coarseStep)
	for start := refineStart; start <= refineEnd; start += fineStride {
		consider(start, fineStride)
	}
	// Final sample-level refinement also handles sample rates above 48 kHz.
	sampleStart := max(minStart, bestStart-fineStride)
	sampleEnd := min(maxStart, bestStart+fineStride)
	for start := sampleStart; start <= sampleEnd; start++ {
		consider(start, fineStride)
	}

	// A clean sinusoidal sync pulse has a normalized score near 0.5;
	// reject broadband noise and ordinary 1500..2300 Hz picture tones.
	if bestScore < 0.08 {
		c.skip(durationMS)
		return false
	}
	c.pos = bestStart + pulseSamples
	c.fractional = 0
	return true
}

func (c *sampleClock) scan(freq []float64, width int, totalMS float64) ([]uint8, error) {
	return c.scanRange(freq, width, totalMS, VideoMinHz, VideoMaxHz)
}

func (c *sampleClock) scanMode(freq []float64, width int, totalMS float64, spec ModeSpec) ([]uint8, error) {
	lo, hi := videoRange(spec)
	return c.scanRange(freq, width, totalMS, lo, hi)
}

func (c *sampleClock) scanRange(freq []float64, width int, totalMS, minHz, maxHz float64) ([]uint8, error) {
	if width <= 0 {
		return nil, errors.New("invalid scan width")
	}
	out := make([]uint8, width)
	pixelSeconds := totalMS / 1000 / float64(width)
	for x := 0; x < width; x++ {
		exact := pixelSeconds*float64(c.rate) + c.fractional
		n := int(math.Round(exact))
		c.fractional = exact - float64(n)
		start, end := c.pos, c.pos+n
		c.pos = end
		if start < 0 || end > len(freq) || n <= 0 {
			return nil, errors.New("SSTV audio ended before the image scan completed")
		}
		lo, hi := start+n/4, end-n/4
		if hi <= lo {
			lo, hi = start, end
		}
		sum, count := 0.0, 0
		for i := lo; i < hi; i++ {
			if freq[i] > 500 && freq[i] < 3_500 {
				sum += freq[i]
				count++
			}
		}
		if count == 0 {
			return nil, fmt.Errorf("unable to estimate SSTV pixel frequency at pixel %d (samples %d..%d)", x, start, end)
		}
		out[x] = frequencyToByteRange(sum/float64(count)-c.offset, minHz, maxHz)
	}
	return out, nil
}

func frequencyToByteRange(frequency, minHz, maxHz float64) uint8 {
	v := (frequency - minHz) * 255 / (maxHz - minHz)
	return uint8(math.Max(0, math.Min(255, math.Round(v))))
}

func frequencyToByte(frequency float64) uint8 {
	v := (frequency - VideoMinHz) * 255 / (VideoMaxHz - VideoMinHz)
	return uint8(math.Max(0, math.Min(255, math.Round(v))))
}

// biquad is a second-order IIR filter (RBJ audio-EQ cookbook).
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

func (b *biquad) Do(x float64) float64 {
	y := b.b0*x + b.b1*b.x1 + b.b2*b.x2 - b.a1*b.y1 - b.a2*b.y2
	b.x2, b.x1 = b.x1, x
	b.y2, b.y1 = b.y1, y
	return y
}

func biquadHighPass(fc, fs float64) biquad {
	w0 := 2 * math.Pi * fc / fs
	alpha := math.Sin(w0) / (2 * 0.707)
	c := math.Cos(w0)
	a0 := 1 + alpha
	return biquad{
		b0: (1 + c) / 2 / a0,
		b1: -(1 + c) / a0,
		b2: (1 + c) / 2 / a0,
		a1: -2 * c / a0,
		a2: (1 - alpha) / a0,
	}
}

func biquadLowPass(fc, fs float64) biquad {
	w0 := 2 * math.Pi * fc / fs
	alpha := math.Sin(w0) / (2 * 0.707)
	c := math.Cos(w0)
	a0 := 1 + alpha
	return biquad{
		b0: (1 - c) / 2 / a0,
		b1: (1 - c) / a0,
		b2: (1 - c) / 2 / a0,
		a1: -2 * c / a0,
		a2: (1 - alpha) / a0,
	}
}

// bandPass is a flat ~900..2800 Hz band-pass (high-pass + low-pass cascade)
// that removes DC offset, power-line hum and out-of-band noise without
// attenuating the closely-spaced VIS data tones. This mirrors MMSSTV's input
// band-pass and raises the in-band signal-to-noise ratio for weak recordings.
type bandPass struct {
	hp, lp biquad
}

func newBandPass(fs float64) bandPass {
	return bandPass{hp: biquadHighPass(900, fs), lp: biquadLowPass(2800, fs)}
}

func (b *bandPass) Do(x float64) float64 {
	return b.lp.Do(b.hp.Do(x))
}

// agc normalizes a signal's peak to a target level using a slow peak tracker,
// matching MMSSTV's CLVL automatic gain control. A weak input is amplified so
// the zero-crossing counter and spectrum see a consistent amplitude instead of
// being buried in noise.
type agc struct {
	gain  float64
	peak  float64
	count int
	win   int
}

func newAGC(sampleRate int) *agc {
	return &agc{gain: 1.0, win: sampleRate / 10} // 100 ms window
}

func (a *agc) Do(x float64) float64 {
	if ax := math.Abs(x); ax > a.peak {
		a.peak = ax
	}
	a.count++
	if a.count >= a.win {
		if a.peak > 1e-4 {
			target := 0.7 / a.peak
			a.gain += 0.3 * (target - a.gain)
			if a.gain > 200 {
				a.gain = 200
			}
		}
		a.peak = 0
		a.count = 0
	}
	return x * a.gain
}

// instantaneousFrequency demodulates the SSTV audio into a per-sample frequency
// track using a zero-crossing counter with linear interpolation, matching the
// MMSSTV CFQC detector. A DC blocker removes offset/hum and an AGC normalizes
// the level before the counter runs.
type frequencyTracker struct {
	rate       int
	prev       float64
	sinceCross float64
	freq       float64
	bp         bandPass
	agc        agc
}

func newFrequencyTracker(sampleRate int) *frequencyTracker {
	return &frequencyTracker{
		rate: sampleRate,
		freq: 0, // no tone yet
		bp:   newBandPass(float64(sampleRate)),
		agc:  *newAGC(sampleRate),
	}
}

func (t *frequencyTracker) Process(samples []int16) []float64 {
	if t == nil || t.rate <= 0 || len(samples) == 0 {
		return nil
	}
	freq := make([]float64, len(samples))
	for n, sample := range samples {
		x := float64(sample) / 32768
		x = t.agc.Do(t.bp.Do(x))
		t.sinceCross++
		if (x >= 0) != (t.prev >= 0) {
			// Linear-interpolate the exact zero-crossing position and use the
			// half-cycle period to derive the instantaneous frequency.
			frac := x / (x - t.prev)
			period := t.sinceCross - frac
			t.sinceCross = frac
			// Half-cycle intervals may legitimately be below two samples for
			// SSTV tones above sampleRate/4. Requiring >=2 silently pinned the
			// discriminator near 2 kHz on 8 kHz devices. Linear interpolation
			// supports any positive interval of at least one sample.
			if period >= 1 && period <= float64(t.rate)/500 {
				if f := float64(t.rate) * 0.5 / period; f >= 500 && f <= 3_500 {
					t.freq = f
				}
			}
		}
		// No crossing for a long interval means silence or DC: report "no tone"
		// rather than holding a stale frequency (which would make silence look
		// like a 1900 Hz tone).
		if t.sinceCross > float64(t.rate)/500 {
			t.freq = 0
			t.sinceCross = 0
		}
		freq[n] = t.freq
		t.prev = x
	}
	return freq
}

// instantaneousFrequency is the whole-buffer convenience wrapper around the
// stateful zero-crossing discriminator used by the live stream decoder.
func instantaneousFrequency(samples []int16, sampleRate int) []float64 {
	return newFrequencyTracker(sampleRate).Process(samples)
}

func decodeRobot(ctx context.Context, freq []float64, clock *sampleClock, img *image.NRGBA, spec ModeSpec) error {
	if spec.Monochrome {
		for row := 0; row < spec.Height; row++ {
			if err := ctxErr(ctx); err != nil {
				return err
			}
			clock.sync(spec.SyncMS, 12)
			luma, err := clock.scanMode(freq, spec.Width, spec.ScanMS, spec)
			if err != nil {
				return err
			}
			for x, value := range luma {
				img.SetNRGBA(x, row, color.NRGBA{R: value, G: value, B: value, A: 255})
			}
		}
		return nil
	}
	if spec.RobotSequential {
		for row := 0; row < spec.Height; row++ {
			if err := ctxErr(ctx); err != nil {
				return err
			}
			clock.sync(spec.SyncMS, 12)
			clock.skip(spec.SyncPorchMS)
			y, err := clock.scanMode(freq, spec.Width, spec.LumaMS, spec)
			if err != nil {
				return err
			}
			clock.skip(spec.SeparatorMS + spec.ColorPorchMS)
			cr, err := clock.scanMode(freq, spec.Width, spec.ChromaMS, spec)
			if err != nil {
				return err
			}
			clock.skip(spec.SeparatorMS + spec.ColorPorchMS)
			cb, err := clock.scanMode(freq, spec.Width, spec.ChromaMS, spec)
			if err != nil {
				return err
			}
			for x := 0; x < spec.Width; x++ {
				r, g, b := color.YCbCrToRGB(y[x], cb[x], cr[x])
				img.SetNRGBA(x, row, color.NRGBA{R: r, G: g, B: b, A: 255})
			}
		}
		return nil
	}

	yRows := make([][]uint8, spec.Height)
	chroma := make([][]uint8, spec.Height)
	for row := 0; row < spec.Height; row++ {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		clock.sync(spec.SyncMS, 12)
		clock.skip(spec.SyncPorchMS)
		y, err := clock.scanMode(freq, spec.Width, spec.LumaMS, spec)
		if err != nil {
			return err
		}
		yRows[row] = y
		clock.skip(spec.SeparatorMS + spec.ColorPorchMS)
		c, err := clock.scanMode(freq, spec.Width, spec.ChromaMS, spec)
		if err != nil {
			return err
		}
		chroma[row] = c
	}
	for row := 0; row < spec.Height; row += 2 {
		if row+1 >= spec.Height {
			break
		}
		cr, cb := chroma[row], chroma[row+1]
		for x := 0; x < spec.Width; x++ {
			r, g, b := color.YCbCrToRGB(yRows[row][x], cb[x], cr[x])
			img.SetNRGBA(x, row, color.NRGBA{R: r, G: g, B: b, A: 255})
			r, g, b = color.YCbCrToRGB(yRows[row+1][x], cb[x], cr[x])
			img.SetNRGBA(x, row+1, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return nil
}

func decodeRGB(ctx context.Context, freq []float64, clock *sampleClock, img *image.NRGBA, spec ModeSpec) error {
	for row := 0; row < spec.Height; row++ {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		values := map[byte][]uint8{'R': nil, 'G': nil, 'B': nil}
		scan := func(channel byte, duration float64) error {
			if duration <= 0 {
				duration = spec.ScanMS
			}
			v, err := clock.scan(freq, spec.Width, duration)
			if err == nil {
				values[channel] = v
			}
			return err
		}
		porch := func() { clock.skip(spec.PorchMS) }
		switch spec.Family {
		case FamilyMartin:
			clock.sync(spec.SyncMS, 12)
			porch()
			for _, channel := range []byte("GBR") {
				if err := scan(channel, spec.ScanMS); err != nil {
					return err
				}
				porch()
			}
		case FamilyScottie:
			porch()
			if err := scan('G', spec.ScanMS); err != nil {
				return err
			}
			porch()
			if err := scan('B', spec.ScanMS); err != nil {
				return err
			}
			clock.syncFirst(spec.SyncMS, 12)
			porch()
			if err := scan('R', spec.ScanMS); err != nil {
				return err
			}
		case FamilyWraase:
			clock.sync(spec.SyncMS, 12)
			for i, channel := range []byte("RGB") {
				if i == 0 || spec.PorchEachChannel {
					porch()
				}
				duration := spec.RGBScanMS[i]
				if err := scan(channel, duration); err != nil {
					return err
				}
			}
			if spec.PorchEachChannel {
				porch()
			}
		case FamilyPasokon:
			clock.sync(spec.SyncMS, 12)
			for _, channel := range []byte("RGB") {
				porch()
				if err := scan(channel, spec.ScanMS); err != nil {
					return err
				}
			}
			porch()
		}
		for x := 0; x < spec.Width; x++ {
			img.SetNRGBA(x, row, color.NRGBA{R: values['R'][x], G: values['G'][x], B: values['B'][x], A: 255})
		}
	}
	return nil
}

func decodePD(ctx context.Context, freq []float64, clock *sampleClock, img *image.NRGBA, spec ModeSpec) error {
	for row := 0; row < spec.Height; row += 2 {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		clock.sync(spec.SyncMS, 12)
		clock.skip(spec.PorchMS)
		y0, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		cr, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		cb, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		y1, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		for x := 0; x < spec.Width; x++ {
			r, g, b := color.YCbCrToRGB(y0[x], cb[x], cr[x])
			img.SetNRGBA(x, row, color.NRGBA{R: r, G: g, B: b, A: 255})
			r, g, b = color.YCbCrToRGB(y1[x], cb[x], cr[x])
			img.SetNRGBA(x, row+1, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return nil
}

func decodeMP(ctx context.Context, freq []float64, clock *sampleClock, img *image.NRGBA, spec ModeSpec) error {
	for row := 0; row < spec.Height; row += 2 {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		clock.sync(spec.SyncMS, 12)
		clock.skip(spec.PorchMS)
		y0, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		cr, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		cb, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		y1, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		for x := 0; x < spec.Width; x++ {
			r, g, b := color.YCbCrToRGB(y0[x], cb[x], cr[x])
			img.SetNRGBA(x, row, color.NRGBA{R: r, G: g, B: b, A: 255})
			r, g, b = color.YCbCrToRGB(y1[x], cb[x], cr[x])
			img.SetNRGBA(x, row+1, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return nil
}

func decodeMRML(ctx context.Context, freq []float64, clock *sampleClock, img *image.NRGBA, spec ModeSpec) error {
	for row := 0; row < spec.Height; row++ {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		clock.sync(spec.SyncMS, 12)
		clock.skip(spec.PorchMS)
		y, err := clock.scanMode(freq, spec.Width, spec.ChannelScanMS, spec)
		if err != nil {
			return err
		}
		clock.skip(spec.PixelHoldMS)
		cr, err := clock.scanMode(freq, spec.Width/2, spec.ChannelScanMS/2, spec)
		if err != nil {
			return err
		}
		clock.skip(spec.PixelHoldMS)
		cb, err := clock.scanMode(freq, spec.Width/2, spec.ChannelScanMS/2, spec)
		if err != nil {
			return err
		}
		clock.skip(spec.PixelHoldMS)
		for x := 0; x < spec.Width; x++ {
			r, g, b := color.YCbCrToRGB(y[x], cb[x/2], cr[x/2])
			img.SetNRGBA(x, row, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return nil
}

func decodeRGBProfile(ctx context.Context, freq []float64, clock *sampleClock, img *image.NRGBA, spec ModeSpec) error {
	for row := 0; row < spec.Height; row++ {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		if spec.Family == FamilyMCN {
			clock.sync(spec.SyncMS, 12)
			clock.skip(spec.PorchMS)
		}
		var channels [3][]uint8
		for i := 0; i < 3; i++ {
			duration := spec.RGBScanMS[i]
			if duration <= 0 {
				duration = spec.ScanMS
			}
			var err error
			channels[i], err = clock.scanMode(freq, spec.Width, duration, spec)
			if err != nil {
				return err
			}
		}
		for x := 0; x < spec.Width; x++ {
			img.SetNRGBA(x, row, color.NRGBA{R: channels[0][x], G: channels[1][x], B: channels[2][x], A: 255})
		}
	}
	return nil
}

// StreamEvent is emitted when a complete SSTV frame has been reconstructed.
// Frame delivery is independent of the radio TX state, so the caller can feed
// this decoder from a continuously running capture stream during transmission.
type StreamEvent struct {
	Image   *image.NRGBA
	Mode    ModeSpec
	VIS     uint8
	FrameID uint64
	// DecodeRequest is populated when deferred final-frame decoding is enabled.
	// It owns immutable frequency chunk headers and can be decoded by a worker
	// after Feed has returned, keeping the real-time audio path short.
	DecodeRequest *StreamUpdate
}

// StreamDecoder accepts arbitrary-sized PCM chunks and automatically detects
// sequential VIS-framed transmissions. It retains one active frame plus a
// bounded idle search window. Progressive snapshots are available during the
// frame through DrainUpdates; completed frames are returned from Feed.
type StreamDecoder struct {
	rate             int
	pending          []int16
	freqChunks       [][]float64
	freqLen          int
	tracker          *frequencyTracker
	idleFreq         []float64
	idleTracker      *frequencyTracker
	mode             *ModeSpec
	bodyStart        int
	maxIdle          int
	visSearchFrom    int
	skipUntil        int // end offset of a complete frame rejected by fixed-mode filtering
	fixedMode        string
	activeVIS        uint8
	offset           float64
	lastPreviewRows  int
	frameID          uint64
	previewSequence  uint64
	updates          []StreamUpdate
	deferFrameDecode bool
}

// StreamUpdate is an immutable request to reconstruct a progressive raster.
// Feed only snapshots the frequency track; Decode performs the comparatively
// expensive pixel reconstruction on a separate worker so audio capture never
// waits for preview painting. Frequency contains samples through the current
// raster row and is safe to use after the decoder continues receiving audio.
type StreamUpdate struct {
	Image      *image.NRGBA // populated only by callers that decode eagerly
	Mode       ModeSpec
	Rows       int
	TotalRows  int
	SampleRate int
	BodyStart  int
	Offset     float64
	FrameID    uint64
	Sequence   uint64
	// FrequencyChunks references immutable per-Feed discriminator blocks.
	// Copying only the slice headers avoids repeatedly copying the entire
	// accumulated image track on the real-time audio goroutine.
	FrequencyChunks [][]float64
	FrequencyLength int
	// PCM references the completed frame's immutable original samples when this
	// request represents a final raster, preserving all mode-specific fallback
	// paths used by the synchronous decoder. Progressive previews leave it nil.
	PCM []int16
}

// Decode reconstructs the currently received rows represented by this update.
// It is intended for standalone callers/tests. The production preview worker
// should use DecodeWithFrequency and append only unseen chunks between updates.
func (u StreamUpdate) Decode(ctx context.Context) (*image.NRGBA, error) {
	if u.Image != nil {
		return u.Image, nil
	}
	freq := flattenFrequencyChunks(u.FrequencyChunks, u.FrequencyLength)
	return u.DecodeWithFrequency(ctx, freq)
}

// DecodeFinal reconstructs a completed frame with the same raw-PCM fallback as
// the synchronous StreamDecoder path. The PCM slice is an immutable view owned
// by the request, so a frame worker can safely decode it after Feed returns.
func (u StreamUpdate) DecodeFinal(ctx context.Context) (*image.NRGBA, error) {
	if u.Image != nil {
		return u.Image, nil
	}
	if u.Mode.ID == "" || u.SampleRate <= 0 || u.FrequencyLength <= 0 {
		return nil, errors.New("invalid completed SSTV frame request")
	}
	frequency := flattenFrequencyChunks(u.FrequencyChunks, u.FrequencyLength)
	if len(u.PCM) == 0 {
		return u.DecodeWithFrequency(ctx, frequency)
	}
	return decodeModeFromFrequency(ctx, frequency, u.SampleRate, u.Mode, u.BodyStart, u.PCM, u.Offset)
}

// DecodeWithFrequency reconstructs a preview from a caller-maintained,
// contiguous frequency track. The supplied slice must contain at least the
// FrequencyLength samples referenced by this update.
func (u StreamUpdate) DecodeWithFrequency(ctx context.Context, frequency []float64) (*image.NRGBA, error) {
	if u.Image != nil {
		return u.Image, nil
	}
	if u.Mode.ID == "" || u.SampleRate <= 0 || u.FrequencyLength <= 0 || len(frequency) < u.FrequencyLength {
		return nil, errors.New("invalid progressive SSTV preview request")
	}
	return decodeModeRowsFromFrequency(ctx, frequency[:u.FrequencyLength:u.FrequencyLength], u.SampleRate, u.Mode, u.BodyStart, nil, u.Offset, u.Rows)
}

func flattenFrequencyChunks(chunks [][]float64, limit int) []float64 {
	if limit <= 0 {
		for _, chunk := range chunks {
			limit += len(chunk)
		}
	}
	if limit <= 0 {
		return nil
	}
	out := make([]float64, 0, limit)
	for _, chunk := range chunks {
		if len(out) >= limit {
			break
		}
		remaining := limit - len(out)
		if len(chunk) > remaining {
			chunk = chunk[:remaining]
		}
		out = append(out, chunk...)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func frequencyChunksTail(chunks [][]float64, skip int) []float64 {
	if skip < 0 {
		skip = 0
	}
	out := make([]float64, 0)
	for _, chunk := range chunks {
		if skip >= len(chunk) {
			skip -= len(chunk)
			continue
		}
		out = append(out, chunk[skip:]...)
		skip = 0
	}
	return out
}

func NewStreamDecoder(sampleRate int) (*StreamDecoder, error) {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	return &StreamDecoder{rate: sampleRate, maxIdle: sampleRate * 12}, nil
}

// Feed appends one captured PCM block and returns every complete image now
// available. It also prepares progressive image snapshots as raster rows
// arrive, keeping PLL state between input blocks.
func (d *StreamDecoder) Feed(ctx context.Context, samples []int16) ([]StreamEvent, error) {
	if d == nil {
		return nil, errors.New("nil SSTV stream decoder")
	}
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	wasActive := d.mode != nil && d.tracker != nil
	if len(samples) > 0 {
		d.pending = append(d.pending, samples...)
		if wasActive {
			chunk := d.tracker.Process(samples)
			if len(chunk) > 0 {
				d.freqChunks = append(d.freqChunks, chunk)
				d.freqLen += len(chunk)
			}
		} else {
			// Idle: maintain an incremental frequency track for header detection
			// instead of re-demodulating the whole pending buffer every feed.
			if d.idleTracker == nil {
				d.idleTracker = newFrequencyTracker(d.rate)
			}
			d.idleFreq = append(d.idleFreq, d.idleTracker.Process(samples)...)
		}
	}
	var events []StreamEvent
	for {
		if err := ctxErr(ctx); err != nil {
			return events, err
		}

		if d.skipUntil > 0 {
			if len(d.pending) < d.skipUntil {
				return events, nil
			}
			d.pending = append([]int16(nil), d.pending[d.skipUntil:]...)
			if len(d.idleFreq) >= d.skipUntil {
				d.idleFreq = append([]float64(nil), d.idleFreq[d.skipUntil:]...)
			}
			d.skipUntil = 0
			d.visSearchFrom = 0
		}

		if d.mode == nil {
			spec, vis, bodyStart, headerStart, nextSearch, err := detectModeHeaderFrom(d.pending, d.idleFreq, d.rate, d.visSearchFrom)
			if err != nil && d.fixedMode != "" {
				// In fixed mode a user has already supplied the mode identity. If
				// the VIS data/parity symbols are corrupted, fall back to the known
				// mode-family sync prefix and decode using the selected profile.
				if fixedSpec, lookupErr := LookupMode(d.fixedMode); lookupErr == nil {
					spec, vis, bodyStart, headerStart, nextSearch, err = detectFixedModePrefix(d.pending, d.idleFreq, d.rate, d.visSearchFrom, fixedSpec)
				}
			}
			if err != nil {
				d.visSearchFrom = max(d.visSearchFrom, nextSearch)
				if d.visSearchFrom > d.rate*2 {
					trim := min(d.visSearchFrom-d.rate, len(d.pending))
					if trim > 0 {
						d.pending = append([]int16(nil), d.pending[trim:]...)
						if len(d.idleFreq) >= trim {
							d.idleFreq = append([]float64(nil), d.idleFreq[trim:]...)
						}
						d.visSearchFrom -= trim
					}
				}
				if len(d.pending) > d.maxIdle {
					trim := len(d.pending) - d.maxIdle
					d.pending = append([]int16(nil), d.pending[trim:]...)
					if len(d.idleFreq) >= trim {
						d.idleFreq = append([]float64(nil), d.idleFreq[trim:]...)
					}
					d.visSearchFrom = max(0, d.visSearchFrom-trim)
				}
				return events, nil
			}
			if d.fixedMode != "" && spec.ID != d.fixedMode {
				bodySamples := int(math.Round(BodyDurationMS(spec) * float64(d.rate) / 1000))
				frameEnd := bodyStart + bodySamples
				if len(d.pending) < frameEnd {
					d.skipUntil = frameEnd
					return events, nil
				}
				d.pending = append([]int16(nil), d.pending[frameEnd:]...)
				if len(d.idleFreq) >= frameEnd {
					d.idleFreq = append([]float64(nil), d.idleFreq[frameEnd:]...)
				}
				d.visSearchFrom = 0
				continue
			}
			if headerStart < 0 || headerStart > len(d.pending) {
				d.visSearchFrom = max(d.visSearchFrom+1, nextSearch)
				return events, nil
			}
			d.pending = append([]int16(nil), d.pending[headerStart:]...)
			bodyStart -= headerStart
			d.mode, d.bodyStart, d.activeVIS = &spec, bodyStart, vis
			d.frameID++
			d.tracker = newFrequencyTracker(d.rate)
			initialFrequency := d.tracker.Process(d.pending)
			d.freqChunks = nil
			if len(initialFrequency) > 0 {
				d.freqChunks = append(d.freqChunks, initialFrequency)
			}
			d.freqLen = len(initialFrequency)
			d.idleFreq = nil
			d.idleTracker = nil
			// The header now begins at offset zero, so its 300 ms leader is the
			// first window. Measure its average frequency from the demodulated
			// track to compensate the receiver's offset.
			d.offset = 0
			if leaderN := int(math.Round(0.300 * float64(d.rate))); leaderN <= d.freqLen {
				if f := meanFreq(d.freqChunks[0], 0, leaderN); f >= 1_700 && f <= 2_100 {
					d.offset = f - 1_900
				}
			}
			d.lastPreviewRows = 0
			d.updates = nil
		}

		bodySamples := int(math.Round(BodyDurationMS(*d.mode) * float64(d.rate) / 1000))
		frameEnd := d.bodyStart + bodySamples
		if len(d.pending) < frameEnd {
			d.prepareProgressiveUpdate(ctx, frameEnd)
			return events, nil
		}
		if d.deferFrameDecode {
			// Snapshot only immutable chunk headers. The actual raster reconstruction
			// and image encoding happen on the frame worker, never on the PCM worker.
			request := &StreamUpdate{
				Mode:            *d.mode,
				Rows:            d.mode.Height,
				TotalRows:       d.mode.Height,
				SampleRate:      d.rate,
				BodyStart:       d.bodyStart,
				Offset:          d.offset,
				FrameID:         d.frameID,
				Sequence:        d.previewSequence + 1,
				FrequencyChunks: append([][]float64(nil), d.freqChunks...),
				FrequencyLength: frameEnd,
				PCM:             d.pending[:frameEnd:frameEnd],
			}
			events = append(events, StreamEvent{Mode: *d.mode, VIS: d.activeVIS, FrameID: d.frameID, DecodeRequest: request})
		} else {
			frameFrequency := flattenFrequencyChunks(d.freqChunks, frameEnd)
			img, err := decodeModeFromFrequency(ctx, frameFrequency, d.rate, *d.mode, d.bodyStart, d.pending[:frameEnd], d.offset)
			if err != nil {
				if ctxErr(ctx) != nil {
					return events, err
				}
				failedMode := d.mode.Name
				d.finishActiveFrame(frameEnd)
				return events, fmt.Errorf("decode streamed %s frame: %w", failedMode, err)
			}
			events = append(events, StreamEvent{Image: img, Mode: *d.mode, VIS: d.activeVIS, FrameID: d.frameID})
		}
		d.finishActiveFrame(frameEnd)
	}
}

func (d *StreamDecoder) prepareProgressiveUpdate(ctx context.Context, frameEnd int) {
	if d.mode == nil || d.bodyStart < 0 || d.freqLen <= d.bodyStart {
		return
	}
	bodySamples := frameEnd - d.bodyStart
	if bodySamples <= 0 {
		return
	}
	received := min(max(0, d.freqLen-d.bodyStart), bodySamples)
	rows := int(float64(received) / float64(bodySamples) * float64(d.mode.Height))
	if rows <= d.lastPreviewRows || rows <= 0 {
		return
	}
	// Publish roughly 60 immutable snapshots per image. The former 20% cadence
	// meant a 240-line Robot36 preview stayed blank for about 48 lines. Unlike
	// the old implementation, this function does not decode pixels: it only
	// copies the exact frequency prefix needed by the preview worker.
	stride := max(1, d.mode.Height/60)
	if rows-d.lastPreviewRows < stride && rows < d.mode.Height {
		return
	}
	previewRows := rows
	if d.mode.Family == FamilyPD || d.mode.Family == FamilyMP ||
		(d.mode.Family == FamilyRobot && !d.mode.Monochrome && !d.mode.RobotSequential) {
		previewRows -= previewRows % 2
	}
	if previewRows <= d.lastPreviewRows {
		return
	}
	previewSpec := *d.mode
	previewSpec.Height = previewRows
	neededSamples := d.bodyStart + int(math.Round(BodyDurationMS(previewSpec)*float64(d.rate)/1000))
	freqEnd := min(d.freqLen, max(d.bodyStart+1, neededSamples))
	// The chunk payload is a shallow immutable snapshot. The audio worker only
	// allocates one small slice of headers per preview instead of copying up to
	// several megabytes of accumulated per-sample frequencies every few rows.
	chunkSnapshot := append([][]float64(nil), d.freqChunks...)
	d.previewSequence++
	d.updates = append(d.updates, StreamUpdate{
		Mode:            *d.mode,
		Rows:            previewRows,
		TotalRows:       d.mode.Height,
		SampleRate:      d.rate,
		BodyStart:       d.bodyStart,
		Offset:          d.offset,
		FrameID:         d.frameID,
		Sequence:        d.previewSequence,
		FrequencyChunks: chunkSnapshot,
		FrequencyLength: freqEnd,
	})
	d.lastPreviewRows = previewRows
}

// finishActiveFrame preserves the remaining audio and frequency track
// for detecting the next VIS header after the current frame.
func (d *StreamDecoder) finishActiveFrame(frameEnd int) {
	if frameEnd < 0 {
		frameEnd = 0
	}
	if frameEnd > len(d.pending) {
		frameEnd = len(d.pending)
	}

	remaining := append([]int16(nil), d.pending[frameEnd:]...)
	d.pending = remaining

	if frameEnd <= d.freqLen {
		d.idleFreq = frequencyChunksTail(d.freqChunks, frameEnd)
		d.idleTracker = d.tracker
	} else {
		d.idleTracker = newFrequencyTracker(d.rate)
		d.idleFreq = d.idleTracker.Process(remaining)
	}

	d.mode = nil
	d.bodyStart = 0
	d.activeVIS = 0
	d.offset = 0
	d.freqChunks = nil
	d.freqLen = 0
	d.tracker = nil
	d.lastPreviewRows = 0
	d.visSearchFrom = 0
}
func (d *StreamDecoder) clearActiveFrame() {
	d.mode = nil
	d.bodyStart = 0
	d.activeVIS = 0
	d.offset = 0
	d.tracker = nil
	d.idleFreq = nil
	d.idleTracker = nil
	d.lastPreviewRows = 0
	d.visSearchFrom = 0
}

// DrainUpdates returns and clears progressive image snapshots produced since
// the previous call. It must be called by the same serialized stream owner as
// Feed.
func (d *StreamDecoder) DrainUpdates() []StreamUpdate {
	if d == nil || len(d.updates) == 0 {
		return nil
	}
	updates := d.updates
	d.updates = nil
	return updates
}

// SetDeferredFrameDecode moves completed-raster reconstruction out of Feed.
// It must be configured before receiving samples. Callers then decode each
// StreamEvent.DecodeRequest on a separate worker; default behavior remains
// synchronous for library users that rely on Image being populated in Feed.
func (d *StreamDecoder) SetDeferredFrameDecode(enabled bool) {
	if d == nil {
		return
	}
	d.deferFrameDecode = enabled
}

// SetMode selects a fixed receive mode. An empty string restores Auto detection.
func (d *StreamDecoder) SetMode(modeID string) error {
	if d == nil {
		return errors.New("nil SSTV stream decoder")
	}
	if strings.TrimSpace(modeID) == "" {
		d.fixedMode = ""
		// A mode change is a stream boundary. Keeping buffered PCM here can
		// make Auto/fixed-mode detection inspect seconds of samples captured
		// under the previous selection and may lock onto a stale partial header.
		d.pending = nil
		d.mode = nil
		d.bodyStart = 0
		d.skipUntil = 0
		d.visSearchFrom = 0
		d.activeVIS = 0
		d.offset = 0
		d.freqChunks = nil
		d.freqLen = 0
		d.tracker = nil
		d.idleFreq = nil
		d.idleTracker = nil
		d.lastPreviewRows = 0
		d.updates = nil
		return nil
	}
	spec, err := LookupMode(modeID)
	if err != nil {
		return err
	}
	if !spec.CodecReady {
		return fmt.Errorf("%s decoder is not implemented", spec.Name)
	}
	d.fixedMode = spec.ID
	d.pending = nil
	d.mode = nil
	d.bodyStart = 0
	d.skipUntil = 0
	d.visSearchFrom = 0
	d.activeVIS = 0
	d.offset = 0
	d.freqChunks = nil
	d.freqLen = 0
	d.tracker = nil
	d.idleFreq = nil
	d.idleTracker = nil
	d.lastPreviewRows = 0
	d.updates = nil
	return nil
}

// Reset discards the current partial image and returns to VIS hunting.
func (d *StreamDecoder) Reset() {
	if d == nil {
		return
	}
	d.pending = nil
	d.mode = nil
	d.bodyStart = 0
	d.skipUntil = 0
	d.visSearchFrom = 0
	d.activeVIS = 0
	d.offset = 0
	d.freqChunks = nil
	d.freqLen = 0
	d.tracker = nil
	d.idleFreq = nil
	d.idleTracker = nil
	d.lastPreviewRows = 0
	d.updates = nil
}

// PendingAudio returns a copy of audio samples buffered after the most recently
// completed frame. It is useful for protocols appended immediately after an
// SSTV raster (such as MMSSTV FSKID): callers can seed their side-channel
// decoder with only the true post-frame tail instead of rescanning hundreds
// of milliseconds of the preceding image.
func (d *StreamDecoder) PendingAudio() []int16 {
	if d == nil || len(d.pending) == 0 {
		return nil
	}
	return append([]int16(nil), d.pending...)
}

// ActiveMode reports the currently detected in-progress image, if any.
func (d *StreamDecoder) ActiveMode() (ModeSpec, bool) {
	if d == nil || d.mode == nil {
		return ModeSpec{}, false
	}
	return *d.mode, true
}

// StreamProgress describes the active frame's progress from the end of VIS
// through the raster. Percent reaches 100 when the full frame audio is ready
// for pixel reconstruction; the decoded image event follows immediately.
type StreamProgress struct {
	Mode      ModeSpec
	Received  int
	Total     int
	Percent   float64
	Rows      int
	TotalRows int
}

// Progress reports the active stream frame without mutating decoder state.
func (d *StreamDecoder) Progress() (StreamProgress, bool) {
	if d == nil || d.mode == nil || d.rate <= 0 {
		return StreamProgress{}, false
	}
	total := int(math.Round(BodyDurationMS(*d.mode) * float64(d.rate) / 1000))
	received := max(0, len(d.pending)-d.bodyStart)
	if received > total {
		received = total
	}
	percent := 0.0
	if total > 0 {
		percent = float64(received) * 100 / float64(total)
	}
	rows := int(math.Floor(percent * float64(d.mode.Height) / 100))
	return StreamProgress{Mode: *d.mode, Received: received, Total: total, Percent: math.Max(0, math.Min(100, percent)), Rows: rows, TotalRows: d.mode.Height}, true
}
