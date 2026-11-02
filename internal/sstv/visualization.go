package sstv

import (
	"image"
	"image/color"
	"math"
)

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


// CorrectedDisplayFrequencyTracker produces the discriminator track used by the
// RX spectrum and waterfall. It consumes the same attenuated PCM as the image
// decoder and subtracts the current AFC offset before frequency histogramming.
type CorrectedDisplayFrequencyTracker struct {
	tracker          *frequencyTracker
	filter           biquad
	prefilter        biquad
	inputSampleRate  int
	outputSampleRate int
	decimation       int
	decimationPhase  int
}

// NewCorrectedDisplayFrequencyTracker creates a low-cost wide-offset display
// tracker. Display work is intentionally performed near 12 kHz: this retains
// the complete 700..2700 Hz SSTV audio band while avoiding a second full-rate
// discriminator competing with the ordered image decoder.
func NewCorrectedDisplayFrequencyTracker(sampleRate int) *CorrectedDisplayFrequencyTracker {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil
	}
	decimation := max(1, int(math.Round(float64(sampleRate)/12_000)))
	outputRate := sampleRate / decimation
	t := &CorrectedDisplayFrequencyTracker{
		inputSampleRate: sampleRate, outputSampleRate: outputRate,
		decimation: decimation,
	}
	t.Reset()
	return t
}

// SampleRate reports the discriminator rate after display-only decimation.
func (t *CorrectedDisplayFrequencyTracker) SampleRate() int {
	if t == nil {
		return 0
	}
	return t.outputSampleRate
}

// Reset discards filter history. It is used after a visualization discontinuity
// or a noise-gate interval; it never touches the SSTV decoder's AFC state.
func (t *CorrectedDisplayFrequencyTracker) Reset() {
	if t == nil || t.outputSampleRate <= 0 {
		return
	}
	t.tracker = newStreamFrequencyTracker(t.outputSampleRate, true)
	t.filter = mmsstvFQCLowPass(400, float64(t.outputSampleRate))
	const zeroFrequency = -1900.0 / 400.0
	t.filter.x1, t.filter.x2, t.filter.y1, t.filter.y2 = zeroFrequency, zeroFrequency, zeroFrequency, zeroFrequency
	t.prefilter = biquad{}
	if t.decimation > 1 {
		// Pre-filter before decimation so energy above the reduced Nyquist
		// frequency cannot fold back into the SSTV video band.
		t.prefilter = biquadLowPass(float64(t.outputSampleRate)*0.34, float64(t.inputSampleRate))
	}
	t.decimationPhase = 0
}

// Process returns a decimated, sample-aligned frequency track after applying
// the supplied AFC offset. A positive measured receiver offset is subtracted
// so standard SSTV tones return to their nominal coordinates.
func (t *CorrectedDisplayFrequencyTracker) Process(samples []int16, afcOffsetHz float64) []float64 {
	if t == nil || t.tracker == nil || len(samples) == 0 {
		return nil
	}
	var decimated []int16
	if t.decimation == 1 {
		decimated = samples
	} else {
		decimated = make([]int16, 0, (len(samples)+t.decimation-1)/t.decimation)
		for _, sample := range samples {
			value := t.prefilter.Do(float64(sample))
			t.decimationPhase++
			if t.decimationPhase < t.decimation {
				continue
			}
			t.decimationPhase = 0
			value = math.Max(-32768, math.Min(32767, math.Round(value)))
			decimated = append(decimated, int16(value))
		}
	}
	raw := t.tracker.Process(decimated)
	out := make([]float64, len(raw))
	for i, frequency := range raw {
		if frequency < 80 || frequency > 3_900 || math.IsNaN(frequency) || math.IsInf(frequency, 0) {
			continue
		}
		normalized := (frequency - afcOffsetHz - 1900) / 400
		out[i] = 1900 + t.filter.Do(normalized)*400
	}
	return out
}

