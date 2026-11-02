package audio

import (
	"math"
	"time"
)

const (
	DefaultAttenuatorTargetDBFS = -3.0
	DefaultAttenuatorMaxDB      = 24.0
)

type AutoAttenuatorConfig struct {
	Enabled          bool
	TargetDBFS       float64
	MaxAttenuationDB float64
	Release          time.Duration
}

func DefaultAutoAttenuatorConfig() AutoAttenuatorConfig {
	return AutoAttenuatorConfig{
		Enabled: true, TargetDBFS: DefaultAttenuatorTargetDBFS,
		MaxAttenuationDB: DefaultAttenuatorMaxDB, Release: 500 * time.Millisecond,
	}
}

type AutoAttenuatorStats struct {
	Gain                float64
	AttenuationDB       float64
	InputPeak           float64
	OutputPeak          float64
	InputClippedSamples uint64
}

// AutoAttenuator applies a feed-forward, linear gain stage. It measures the
// complete capture block before applying gain, so overload protection does not
// introduce clipping or nonlinear harmonics into SSTV tones. Gain reduction is
// immediate; recovery is smoothed and never exceeds unity.
type AutoAttenuator struct {
	sampleRate int
	config     AutoAttenuatorConfig
	gain       float64
}

func NewAutoAttenuator(sampleRate int, config AutoAttenuatorConfig) *AutoAttenuator {
	if sampleRate <= 0 {
		sampleRate = 48_000
	}
	if config.TargetDBFS == 0 || config.TargetDBFS < -60 || config.TargetDBFS >= 0 {
		config.TargetDBFS = DefaultAttenuatorTargetDBFS
	}
	if config.MaxAttenuationDB < 0 || config.MaxAttenuationDB > 60 {
		config.MaxAttenuationDB = DefaultAttenuatorMaxDB
	}
	if config.Release <= 0 {
		config.Release = 500 * time.Millisecond
	}
	return &AutoAttenuator{sampleRate: sampleRate, config: config, gain: 1}
}

func (a *AutoAttenuator) Reset() {
	if a != nil {
		a.gain = 1
	}
}

func (a *AutoAttenuator) Process(samples []float32) ([]float32, AutoAttenuatorStats) {
	stats := AutoAttenuatorStats{Gain: 1}
	if a == nil || len(samples) == 0 {
		return append([]float32(nil), samples...), stats
	}
	peak := 0.0
	for _, sample := range samples {
		v := math.Abs(float64(sample))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v > peak {
			peak = v
		}
		if v >= 0.999 {
			stats.InputClippedSamples++
		}
	}
	stats.InputPeak = peak

	gain := a.gain
	if a.config.Enabled {
		target := math.Pow(10, a.config.TargetDBFS/20)
		desired := 1.0
		if peak > target && peak > 0 {
			desired = target / peak
		}
		minGain := math.Pow(10, -a.config.MaxAttenuationDB/20)
		if desired < minGain {
			desired = minGain
		}
		if desired < gain {
			gain = desired
		} else {
			duration := float64(len(samples)) / float64(a.sampleRate)
			alpha := 1 - math.Exp(-duration/a.config.Release.Seconds())
			gain += (desired - gain) * alpha
		}
		gain = math.Max(minGain, math.Min(1, gain))
	} else {
		gain = 1
	}
	a.gain = gain

	out := make([]float32, len(samples))
	for i, sample := range samples {
		v := float64(sample)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			v = 0
		}
		v *= gain
		out[i] = float32(v)
		if math.Abs(v) > stats.OutputPeak {
			stats.OutputPeak = math.Abs(v)
		}
	}
	stats.Gain = gain
	if gain > 0 {
		stats.AttenuationDB = -20 * math.Log10(gain)
	}
	return out, stats
}
