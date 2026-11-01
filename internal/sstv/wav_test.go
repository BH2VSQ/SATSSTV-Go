package sstv

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
)

func analyzeWAV(t *testing.T, name string) {
	f, err := os.Open("../../" + name)
	if err != nil {
		t.Skipf("%s not found: %v", name, err)
	}
	defer f.Close()
	samples, rate, err := ReadWAV(f)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	// RMS level.
	var sum float64
	for _, s := range samples {
		x := float64(s) / 32768
		sum += x * x
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	fmt.Printf("\n=== %s: rate=%d samples=%d dur=%.1fs rms=%.4f (%.1f dBFS) ===\n", name, rate, len(samples), float64(len(samples))/float64(rate), rms, 20*math.Log10(rms+1e-12))

	if spec, vis, bodyStart, err := DetectModeHeader(samples, rate); err != nil {
		fmt.Printf("  DetectModeHeader: %v\n", err)
	} else {
		fmt.Printf("  DetectModeHeader: mode=%s VIS=%d bodyStart=%d\n", spec.ID, vis, bodyStart)
	}
	if result, err := Decode(context.Background(), samples, rate); err != nil {
		fmt.Printf("  Decode: %v\n", err)
	} else {
		fmt.Printf("  Decode: mode=%s size=%dx%d\n", result.Mode.ID, result.Image.Bounds().Dx(), result.Image.Bounds().Dy())
	}

	freq := instantaneousFrequency(samples, rate)
	for ms := 200; ms <= 2000; ms += 200 {
		idx := ms * rate / 1000
		if idx < len(freq) {
			fmt.Printf("  t=%dms freq=%.0fHz\n", ms, freq[idx])
		}
	}
}

func TestAnalyzeCaptureWav(t *testing.T) {
	analyzeWAV(t, "capture.wav")
}

func TestAnalyzeSSTVWav(t *testing.T) {
	analyzeWAV(t, "SSTV.wav")
}