// FrequencyHistogramFromTrack creates an MMSSTV-style frequency-occupancy
// spectrum from an already AFC-corrected discriminator track. Unlike the
// legacy PCM histogram, it does not apply a second independent discriminator.
func FrequencyHistogramFromTrack(frequency []float64, sampleRate, bins int, minHz, maxHz float64) []float64 {
	out := make([]float64, max(0, bins))
	if len(frequency) < 16 || sampleRate < 8_000 || bins < 2 || minHz < 0 || maxHz <= minHz {
		return out
	}
	decimation := 1
	switch {
	case sampleRate >= 43_000:
		decimation = 4
	case sampleRate >= 23_000:
		decimation = 2
	}
	effectiveRate := sampleRate / decimation
	const fftSize = 2048.0
	const adjacent = 15.0
	const centre = 30.0
	const ceiling = 720.0
	occupancy := make([]float64, 2048)
	for i := 0; i < len(frequency); i += decimation {
		f := frequency[i]
		if f < minHz || f > maxHz || math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		index := int(f * fftSize / float64(effectiveRate))
		if index < 1 || index >= len(occupancy)-1 {
			continue
		}
		if occupancy[index] < ceiling {
			occupancy[index] = math.Min(ceiling, occupancy[index]+centre)
			occupancy[index-1] += adjacent
			occupancy[index+1] += adjacent
		}
	}
	maximum := 0.0
	for _, value := range occupancy {
		if value > maximum {
			maximum = value
		}
	}
	if maximum <= 0 {
		return out
	}
	for i := range out {
		f := minHz + float64(i)*(maxHz-minHz)/float64(bins-1)
		index := int(f * fftSize / float64(effectiveRate))
		if index >= 0 && index < len(occupancy) {
			out[i] = math.Min(1, occupancy[index]/maximum)
		}
	}
	return out
}

// BuildAFCFrameWaterfall creates a complete, frame-aligned waterfall from the
// exact discriminator track and AFC correction points used by the decoder.
// Each image row represents one SSTV raster-line time interval; x represents
// standard corrected audio frequency from minHz to maxHz.
func BuildAFCFrameWaterfall(frequency []float64, sampleRate int, spec ModeSpec, bodyStart int, offsets []AFCOffsetPoint, width int, minHz, maxHz float64) *image.Gray {
	if sampleRate < 8_000 || spec.Height <= 0 || bodyStart < 0 || bodyStart >= len(frequency) || width < 2 || maxHz <= minHz {
		return nil
	}
	bodyEnd := min(len(frequency), bodyStart+int(math.Round(BodyDurationMS(spec)*float64(sampleRate)/1000)))
	if bodyEnd <= bodyStart {
		return nil
	}
	img := image.NewGray(image.Rect(0, 0, width, spec.Height))
	offsetIndex := 0
	currentOffset := 0.0
	for row := 0; row < spec.Height; row++ {
		start := bodyStart + (bodyEnd-bodyStart)*row/spec.Height
		end := bodyStart + (bodyEnd-bodyStart)*(row+1)/spec.Height
		if end <= start {
			continue
		}
		hist := make([]float64, width)
		for sample := start; sample < end; sample++ {
			for offsetIndex < len(offsets) && offsets[offsetIndex].SampleIndex <= sample {
				currentOffset = offsets[offsetIndex].OffsetHz
				offsetIndex++
			}
			f := frequency[sample]
			if f <= 80 || f >= 3_900 || math.IsNaN(f) || math.IsInf(f, 0) {
				continue
			}
			f -= currentOffset
			if f < minHz || f > maxHz {
				continue
			}
			x := int(math.Round((f-minHz)*float64(width-1)/(maxHz-minHz)))
			if x >= 0 && x < width {
				hist[x]++
			}
		}
		peak := 0.0
		for _, value := range hist {
			if value > peak {
				peak = value
			}
		}
		if peak <= 0 {
			continue
		}
		for x, value := range hist {
			// Square-root compression keeps weak, narrow tones visible while
			// retaining strong sync and porch structures without saturation.
			intensity := math.Sqrt(value / peak)
			img.SetGray(x, row, color.Gray{Y: uint8(math.Round(255 * intensity))})
		}
	}
	return img
}


// BuildAFCFrameSpectrum creates a frame-wide spectrum using the same per-sample
// AFC correction points that were used to reconstruct the completed image.
func BuildAFCFrameSpectrum(frequency []float64, sampleRate int, spec ModeSpec, bodyStart int, offsets []AFCOffsetPoint, bins int, minHz, maxHz float64) []float64 {
	if sampleRate < 8_000 || spec.Height <= 0 || bodyStart < 0 || bodyStart >= len(frequency) || bins < 2 || maxHz <= minHz {
		return make([]float64, max(0, bins))
	}
	bodyEnd := min(len(frequency), bodyStart+int(math.Round(BodyDurationMS(spec)*float64(sampleRate)/1000)))
	if bodyEnd <= bodyStart {
		return make([]float64, bins)
	}
	corrected := make([]float64, bodyEnd-bodyStart)
	offsetIndex := 0
	currentOffset := 0.0
	for sample := bodyStart; sample < bodyEnd; sample++ {
		for offsetIndex < len(offsets) && offsets[offsetIndex].SampleIndex <= sample {
			currentOffset = offsets[offsetIndex].OffsetHz
			offsetIndex++
		}
		if f := frequency[sample]; f > 80 && f < 3_900 && !math.IsNaN(f) && !math.IsInf(f, 0) {
			corrected[sample-bodyStart] = f - currentOffset
		}
	}
	return FrequencyHistogramFromTrack(corrected, sampleRate, bins, minHz, maxHz)
}
