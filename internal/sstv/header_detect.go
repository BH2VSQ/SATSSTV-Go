package sstv

import (
	"errors"
	"fmt"
	"math"
)

const (
	mmsstvExtendedHeaderMS = 1_150.0
	narrowVISHeaderMS      = 950.0
	avt90HeaderMS          = 3*910.0 + 32*17*(1000.0/102.4)
)

// DetectModeHeader detects standard VIS, MMSSTV extended VIS, N-VIS and AVT90
// headers. The returned bodyStart points at the first image sample.
func DetectModeHeader(samples []int16, sampleRate int) (ModeSpec, uint8, int, error) {
	freq := instantaneousFrequency(samples, sampleRate)
	spec, vis, bodyStart, _, _, err := detectModeHeaderFrom(samples, freq, sampleRate, 0)
	return spec, vis, bodyStart, err
}

func detectModeHeaderFrom(samples []int16, freq []float64, sampleRate, startAt int) (spec ModeSpec, vis uint8, bodyStart, headerStart, nextStart int, retErr error) {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return ModeSpec{}, 0, 0, startAt, startAt, fmt.Errorf("invalid sample rate %d", sampleRate)
	}
	step := max(1, sampleRate/500)
	var nextCandidates []int

	// Probe ordinary VIS first, then verify any registered code against the
	// longer MMSSTV header families before committing to Auto mode selection.
	visCode, afterVIS, nextVIS, visErr := detectVISFrom(samples, freq, sampleRate, startAt)
	if visErr == nil {
		headerStart = afterVIS - int(math.Round(0.910*float64(sampleRate)))
		if found, ok := ModeFromVIS(visCode); ok {
			if found.Header == HeaderAVT {
				body, complete, nextAVT, err := detectAVT90BodyStart(samples, sampleRate, headerStart)
				if err != nil {
					return ModeSpec{}, visCode, 0, headerStart, nextAVT, err
				}
				if !complete {
					return ModeSpec{}, visCode, 0, headerStart, headerStart, errors.New("AVT90 synchronization header is incomplete")
				}
				return found, visCode, body, headerStart, body, nil
			}
			// A registered 7-bit code can also be the first seven data bits of an
			// MMSSTV 16-bit VIS. Always verify the longer header formats before
			// locking Auto to a standard mode, even when the leader starts at the
			// current search cursor.
			if code, offset, next, err := detectExtendedVISFrom(samples, freq, sampleRate, startAt); err == nil {
				extendedStart := offset - int(math.Round(mmsstvExtendedHeaderMS/1000*float64(sampleRate)))
				if foundExtended, ok := ModeFromExtendedVIS(code); ok && extendedStart <= headerStart {
					return foundExtended, 0x23, offset, extendedStart, next, nil
				}
			} else {
				if next <= startAt {
					return ModeSpec{}, 0, 0, 0, startAt, err
				}
				nextCandidates = append(nextCandidates, next)
			}
			if code, offset, next, err := detectNarrowVISFrom(samples, freq, sampleRate, startAt); err == nil {
				narrowStart := offset - int(math.Round(narrowVISHeaderMS/1000*float64(sampleRate)))
				if foundNarrow, ok := ModeFromNarrowVIS(code); ok && narrowStart < headerStart {
					return foundNarrow, code, offset, narrowStart, next, nil
				}
			} else {
				if next <= startAt {
					return ModeSpec{}, 0, 0, 0, startAt, err
				}
				nextCandidates = append(nextCandidates, next)
			}
			return found, visCode, afterVIS, headerStart, headerStart + step, nil
		}
		nextCandidates = append(nextCandidates, headerStart+step)
	} else {
		nextCandidates = append(nextCandidates, nextVIS)
	}

	if code, offset, next, err := detectExtendedVISFrom(samples, freq, sampleRate, startAt); err == nil {
		if found, ok := ModeFromExtendedVIS(code); ok {
			return found, 0x23, offset, offset - int(math.Round(mmsstvExtendedHeaderMS/1000*float64(sampleRate))), next, nil
		}
	} else {
		nextCandidates = append(nextCandidates, next)
	}
	if code, offset, next, err := detectNarrowVISFrom(samples, freq, sampleRate, startAt); err == nil {
		if found, ok := ModeFromNarrowVIS(code); ok {
			return found, code, offset, offset - int(math.Round(narrowVISHeaderMS/1000*float64(sampleRate))), next, nil
		}
	} else {
		nextCandidates = append(nextCandidates, next)
	}

	if len(nextCandidates) == 0 {
		nextStart = startAt
	} else {
		nextStart = nextCandidates[0]
		for _, candidate := range nextCandidates[1:] {
			// Advance only as far as every candidate header format has been
			// checked. The longer 16-bit header must not be skipped by a
			// shorter standard VIS probe.
			if candidate < nextStart {
				nextStart = candidate
			}
		}
	}
	if visErr == nil {
		return ModeSpec{}, visCode, afterVIS, headerStart, max(startAt, nextStart), fmt.Errorf("VIS %d is not registered", visCode)
	}
	return ModeSpec{}, 0, 0, 0, max(startAt, nextStart), visErr
}

