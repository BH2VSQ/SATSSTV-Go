package sstv

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// shiftFrequency shifts a narrowband real signal by shiftHz using the analytic
// signal (Hilbert FIR) so an end-to-end mistuned-receiver scenario can be
// tested without a radio.
func shiftFrequency(samples []int16, rate int, shiftHz float64) []int16 {
	const n = 15
	hilb := make([]float64, n)
	for i := 0; i < n; i++ {
		m := i - n/2
		if m%2 != 0 {
			hilb[i] = 2.0 / (math.Pi * float64(m))
		}
		hilb[i] *= 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
	}
	delay := n / 2
	out := make([]int16, len(samples))
	for i := 0; i < len(samples); i++ {
		var xi float64
		if i >= delay {
			xi = float64(samples[i-delay]) / 32768
		}
		var xq float64
		for k := 0; k < n; k++ {
			if idx := i - k; idx >= 0 {
				xq += float64(samples[idx]) / 32768 * hilb[k]
			}
		}
		phi := 2 * math.Pi * shiftHz * float64(i) / float64(rate)
		re := xi*math.Cos(phi) - xq*math.Sin(phi)
		if re > 1 {
			re = 1
		} else if re < -1 {
			re = -1
		}
		out[i] = int16(math.Round(re * 32767))
	}
	return out
}

func TestDecodeToleratesFrequencyOffset(t *testing.T) {
	const rate = 48_000
	for _, offset := range []float64{-120, -60, 0, 60, 120} {
		t.Run(fmt.Sprintf("offset_%+.0fHz", offset), func(t *testing.T) {
			encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
			if err != nil {
				t.Fatal(err)
			}
			shifted := shiftFrequency(encoded, rate, offset)
			detected, vis, _, err := DetectModeHeader(shifted, rate)
			if err != nil {
				t.Fatalf("detect mode header: %v", err)
			}
			if detected.ID != Robot36ID || vis != 8 {
				t.Fatalf("detected %s / VIS %d, want robot36 / 8", detected.ID, vis)
			}
			decoded, err := Decode(context.Background(), shifted, rate)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Mode.ID != Robot36ID {
				t.Fatalf("auto-detected %s", decoded.Mode.ID)
			}
			want := resizeCropToRGB(makeTestSSTVImage(), 320, 240)
			var total float64
			var count int
			for y := 8; y < 232; y += 8 {
				for x := 8; x < 312; x += 8 {
					got := decoded.Image.NRGBAAt(x, y)
					w := want[y][x]
					total += math.Abs(float64(got.R)-float64(w.R)) + math.Abs(float64(got.G)-float64(w.G)) + math.Abs(float64(got.B)-float64(w.B))
					count += 3
				}
			}
			mae := total / float64(count)
			t.Logf("offset %+.0f Hz MAE=%.1f", offset, mae)
			if mae > 45 {
				t.Errorf("offset %+.0f Hz MAE %.1f exceeds 45", offset, mae)
			}
		})
	}
}
