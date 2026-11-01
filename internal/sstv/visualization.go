package sstv

import "math"

// MMSSTVSpectrumWindowSamples returns the raw mono PCM window length used by
// MMSSTV's frequency-occupancy spectrum at the supplied input sample rate.
// MMSSTV targets 2048 discriminator samples and downsamples 48/44.1 kHz by 4,
// 23/24 kHz by 2, and lower rates directly. Below 10 kHz it uses 1024 samples.
func MMSSTVSpectrumWindowSamples(sampleRate int) int {
	if sampleRate >= 43_000 {
		return 2048 * 4
	}
	if sampleRate >= 23_000 {
		return 2048 * 2
	}
	if sampleRate >= 10_000 {
		return 2048
	}
	return 1024
}

// MMSSTVFrequencyHistogram reproduces the core of MMSSTV's default FFTType=2
// display. It is not a magnitude FFT: it runs a zero-crossing frequency
// discriminator, smooths the instantaneous frequency with a 400 Hz 2nd-order
// low-pass, and deposits each sample into its frequency bin with half-weight
// adjacent bins. The result is normalized to MMSSTV FFTGain=5's 24-hit ceiling.
//
// minHz/maxHz describe the displayed frequency span (MMSSTV FFTWidth=1 is
// 700..2700 Hz). bins is the number of horizontal display bins.
func MMSSTVFrequencyHistogram(samples []int16, sampleRate, bins int, minHz, maxHz float64) []float64 {
	out := make([]float64, max(0, bins))
	if len(samples) < 16 || sampleRate < 8_000 || bins < 2 || minHz < 0 || maxHz <= minHz {
		return out
	}

	decimation := 1
	fftSamples := 2048
	switch {
	case sampleRate >= 43_000:
		decimation = 4
	case sampleRate >= 23_000:
		decimation = 2
	case sampleRate < 10_000:
		fftSamples = 1024
	}
	windowSamples := fftSamples * decimation
	if len(samples) > windowSamples {
		samples = samples[len(samples)-windowSamples:]
	}

	decimated := make([]int16, 0, (len(samples)+decimation-1)/decimation)
	for i := 0; i < len(samples); i += decimation {
		decimated = append(decimated, samples[i])
	}
	effectiveRate := sampleRate / decimation
	fqc := newMMSSTVFQC(effectiveRate)

	// Preserve MMSSTV's native 2048-bin frequency grid first, then resample the
	// selected 700..2700 Hz window to the browser canvas. At 48 kHz after /4,
	// one native bin is 5.86 Hz; directly depositing into 1 Hz screen bins would
	// make each SSTV tone look artificially thin compared with MMSSTV.
	const fftSize = 2048.0
	const centerIncrement = 30.0
	const adjacentIncrement = centerIncrement / 2
	const maximum = centerIncrement * 24 // FFTGain=5 default
	const nativeBins = 2048
	occupancy := make([]float64, nativeBins)
	for _, sample := range decimated {
		filteredHz := fqc.Do(float64(sample) / 32768)
		frequencyIndex := int(filteredHz * fftSize / float64(effectiveRate))
		if frequencyIndex < 1 || frequencyIndex >= nativeBins-1 {
			continue
		}
		if occupancy[frequencyIndex] < maximum {
			occupancy[frequencyIndex] = math.Min(maximum, occupancy[frequencyIndex]+centerIncrement)
			occupancy[frequencyIndex-1] += adjacentIncrement
			occupancy[frequencyIndex+1] += adjacentIncrement
		}
	}
	// Main.cpp's CalcFFT selects the range by native bin index and stretches
	// it to the graph's width. Sample the same integer-index mapping here.
	for i := range out {
		frequency := minHz + float64(i)*(maxHz-minHz)/float64(bins-1)
		index := int(frequency * fftSize / float64(effectiveRate))
		if index >= 0 && index < len(occupancy) {
			out[i] = math.Min(1, occupancy[index]/maximum)
		}
	}
	return out
}

// mmsstvFQC follows the CFQC zero-crossing detector used by MMSSTV's FFTType=2
// frequency-occupancy display. It intentionally has no input AGC or pre-filter:
// the original FQC works on raw decimated audio and uses MMSSTV's prewarped
// 2nd-order Butterworth 400 Hz IIR on normalized frequency after crossing.
type mmsstvFQC struct {
	rate       float64
	previous   float64
	samplePos  float64
	lastCross  float64
	normalized float64
	timer      int
	outLPF     biquad
}

func newMMSSTVFQC(sampleRate int) *mmsstvFQC {
	rate := float64(sampleRate)
	const zeroFrequency = -1900.0 / 400.0
	lpf := mmsstvFQCLowPass(400, rate)
	// Seed the IIR delay line with the steady-state idle value. MMSSTV's
	// zero-initialized delay line creates a short artificial 1900 Hz startup
	// streak; preloading avoids drawing that non-signal into a fresh waterfall.
	lpf.x1, lpf.x2, lpf.y1, lpf.y2 = zeroFrequency, zeroFrequency, zeroFrequency, zeroFrequency
	return &mmsstvFQC{
		rate:       rate,
		normalized: zeroFrequency, // MMSSTV's ZEROFQ
		timer:      sampleRate,
		outLPF:     lpf,
	}
}

// mmsstvFQCLowPass ports fir.cpp's MakeIIR(fc, fs, order=2, bc=0, rp=0)
// coefficient calculation. Its frequency prewarping differs slightly from
// an RBJ cookbook biquad, so use the same bilinear-transform coefficients.
func mmsstvFQCLowPass(fc, fs float64) biquad {
	wa := math.Tan(math.Pi * fc / fs)
	wa2 := wa * wa
	a0 := 1 + math.Sqrt2*wa + wa2
	return biquad{
		b0: wa2 / a0,
		b1: 2 * wa2 / a0,
		b2: wa2 / a0,
		a1: 2 * (wa2 - 1) / a0,
		a2: (1 - math.Sqrt2*wa + wa2) / a0,
	}
}

func (f *mmsstvFQC) Do(sample float64) float64 {
	crossing := (sample >= 0 && f.previous < 0) || (sample < 0 && f.previous >= 0)
	if crossing {
		interval := f.samplePos - f.lastCross
		offset := sample / (sample - f.previous)
		f.lastCross = f.samplePos - offset
		interval -= offset
		if interval >= 1 {
			frequency := f.rate * 0.5 / interval
			f.normalized = (frequency - 1900) / 400
			f.timer = int(f.rate)
		}
	}
	if f.timer > 0 {
		f.timer--
		if f.timer == 0 {
			f.normalized = -1900.0 / 400.0
		}
	}
	filtered := f.outLPF.Do(f.normalized)
	f.previous = sample
	f.samplePos++
	return 1900 + filtered*400
}