// detectFixedModePrefix locates a transmission for an explicitly selected
// RX mode even if its VIS payload/parity bits are damaged. It still requires
// the mode-family leader and break sequence, so random audio cannot immediately
// open a raster. Auto mode always requires a fully valid header.
func detectFixedModePrefix(samples []int16, freq []float64, rate, startAt int, spec ModeSpec) (ModeSpec, uint8, int, int, int, error) {
	if rate < 8_000 || rate > 192_000 {
		return ModeSpec{}, 0, 0, startAt, startAt, fmt.Errorf("invalid sample rate %d", rate)
	}
	var leaderN, breakN, firstToneN, headerN int
	var breakHz, firstToneHz float64
	switch spec.Header {
	case HeaderVIS:
		leaderN = int(math.Round(.300 * float64(rate)))
		breakN = int(math.Round(.010 * float64(rate)))
		firstToneN = leaderN
		breakHz, firstToneHz = 1_200, 1_900
		headerN = int(math.Round(.910 * float64(rate)))
	case HeaderMMSSTV:
		leaderN = int(math.Round(.300 * float64(rate)))
		breakN = int(math.Round(.010 * float64(rate)))
		firstToneN = leaderN
		breakHz, firstToneHz = 1_200, 1_900
		headerN = int(math.Round(1.150 * float64(rate)))
	case HeaderNarrowVIS:
		leaderN = int(math.Round(.300 * float64(rate)))
		breakN = int(math.Round(.100 * float64(rate)))
		firstToneN = int(math.Round(.022 * float64(rate)))
		breakHz, firstToneHz = 2_100, 1_900
		headerN = int(math.Round(.950 * float64(rate)))
	default:
		return ModeSpec{}, 0, 0, startAt, startAt, errors.New("fixed-prefix fallback is unavailable for this header family")
	}
	if len(samples) < headerN {
		return ModeSpec{}, 0, startAt, startAt, startAt, errors.New("audio is too short for selected-mode synchronization")
	}
	step := max(1, rate/500)
	start, step := alignedStart(startAt, step)
	maxStart := min(len(samples)-headerN, rate*3)
	for coarse := start; coarse <= maxStart; coarse += step {
		offset := meanFreqQuick(freq, coarse, leaderN, max(1, rate/8_000)) - 1_900
		if offset < -350 || offset > 350 {
			continue
		}
		low, high := max(startAt, coarse-step), min(maxStart, coarse+step)
		fineStep := max(1, rate/4_000)
		for candidate := low; candidate <= high; candidate += fineStep {
			p := candidate
			if p+leaderN+breakN+firstToneN > len(samples) {
				continue
			}
			breakMean := meanFreqQuick(freq, p+leaderN, breakN, max(1, rate/8_000))
			if breakMean < breakHz+offset-100 || breakMean > breakHz+offset+100 {
				continue
			}
			if headerTone(samples[p:p+leaderN], rate, 1_900+offset) != 1_900+offset {
				continue
			}
			p += leaderN
			if headerTone(samples[p:p+breakN], rate, breakHz+offset) != breakHz+offset {
				continue
			}
			p += breakN
			if headerTone(samples[p:p+firstToneN], rate, firstToneHz+offset) != firstToneHz+offset {
				continue
			}
			return spec, spec.VIS, candidate + headerN, candidate, candidate + headerN, nil
		}
	}
	return ModeSpec{}, 0, 0, maxStart + 1, maxStart + 1, errors.New("selected-mode synchronization prefix not detected")
}

