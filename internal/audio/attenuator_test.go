package audio

import (
	"math"
	"testing"
	"time"
)

func TestAutoAttenuatorReducesPeakWithoutChangingToneCrossings(t *testing.T) {
	const rate = 48_000
	samples := make([]float32, 480)
	for i := range samples {
		samples[i] = float32(1.25 * math.Sin(2*math.Pi*1500*float64(i)/rate))
	}
	out, stats := NewAutoAttenuator(rate, DefaultAutoAttenuatorConfig()).Process(samples)
	if stats.Gain >= 1 {
		t.Fatalf("expected gain reduction, got %.4f", stats.Gain)
	}
	if stats.OutputPeak > math.Pow(10, -3.0/20)+0.005 {
		t.Fatalf("output peak %.4f exceeds target", stats.OutputPeak)
	}
	crossings := 0
	for i := 1; i < len(out); i++ {
		if (out[i-1] < 0 && out[i] >= 0) || (out[i-1] >= 0 && out[i] < 0) {
			crossings++
		}
	}
	if crossings < 28 || crossings > 32 {
		t.Fatalf("attenuator changed tone crossing count: %d", crossings)
	}
}

func TestAutoAttenuatorReleaseRecoversSmoothly(t *testing.T) {
	a := NewAutoAttenuator(48_000, DefaultAutoAttenuatorConfig())
	loud := make([]float32, 480)
	for i := range loud {
		loud[i] = 0.95
	}
	_, reduced := a.Process(loud)
	if reduced.Gain >= 1 {
		t.Fatal("loud block did not reduce gain")
	}
	quiet := make([]float32, 480)
	previous := reduced.Gain
	for i := 0; i < 100; i++ {
		_, stats := a.Process(quiet)
		if stats.Gain < previous || stats.Gain > 1 {
			t.Fatalf("release gain is not monotonic/bounded: %.4f after %.4f", stats.Gain, previous)
		}
		previous = stats.Gain
	}
	if previous < 0.9 {
		t.Fatalf("gain did not recover: %.4f", previous)
	}
}

func TestAutoAttenuatorPreservesInputAndReportsClipping(t *testing.T) {
	input := []float32{0.2, 1, -1, 0.5, -0.9995}
	original := append([]float32(nil), input...)
	out, stats := NewAutoAttenuator(48_000, DefaultAutoAttenuatorConfig()).Process(input)
	if stats.InputClippedSamples != 3 {
		t.Fatalf("clipped sample count = %d, want 3", stats.InputClippedSamples)
	}
	if stats.OutputPeak >= 1 {
		t.Fatalf("output still reaches full scale: %.4f", stats.OutputPeak)
	}
	for i := range input {
		if input[i] != original[i] {
			t.Fatalf("input was modified at sample %d", i)
		}
		if math.Abs(float64(out[i])) > 1 {
			t.Fatalf("output exceeds full scale at sample %d", i)
		}
	}
}

func TestAutoAttenuatorDisabledIsUnityGain(t *testing.T) {
	cfg := DefaultAutoAttenuatorConfig()
	cfg.Enabled = false
	input := []float32{0.1, -0.4, 1.2}
	out, stats := NewAutoAttenuator(48_000, cfg).Process(input)
	if stats.Gain != 1 || stats.AttenuationDB != 0 {
		t.Fatalf("disabled attenuator applied gain: %+v", stats)
	}
	for i := range input {
		if out[i] != input[i] {
			t.Fatalf("disabled output[%d] = %f, want %f", i, out[i], input[i])
		}
	}
}

func TestAutoAttenuatorResetRestoresUnity(t *testing.T) {
	a := NewAutoAttenuator(48_000, DefaultAutoAttenuatorConfig())
	loud := make([]float32, 480)
	for i := range loud {
		loud[i] = 1
	}
	_, reduced := a.Process(loud)
	if reduced.Gain >= 1 {
		t.Fatal("expected gain reduction before reset")
	}
	a.Reset()
	_, reset := a.Process(make([]float32, 480))
	if reset.Gain != 1 {
		t.Fatalf("reset gain = %.4f, want unity", reset.Gain)
	}
	_ = time.Second // keep this test file's import stable if release timing changes.
}
