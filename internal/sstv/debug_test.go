package sstv

import (
	"fmt"
	"os"
	"testing"
)

func TestDebugCaptureBreak(t *testing.T) {
	f, err := os.Open("../../capture.wav")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	samples, rate, err := ReadWAV(f)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("rate=%d samples=%d dur=%.2fs\n", rate, len(samples), float64(len(samples))/float64(rate))

	sigStart := -1
	for i, s := range samples {
		if s != 0 {
			sigStart = i
			break
		}
	}
	fmt.Printf("first non-zero sample at %.3fs\n", float64(sigStart)/float64(rate))

	// Fine frequency profile every 10ms from signal start.
	seg := samples[sigStart:]
	for off := 0; off+rate/50 < len(seg); off += rate / 100 { // 10ms step, 20ms window
		win := seg[off : off+rate/50]
		bt := bestTone(win, rate, 1900, 1200, 1300)
		if off%(rate/10) == 0 { // print every 100ms
			fmt.Printf("  +%5.0fms freq=%4.0fHz\n", float64(off)/float64(rate)*1000, bt)
		}
	}
	_ = seg
}