func detectExtendedVISFrom(samples []int16, freq []float64, rate, startAt int) (uint16, int, int, error) {
	headerN := int(math.Round(mmsstvExtendedHeaderMS / 1000 * float64(rate)))
	if len(samples) < headerN {
		return 0, 0, startAt, errors.New("audio is too short for MMSSTV extended VIS")
	}
	start, step := alignedStart(startAt, max(1, rate/500))
	maxStart := min(len(samples)-headerN, rate*3)
	leaderN := int(math.Round(.300 * float64(rate)))
	breakN := int(math.Round(.010 * float64(rate)))
	bitN := int(math.Round(.030 * float64(rate)))
	quickStride := max(1, rate/8_000)
	for ; start <= maxStart; start += step {
		p := start
		// Reject candidates unless both the leader and its short break have the
		// expected frequency. A continuous 1900 Hz idle tone must not trigger a
		// 300 ms Goertzel scan at every 2 ms search position.
		leader := meanFreqQuick(freq, p, leaderN, quickStride)
		if leader < 1_550 || leader > 2_250 {
			continue
		}
		offset := leader - 1_900
		breakMean := meanFreqQuick(freq, p+leaderN, breakN, quickStride)
		if breakMean < 1_100+offset-120 || breakMean > 1_300+offset+120 {
			continue
		}
		if bestTone(samples[p:p+leaderN], rate, 1_900+offset, 1_200+offset) != 1_900+offset {
			continue
		}
		p += leaderN
		if headerTone(samples[p:p+breakN], rate, 1_200+offset, 1_900+offset) != 1_200+offset {
			continue
		}
		p += breakN
		if headerTone(samples[p:p+leaderN], rate, 1_900+offset, 1_200+offset) != 1_900+offset {
			continue
		}
		p += leaderN
		if headerTone(samples[p:p+bitN], rate, 1_200+offset, 1_100+offset) != 1_200+offset {
			continue
		}
		p += bitN
		var code uint16
		valid := true
		for bit := 0; bit < 16; bit++ {
			tone := headerTone(samples[p:p+bitN], rate, 1_100+offset, 1_300+offset)
			if tone == 1_100+offset {
				code |= 1 << bit
			} else if tone != 1_300+offset {
				valid = false
			}
			p += bitN
		}
		if headerTone(samples[p:p+bitN], rate, 1_200+offset, 1_100+offset) != 1_200+offset {
			valid = false
		}
		if valid {
			if _, ok := ModeFromExtendedVIS(code); ok {
				return code, start + headerN, start, nil
			}
		}
	}
	return 0, 0, maxStart + 1, errors.New("MMSSTV extended VIS header not detected")
}

func detectNarrowVISFrom(samples []int16, freq []float64, rate, startAt int) (uint8, int, int, error) {
	headerN := int(math.Round(narrowVISHeaderMS / 1000 * float64(rate)))
	if len(samples) < headerN {
		return 0, 0, startAt, errors.New("audio is too short for N-VIS")
	}
	start, step := alignedStart(startAt, max(1, rate/500))
	maxStart := min(len(samples)-headerN, rate*3)
	leaderN := int(math.Round(.300 * float64(rate)))
	breakN := int(math.Round(.100 * float64(rate)))
	bitN := int(math.Round(.022 * float64(rate)))
	quickStride := max(1, rate/8_000)
	for ; start <= maxStart; start += step {
		p := start
		leader := meanFreqQuick(freq, p, leaderN, quickStride)
		if leader < 1_550 || leader > 2_250 {
			continue
		}
		offset := leader - 1_900
		breakMean := meanFreqQuick(freq, p+leaderN, breakN, quickStride)
		if breakMean < 2_000+offset || breakMean > 2_200+offset {
			continue
		}
		if bestTone(samples[p:p+leaderN], rate, 1_900+offset, 2_100+offset) != 1_900+offset {
			continue
		}
		p += leaderN
		if headerTone(samples[p:p+breakN], rate, 2_100+offset, 1_900+offset) != 2_100+offset {
			continue
		}
		p += breakN
		if headerTone(samples[p:p+bitN], rate, 1_900+offset, 2_100+offset) != 1_900+offset {
			continue
		}
		p += bitN
		var groups [4]uint8
		valid := true
		for group := range groups {
			for bit := 5; bit >= 0; bit-- {
				tone := headerTone(samples[p:p+bitN], rate, 1_900+offset, 2_100+offset)
				if tone == 1_900+offset {
					groups[group] |= 1 << bit
				} else if tone != 2_100+offset {
					valid = false
				}
				p += bitN
			}
		}
		if groups[0] != 0b101101 || groups[1] != 0b010101 || groups[3] != (0b010101^groups[2]) {
			valid = false
		}
		if valid {
			if _, ok := ModeFromNarrowVIS(groups[2]); ok {
				return groups[2], start + headerN, start, nil
			}
		}
	}
	return 0, 0, maxStart + 1, errors.New("MMSSTV N-VIS header not detected")
}

func detectAVT90BodyStart(samples []int16, rate, firstStart int) (int, bool, int, error) {
	visN := int(math.Round(.910 * float64(rate)))
	if len(samples) < firstStart+3*visN {
		// Keep the candidate's first VIS position. Advancing the stream search
		// cursor while this header is incomplete would make the next chunk
		// start at VIS #2 or inside the countdown and permanently lose AVT sync.
		return 0, false, firstStart, nil
	}
	for i := 0; i < 3; i++ {
		code, ok := decodeStandardVISAt(samples, rate, firstStart+i*visN)
		if !ok || code != 68 {
			return 0, false, firstStart + visN*i, errors.New("AVT90 requires three consecutive VIS 68 headers")
		}
	}
	p := firstStart + 3*visN
	const symbolMS = 1000.0 / 102.4
	frac := 0.0
	nextSymbol := func() (int, int, bool) {
		exact := symbolMS/1000*float64(rate) + frac
		n := int(math.Round(exact))
		frac = exact - float64(n)
		if p+n > len(samples) {
			return p, p, false
		}
		start, end := p, p+n
		p = end
		return start, end, true
	}
	for count := 0; count < 32; count++ {
		start, end, ok := nextSymbol()
		if !ok {
			return 0, false, firstStart, nil
		}
		if headerTone(samples[start:end], rate, 1900, 1600, 2200) != 1900 {
			return 0, false, start, errors.New("invalid AVT90 countdown marker")
		}
		var first, inverse uint8
		for bit := 7; bit >= 0; bit-- {
			start, end, ok = nextSymbol()
			if !ok {
				return 0, false, firstStart, nil
			}
			tone := headerTone(samples[start:end], rate, 1600, 2200)
			if tone == 2200 {
				first |= 1 << bit
			} else if tone != 1600 {
				return 0, false, start, errors.New("invalid AVT90 data symbol")
			}
		}
		for bit := 7; bit >= 0; bit-- {
			start, end, ok = nextSymbol()
			if !ok {
				return 0, false, firstStart, nil
			}
			tone := headerTone(samples[start:end], rate, 1600, 2200)
			if tone == 2200 {
				inverse |= 1 << bit
			} else if tone != 1600 {
				return 0, false, start, errors.New("invalid AVT90 inverse symbol")
			}
		}
		if inverse != ^first || first>>5 != 0b101 || int(first&0x1f) != count {
			return 0, false, p, errors.New("AVT90 countdown integrity check failed")
		}
	}
	return p, true, p, nil
}

func decodeStandardVISAt(samples []int16, rate, start int) (uint8, bool) {
	leaderN := int(math.Round(.300 * float64(rate)))
	breakN := int(math.Round(.010 * float64(rate)))
	bitN := int(math.Round(.030 * float64(rate)))
	headerN := leaderN + breakN + leaderN + 10*bitN
	if start < 0 || start+headerN > len(samples) {
		return 0, false
	}
	p := start
	if headerTone(samples[p:p+leaderN], rate, 1900, 1200) != 1900 {
		return 0, false
	}
	p += leaderN
	if headerTone(samples[p:p+breakN], rate, 1200, 1900) != 1200 {
		return 0, false
	}
	p += breakN
	if headerTone(samples[p:p+leaderN], rate, 1900, 1200) != 1900 {
		return 0, false
	}
	p += leaderN
	if headerTone(samples[p:p+bitN], rate, 1200, 1100) != 1200 {
		return 0, false
	}
	p += bitN
	var code uint8
	ones := 0
	for bit := 0; bit < 7; bit++ {
		tone := headerTone(samples[p:p+bitN], rate, 1100, 1300)
		if tone == 1100 {
			code |= 1 << bit
			ones++
		} else if tone != 1300 {
			return 0, false
		}
		p += bitN
	}
	parity := headerTone(samples[p:p+bitN], rate, 1100, 1300)
	if parity != 1100 && parity != 1300 {
		return 0, false
	}
	if (ones+boolInt(parity == 1100))%2 != 0 {
		return 0, false
	}
	p += bitN
	if headerTone(samples[p:p+bitN], rate, 1200, 1100) != 1200 {
		return 0, false
	}
	return code, true
}

func alignedStart(start, step int) (int, int) {
	if start < 0 {
		start = 0
	}
	if r := start % step; r != 0 {
		start += step - r
	}
	return start, step
}
