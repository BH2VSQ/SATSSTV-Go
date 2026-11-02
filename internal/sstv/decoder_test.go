package sstv

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
	"time"
)

func makeTestSSTVImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 96, 72))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / (img.Bounds().Dx() - 1)),
				G: uint8(y * 255 / (img.Bounds().Dy() - 1)),
				B: uint8((x + y) * 255 / (img.Bounds().Dx() + img.Bounds().Dy() - 2)),
				A: 255,
			})
		}
	}
	return img
}

func TestDecodeModeRoundTripAcrossFamilies(t *testing.T) {
	for _, id := range []string{"robot36", "robot72", "martin_m1", "scottie_s1", "pd120", "sc2_180", "pasokon_p3"} {
		t.Run(id, func(t *testing.T) {
			const rate = 48_000
			encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), id)
			if err != nil {
				t.Fatal(err)
			}
			spec, _ := LookupMode(id)
			vis, bodyStart, err := DetectVIS(encoded, rate)
			if err != nil {
				t.Fatal(err)
			}
			if vis != spec.VIS || bodyStart != int(float64(rate)*0.910) {
				t.Fatalf("VIS detection = %d at %d, want %d", vis, bodyStart, spec.VIS)
			}
			decoded, err := DecodeMode(context.Background(), encoded, rate, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() != spec.Width || decoded.Bounds().Dy() != spec.Height {
				t.Fatalf("decoded bounds = %v, want %dx%d", decoded.Bounds(), spec.Width, spec.Height)
			}
			// Check visible image structure as well as frame dimensions. The analog
			// tone demodulator has small quantization/PLL error but should retain
			// the smooth gradient in the source image.
			want := resizeCropToRGB(makeTestSSTVImage(), spec.Width, spec.Height)
			var total float64
			for y := 4; y < spec.Height-4; y += 4 {
				for x := 4; x < spec.Width-4; x += 4 {
					got := decoded.NRGBAAt(x, y)
					w := want[y][x]
					total += math.Abs(float64(got.R)-float64(w.R)) + math.Abs(float64(got.G)-float64(w.G)) + math.Abs(float64(got.B)-float64(w.B))
				}
			}
			pixels := float64(((spec.Height - 8) / 4) * ((spec.Width - 8) / 4) * 3)
			mae := total / pixels
			t.Logf("mean absolute RGB channel error = %.1f", mae)
			if mae > 45 {
				t.Errorf("decoded image error %.1f exceeds the coarse round-trip limit", mae)
			}
		})
	}
}

func TestDecodeAutomaticallyDetectsVIS(t *testing.T) {
	const rate = 48_000
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), "robot36")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Decode(context.Background(), encoded, rate)
	if err != nil {
		t.Fatal(err)
	}
	if result.VIS != 8 || result.Mode.ID != Robot36ID {
		t.Fatalf("detected mode = %s / VIS %d, want Robot 36 / VIS 8", result.Mode.ID, result.VIS)
	}
}

func TestDetectAllModeHeaderFamiliesAt48k(t *testing.T) {
	const rate = 48_000
	for _, spec := range Modes() {
		if !spec.CodecReady {
			continue
		}
		t.Run(spec.ID, func(t *testing.T) {
			g, err := NewToneGenerator(rate, 0.35)
			if err != nil {
				t.Fatal(err)
			}
			switch spec.Header {
			case HeaderVIS:
				err = WriteVIS(g, spec.VIS)
			case HeaderMMSSTV:
				err = WriteMMSSTVHeader(g, spec.ExtendedVIS)
			case HeaderNarrowVIS:
				err = WriteNarrowVIS(g, spec.NarrowVIS)
			case HeaderAVT:
				err = WriteAVT90Header(g)
			default:
				t.Fatalf("unsupported header family %q", spec.Header)
			}
			if err != nil {
				t.Fatalf("write header: %v", err)
			}
			samples := append(g.Samples(), make([]int16, rate*2)...)
			detected, _, _, err := DetectModeHeader(samples, rate)
			if err != nil {
				t.Fatalf("detect header: %v", err)
			}
			if detected.ID != spec.ID {
				t.Fatalf("detected %q, want %q", detected.ID, spec.ID)
			}
		})
	}
}

func TestStreamDecoderReceivesRobot36At48k10msBlocks(t *testing.T) {
	const rate = 48_000
	const chunk = rate / 100 // 10 ms native audio callback blocks.
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	// Include an arbitrary leading offset and a short quiet interval, as a live
	// receiver does not necessarily start exactly on the VIS leader boundary.
	samples := append(make([]int16, 317), make([]int16, rate/2)...)
	samples = append(samples, encoded...)
	for _, fixedMode := range []string{"", Robot36ID} {
		name := "auto"
		if fixedMode != "" {
			name = "fixed"
		}
		t.Run(name, func(t *testing.T) {
			decoder, err := NewStreamDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			if err := decoder.SetMode(fixedMode); err != nil {
				t.Fatal(err)
			}
			var events []StreamEvent
			for start := 0; start < len(samples); start += chunk {
				end := min(start+chunk, len(samples))
				batch, feedErr := decoder.Feed(context.Background(), samples[start:end])
				decoder.DrainUpdates()
				if feedErr != nil {
					t.Fatalf("feed at sample %d: %v", start, feedErr)
				}
				events = append(events, batch...)
			}
			if len(events) != 1 || events[0].Mode.ID != Robot36ID {
				t.Fatalf("received %+v, want one Robot 36 image", events)
			}
		})
	}
}

func TestStreamDecoderReceivesFrameInChunks(t *testing.T) {
	const rate = 48_000
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	for start := 0; start < len(encoded); start += 4096 {
		end := min(start+4096, len(encoded))
		got, feedErr := decoder.Feed(context.Background(), encoded[start:end])
		decoder.DrainUpdates()
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		events = append(events, got...)
	}
	if len(events) != 1 {
		t.Fatalf("received %d complete frames, want one", len(events))
	}
	if events[0].VIS != 8 || events[0].Mode.ID != Robot36ID {
		t.Fatalf("stream event = %+v", events[0])
	}
	if events[0].Image.Bounds().Dx() != 320 || events[0].Image.Bounds().Dy() != 240 {
		t.Fatalf("unexpected frame size %v", events[0].Image.Bounds())
	}
}

func TestStreamDecoderIdle1900HzLeaderToneStaysRealtime(t *testing.T) {
	const rate = 48_000
	samples := make([]int16, rate*3)
	for i := range samples {
		samples[i] = int16(0.30 * 32767 * math.Sin(2*math.Pi*1900*float64(i)/float64(rate)))
	}
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for offset := 0; offset < len(samples); offset += 480 {
		end := min(offset+480, len(samples))
		if _, err := decoder.Feed(context.Background(), samples[offset:end]); err != nil {
			t.Fatalf("feed idle leader tone at %d: %v", offset, err)
		}
	}
	elapsed := time.Since(started)
	t.Logf("3 seconds of idle 1900 Hz leader audio processed in %s", elapsed)
	if elapsed >= 2*time.Second {
		t.Fatalf("3 seconds of idle 1900 Hz audio took %s; VIS search must remain real-time", elapsed)
	}
}

func TestStreamDecoderIdle1500HzToneStaysRealtime(t *testing.T) {
	const rate = 48_000
	const durationSeconds = 3
	samples := make([]int16, rate*durationSeconds)
	for i := range samples {
		samples[i] = int16(0.35 * 32767 * math.Sin(2*math.Pi*1500*float64(i)/float64(rate)))
	}
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for offset := 0; offset < len(samples); offset += 1024 {
		end := min(offset+1024, len(samples))
		if _, err := decoder.Feed(context.Background(), samples[offset:end]); err != nil {
			t.Fatalf("feed idle tone at %d: %v", offset, err)
		}
	}
	elapsed := time.Since(started)
	t.Logf("3 seconds of idle 1500 Hz audio processed in %s", elapsed)
	if elapsed >= 2*time.Second {
		t.Fatalf("3 seconds of idle 1500 Hz audio took %s to process; receiver must stay below realtime budget", elapsed)
	}
}

func TestStreamDecoderReceivesAllImplementedModesInChunks(t *testing.T) {
	// Exercise the live path, not only Decode/DecodeMode. The production RX
	// worker feeds small arbitrary blocks and must preserve detector state for
	// every standard, extended, narrow-band and AVT header family.
	const rate = 8_000
	source := makeTestSSTVImage()
	encoder := NewEncoder(rate)
	for _, spec := range Modes() {
		if !spec.CodecReady {
			continue
		}
		t.Run(spec.ID, func(t *testing.T) {
			samples, err := encoder.Encode(context.Background(), source, spec.ID)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			decoder, err := NewStreamDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			var events []StreamEvent
			for offset := 0; offset < len(samples); offset += 2048 {
				end := min(offset+2048, len(samples))
				batch, feedErr := decoder.Feed(context.Background(), samples[offset:end])
				decoder.DrainUpdates()
				if feedErr != nil {
					t.Fatalf("feed at sample %d: %v", offset, feedErr)
				}
				events = append(events, batch...)
			}
			if len(events) != 1 {
				t.Fatalf("stream returned %d frames, want exactly one", len(events))
			}
			if events[0].Mode.ID != spec.ID {
				t.Fatalf("stream detected %q, want %q", events[0].Mode.ID, spec.ID)
			}
			if bounds := events[0].Image.Bounds(); bounds.Dx() != spec.Width || bounds.Dy() != spec.Height {
				t.Fatalf("decoded bounds = %v, want %dx%d", bounds, spec.Width, spec.Height)
			}
		})
	}
}

func TestStreamDecoderFindsVISAfterLongIdle(t *testing.T) {
	const rate = 8_000
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	// Hold a silent receiver open long enough to roll the VIS search window.
	for sent := 0; sent < rate*4; sent += 512 {
		if _, err := decoder.Feed(context.Background(), make([]int16, min(512, rate*4-sent))); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	for start := 0; start < len(encoded); start += 2048 {
		end := min(start+2048, len(encoded))
		batch, feedErr := decoder.Feed(context.Background(), encoded[start:end])
		decoder.DrainUpdates()
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		events = append(events, batch...)
	}
	if len(events) != 1 || events[0].Mode.ID != Robot36ID {
		t.Fatalf("received %d events after idle, want one Robot 36 frame", len(events))
	}
}

func TestRobot72CatalogAndTiming(t *testing.T) {
	spec, err := LookupMode("r72")
	if err != nil {
		t.Fatal(err)
	}
	if spec.VIS != 12 || spec.Width != 320 || spec.Height != 240 || !spec.RobotSequential {
		t.Fatalf("unexpected Robot 72 profile: %+v", spec)
	}
	if got := BodyDurationMS(spec); math.Abs(got-72_000) > 1 {
		t.Fatalf("Robot 72 body duration = %.3f ms, want 72000 ms", got)
	}
	if got, ok := ModeFromVIS(12); !ok || got.ID != "robot72" {
		t.Fatalf("VIS 12 lookup = %+v, %v", got, ok)
	}
}

func TestImplementedModesDecodeTheirOwnWaveforms(t *testing.T) {
	const standardRate = 8_000
	source := makeTestSSTVImage()
	for _, spec := range Modes() {
		if !spec.CodecReady {
			continue
		}
		t.Run(spec.ID, func(t *testing.T) {
			// N-VIS narrow-band MP carries the full 0..255 component range in
			// only 256 Hz. At 8 kHz, the short MP73 pixel dwell provides too few
			// samples for a meaningful zero-crossing frequency estimate. Exercise
			// these modes at the application's normal 48 kHz audio rate instead.
			rate := standardRate
			if spec.NarrowBand {
				rate = DefaultRate
			}
			encoder := NewEncoder(rate)
			samples, err := encoder.Encode(context.Background(), source, spec.ID)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			detected, vis, _, err := DetectModeHeader(samples, rate)
			if err != nil {
				t.Fatalf("detect mode header: %v", err)
			}
			if detected.ID != spec.ID {
				t.Fatalf("header detected %s, want %s (VIS=%d)", detected.ID, spec.ID, vis)
			}
			decoded, err := Decode(context.Background(), samples, rate)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Mode.ID != spec.ID {
				t.Fatalf("auto-detected %s, want %s", decoded.Mode.ID, spec.ID)
			}
			if got := decoded.Image.Bounds(); got.Dx() != spec.Width || got.Dy() != spec.Height {
				t.Fatalf("decoded bounds = %v, want %dx%d", got, spec.Width, spec.Height)
			}
			want := resizeCropToRGB(source, spec.Width, spec.Height)
			var total float64
			var channelCount int
			for y := 4; y < spec.Height-4; y += 8 {
				for x := 4; x < spec.Width-4; x += 8 {
					gotPixel, wantPixel := decoded.Image.NRGBAAt(x, y), want[y][x]
					total += math.Abs(float64(gotPixel.R) - float64(wantPixel.R))
					total += math.Abs(float64(gotPixel.G) - float64(wantPixel.G))
					total += math.Abs(float64(gotPixel.B) - float64(wantPixel.B))
					channelCount += 3
				}
			}
			mae := total / float64(channelCount)
			if mae > 45 {
				t.Errorf("round-trip RGB error %.1f exceeds 45", mae)
			}
		})
	}
}

func absCh(a, b uint8) uint8 {
	if a >= b {
		return a - b
	}
	return b - a
}

func TestRobotMonochromeModesRoundTrip(t *testing.T) {
	const rate = 8_000
	source := makeTestSSTVImage()
	for _, id := range []string{"bw8", "bw12"} {
		t.Run(id, func(t *testing.T) {
			spec, err := LookupMode(id)
			if err != nil {
				t.Fatal(err)
			}
			if !spec.CodecReady || !spec.Monochrome {
				t.Fatalf("mode is not marked as an implemented monochrome codec: %+v", spec)
			}
			if got := BodyDurationMS(spec); math.Abs(got-float64(spec.BodyTimeMS)) > 0.01 {
				t.Fatalf("body duration = %.3f ms, want %d ms", got, spec.BodyTimeMS)
			}
			samples, err := NewEncoder(rate).Encode(context.Background(), source, id)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			detected, vis, _, err := DetectModeHeader(samples, rate)
			if err != nil {
				t.Fatalf("detect mode header: %v", err)
			}
			if detected.ID != spec.ID {
				t.Fatalf("header detected %s, want %s (VIS=%d)", detected.ID, spec.ID, vis)
			}
			decoded, err := Decode(context.Background(), samples, rate)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Mode.ID != id {
				t.Fatalf("auto-detected mode = %q, want %q", decoded.Mode.ID, id)
			}
			for _, point := range [][2]int{{10, 10}, {spec.Width / 2, spec.Height / 2}, {spec.Width - 11, spec.Height - 11}} {
				pixel := decoded.Image.NRGBAAt(point[0], point[1])
				if pixel.R != pixel.G || pixel.G != pixel.B {
					t.Fatalf("decoded pixel (%d,%d) is not grayscale: %+v", point[0], point[1], pixel)
				}
			}
		})
	}
}

func TestStreamDecoderFixedModeSkipsWholeMismatchedFrame(t *testing.T) {
	const rate = 8_000
	encoder := NewEncoder(rate)
	first, err := encoder.Encode(context.Background(), makeTestSSTVImage(), "bw8")
	if err != nil {
		t.Fatal(err)
	}
	second, err := encoder.Encode(context.Background(), makeTestSSTVImage(), "bw12")
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoder.SetMode("bw12"); err != nil {
		t.Fatal(err)
	}
	combined := make([]int16, 0, len(first)+len(second))
	combined = append(combined, first...)
	combined = append(combined, second...)
	var events []StreamEvent
	for offset := 0; offset < len(combined); offset += 4096 {
		end := min(offset+4096, len(combined))
		batch, feedErr := decoder.Feed(context.Background(), combined[offset:end])
		if feedErr != nil {
			t.Fatalf("feed combined stream: %v", feedErr)
		}
		events = append(events, batch...)
	}
	if len(events) != 1 || events[0].Mode.ID != "bw12" {
		t.Fatalf("fixed bw12 receiver emitted %+v, want exactly one bw12 frame", events)
	}
}

func TestStreamDecoderFixedModeFallsBackWhenVISBitsAreDamaged(t *testing.T) {
	const rate = 8_000
	const modeID = "robot36"
	spec, err := LookupMode(modeID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), modeID)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuild the 610 ms synchronization prefix and the VIS payload, but invert
	// the parity symbol. A fixed-mode receiver knows the selected raster and
	// should still lock to the valid leader/break/leader prefix.
	header, err := NewToneGenerator(rate, 0.35)
	if err != nil {
		t.Fatal(err)
	}
	for _, segment := range []struct{ hz, ms float64 }{{1900, 300}, {1200, 10}, {1900, 300}, {1200, 30}} {
		if err := header.ToneMS(segment.hz, segment.ms); err != nil {
			t.Fatal(err)
		}
	}
	ones := 0
	for bit := 0; bit < 7; bit++ {
		tone := 1_300.0
		if (spec.VIS>>bit)&1 != 0 {
			tone = 1_100
			ones++
		}
		if err := header.ToneMS(tone, 30); err != nil {
			t.Fatal(err)
		}
	}
	parityTone := 1_300.0
	if ones%2 == 0 { // Deliberately invert the required even-parity bit.
		parityTone = 1_100
	}
	if err := header.ToneMS(parityTone, 30); err != nil {
		t.Fatal(err)
	}
	if err := header.ToneMS(1_200, 30); err != nil {
		t.Fatal(err)
	}
	badHeader := header.Samples()
	bodyStart := int(math.Round(.910 * rate))
	combined := append(badHeader, encoded[bodyStart:]...)

	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoder.SetMode(modeID); err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	for start := 0; start < len(combined); start += 1024 {
		end := min(start+1024, len(combined))
		batch, feedErr := decoder.Feed(context.Background(), combined[start:end])
		if feedErr != nil {
			t.Fatalf("feed damaged-VIS frame: %v", feedErr)
		}
		events = append(events, batch...)
	}
	if len(events) != 1 || events[0].Mode.ID != modeID {
		t.Fatalf("fixed-mode fallback returned %+v, want one %s frame", events, modeID)
	}
}

func TestStreamDecoderAutoAcceptsVISWithDamagedParity(t *testing.T) {
	const rate = 48_000
	const modeID = Robot36ID
	spec, err := LookupMode(modeID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), modeID)
	if err != nil {
		t.Fatal(err)
	}
	header, err := NewToneGenerator(rate, 0.30)
	if err != nil {
		t.Fatal(err)
	}
	for _, segment := range []struct{ hz, ms float64 }{{1900, 300}, {1200, 10}, {1900, 300}, {1200, 30}} {
		if err := header.ToneMS(segment.hz, segment.ms); err != nil {
			t.Fatal(err)
		}
	}
	ones := 0
	for bit := 0; bit < 7; bit++ {
		tone := 1_300.0
		if (spec.VIS>>bit)&1 != 0 {
			tone = 1_100
			ones++
		}
		if err := header.ToneMS(tone, 30); err != nil {
			t.Fatal(err)
		}
	}
	// Invert only the parity symbol. All mode data bits and sync symbols remain
	// intact, matching a marginal real-world VIS reception.
	parityTone := 1_300.0
	if ones%2 == 0 {
		parityTone = 1_100
	}
	if err := header.ToneMS(parityTone, 30); err != nil {
		t.Fatal(err)
	}
	if err := header.ToneMS(1_200, 30); err != nil {
		t.Fatal(err)
	}
	bodyStart := int(math.Round(.910 * rate))
	combined := append(make([]int16, rate/5), header.Samples()...)
	combined = append(combined, encoded[bodyStart:]...)

	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	for start := 0; start < len(combined); start += rate / 100 {
		end := min(start+rate/100, len(combined))
		batch, feedErr := decoder.Feed(context.Background(), combined[start:end])
		if feedErr != nil {
			t.Fatalf("feed parity-damaged Auto frame: %v", feedErr)
		}
		events = append(events, batch...)
	}
	if len(events) != 1 || events[0].Mode.ID != modeID {
		t.Fatalf("Auto decoder returned %+v, want one %s frame despite bad parity", events, modeID)
	}
}

func TestStreamDecoderFixedModeFallbackSupportsMMSSTVAndNarrowVIS(t *testing.T) {
	const rate = 8_000
	for _, modeID := range []string{"mp73", "mp73_n"} {
		t.Run(modeID, func(t *testing.T) {
			spec, err := LookupMode(modeID)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), modeID)
			if err != nil {
				t.Fatalf("encode %s: %v", modeID, err)
			}
			header, err := NewToneGenerator(rate, 0.35)
			if err != nil {
				t.Fatal(err)
			}
			var headerMS float64
			switch spec.Header {
			case HeaderMMSSTV:
				// 0xffff is deliberately not a registered extended VIS code.
				if err := WriteMMSSTVHeader(header, 0xffff); err != nil {
					t.Fatal(err)
				}
				headerMS = mmsstvExtendedHeaderMS
			case HeaderNarrowVIS:
				// 0x3f is deliberately not a registered N-VIS mode code.
				if err := WriteNarrowVIS(header, 0x3f); err != nil {
					t.Fatal(err)
				}
				headerMS = narrowVISHeaderMS
			default:
				t.Fatalf("unexpected header family %q", spec.Header)
			}
			headerSamples := int(math.Round(headerMS * float64(rate) / 1000))
			if len(encoded) <= headerSamples {
				t.Fatalf("encoded %s frame is shorter than its header", modeID)
			}
			combined := append(header.Samples(), encoded[headerSamples:]...)

			decoder, err := NewStreamDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			if err := decoder.SetMode(modeID); err != nil {
				t.Fatal(err)
			}
			var events []StreamEvent
			for start := 0; start < len(combined); start += 2048 {
				end := min(start+2048, len(combined))
				batch, feedErr := decoder.Feed(context.Background(), combined[start:end])
				decoder.DrainUpdates()
				if feedErr != nil {
					t.Fatalf("feed damaged %s header: %v", modeID, feedErr)
				}
				events = append(events, batch...)
			}
			if len(events) != 1 || events[0].Mode.ID != modeID {
				t.Fatalf("fixed-mode fallback returned %+v, want one %s frame", events, modeID)
			}
		})
	}
}

func TestStreamDecoderCanReturnToAutoAfterFixedMode(t *testing.T) {
	decoder, err := NewStreamDecoder(8_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoder.SetMode("bw12"); err != nil {
		t.Fatal(err)
	}
	if err := decoder.SetMode(""); err != nil {
		t.Fatal(err)
	}
	if decoder.fixedMode != "" || decoder.skipUntil != 0 {
		t.Fatalf("Auto mode did not clear fixed-frame filtering: fixed=%q skipUntil=%d", decoder.fixedMode, decoder.skipUntil)
	}
}

func TestStreamDecoderModeChangeClearsBufferedAudio(t *testing.T) {
	decoder, err := NewStreamDecoder(8_000)
	if err != nil {
		t.Fatal(err)
	}
	decoder.pending = make([]int16, 4096)
	decoder.idleFreq = make([]float64, 4096)
	if err := decoder.SetMode("pd120"); err != nil {
		t.Fatal(err)
	}
	if len(decoder.pending) != 0 || len(decoder.idleFreq) != 0 {
		t.Fatalf("mode change retained old audio: pending=%d idleFreq=%d", len(decoder.pending), len(decoder.idleFreq))
	}
	decoder.pending = make([]int16, 1024)
	if err := decoder.SetMode(""); err != nil {
		t.Fatal(err)
	}
	if len(decoder.pending) != 0 {
		t.Fatalf("returning to Auto retained old PCM: %d samples", len(decoder.pending))
	}
}

func TestStreamDecoderDropsUndecodableFrameAndContinues(t *testing.T) {
	const rate = 8_000
	good, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), "bw8")
	if err != nil {
		t.Fatal(err)
	}
	badSpec, _ := LookupMode("bw8")
	original := modeTable[badSpec.ID]
	broken := original
	broken.Family = ModeFamily("test-invalid-family")
	modeTable[badSpec.ID] = broken
	defer func() { modeTable[badSpec.ID] = original }()

	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Feed(context.Background(), good); err == nil {
		t.Fatal("expected invalid decoder-family error")
	}
	if decoder.mode != nil || decoder.bodyStart != 0 {
		t.Fatalf("decoder retained a failed frame: mode=%+v bodyStart=%d", decoder.mode, decoder.bodyStart)
	}
	modeTable[badSpec.ID] = original
	events, err := decoder.Feed(context.Background(), good)
	if err != nil {
		t.Fatalf("decoder did not recover on next frame: %v", err)
	}
	if len(events) != 1 || events[0].Mode.ID != "bw8" {
		t.Fatalf("recovered events = %+v, want one bw8 frame", events)
	}
}

func TestSilentAudioDoesNotProduceVIS(t *testing.T) {
	const rate = 8_000
	silence := make([]int16, rate*4)
	if _, _, err := DetectVIS(silence, rate); err == nil {
		t.Fatal("digital silence must not be classified as a VIS header")
	}
	if _, _, _, err := DetectModeHeader(silence, rate); err == nil {
		t.Fatal("digital silence must not be classified as an SSTV mode header")
	}
}

func TestStreamDecoderReceivesShortFrameAfterLongIdle(t *testing.T) {
	const rate = 8_000
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	for sent := 0; sent < rate*5; sent += 512 {
		if _, err := decoder.Feed(context.Background(), make([]int16, min(512, rate*5-sent))); err != nil {
			t.Fatal(err)
		}
	}

	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), "bw8")
	if err != nil {
		t.Fatal(err)
	}
	var events []StreamEvent
	for start := 0; start < len(encoded); start += 1024 {
		end := min(start+1024, len(encoded))
		batch, feedErr := decoder.Feed(context.Background(), encoded[start:end])
		decoder.DrainUpdates()
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		events = append(events, batch...)
	}
	if len(events) != 1 || events[0].Mode.ID != "bw8" {
		t.Fatalf("received %d events after long idle, want one B/W 8 frame", len(events))
	}
}

func TestStreamDecoderKeepsExtendedAndNarrowVISHeadersAcrossChunks(t *testing.T) {
	const rate = 8_000
	tests := []struct {
		name  string
		mode  string
		write func(*ToneGenerator) error
	}{
		{
			name:  "MMSSTV extended VIS",
			mode:  "mp73",
			write: func(g *ToneGenerator) error { return WriteMMSSTVHeader(g, 0x2523) },
		},
		{
			name:  "N-VIS",
			mode:  "mp73_n",
			write: func(g *ToneGenerator) error { return WriteNarrowVIS(g, 0x02) },
		},
		{
			name:  "AVT90 digital countdown",
			mode:  "avt90",
			write: WriteAVT90Header,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator, err := NewToneGenerator(rate, DefaultLevel)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.write(generator); err != nil {
				t.Fatal(err)
			}
			decoder, err := NewStreamDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			samples := generator.Samples()
			for start := 0; start < len(samples); start += 1024 {
				end := min(start+1024, len(samples))
				if _, err := decoder.Feed(context.Background(), samples[start:end]); err != nil {
					t.Fatal(err)
				}
				decoder.DrainUpdates()
			}
			got, active := decoder.ActiveMode()
			if !active || got.ID != tt.mode {
				t.Fatalf("stream header resolved to %q (active=%v), want %q", got.ID, active, tt.mode)
			}
		})
	}
}

func TestRobotColourSeparatorsIdentifyCrAndCb(t *testing.T) {
	const rate = 48_000
	for _, tc := range []struct {
		mode     string
		firstMS  float64
		secondMS float64
	}{
		{mode: "robot24", firstMS: 100, secondMS: 150},
		{mode: "robot72", firstMS: 150, secondMS: 225},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			samples, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			_, _, bodyStart, err := DetectModeHeader(samples, rate)
			if err != nil {
				t.Fatalf("detect mode header: %v", err)
			}
			separatorSamples := int(math.Round(4.5 * float64(rate) / 1000))
			for _, marker := range []struct {
				label string
				atMS  float64
				want  float64
			}{
				{label: "Cr marker", atMS: tc.firstMS, want: VideoMinHz},
				{label: "Cb marker", atMS: tc.secondMS, want: VideoMaxHz},
			} {
				start := bodyStart + int(math.Round(marker.atMS*float64(rate)/1000))
				end := min(start+separatorSamples, len(samples))
				got := bestTone(samples[start:end], rate, VideoMinHz, VideoMaxHz)
				if got != marker.want {
					t.Errorf("%s = %.0f Hz, want %.0f Hz", marker.label, got, marker.want)
				}
			}
		})
	}
}

func TestNarrowBandMPUsesNarrowSyncAndPorch(t *testing.T) {
	const rate = 48_000
	for _, tc := range []struct {
		mode      string
		wantSync  float64
		wantPorch float64
	}{
		{mode: "mp73", wantSync: HorizontalSync, wantPorch: VideoMinHz},
		{mode: "mp73_n", wantSync: visLeaderHz, wantPorch: NarrowVideoMinHz},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			samples, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			_, _, bodyStart, err := DetectModeHeader(samples, rate)
			if err != nil {
				t.Fatalf("detect mode header: %v", err)
			}
			syncSamples := int(math.Round(9 * float64(rate) / 1000))
			porchSamples := int(math.Round(float64(rate) / 1000))
			gotSync := bestTone(samples[bodyStart:bodyStart+syncSamples], rate, HorizontalSync, visLeaderHz)
			porchStart := bodyStart + syncSamples
			gotPorch := bestTone(samples[porchStart:porchStart+porchSamples], rate, VideoMinHz, NarrowVideoMinHz)
			if gotSync != tc.wantSync {
				t.Errorf("line sync = %.0f Hz, want %.0f Hz", gotSync, tc.wantSync)
			}
			if gotPorch != tc.wantPorch {
				t.Errorf("line porch = %.0f Hz, want %.0f Hz", gotPorch, tc.wantPorch)
			}
		})
	}
}

func TestStreamUpdateDecodeDoesNotOverwriteFrequencyBufferTail(t *testing.T) {
	const rate = 8_000
	mode, err := LookupMode(Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	frequency := make([]float64, 64, rate*2)
	backing := frequency[:cap(frequency)]
	for i := range backing {
		backing[i] = VideoCenterHz
	}
	for i := len(frequency); i < len(backing); i++ {
		backing[i] = 98765.0 // sentinel beyond the published immutable prefix
	}
	update := StreamUpdate{
		Mode: mode, Rows: 1, TotalRows: mode.Height, SampleRate: rate,
		BodyStart: 0, FrequencyLength: len(frequency), Offset: 0,
	}
	if _, err := update.DecodeWithFrequency(context.Background(), frequency); err != nil {
		t.Fatalf("decode one-row preview: %v", err)
	}
	for i := len(frequency); i < len(backing); i++ {
		if backing[i] != 98765.0 {
			t.Fatalf("preview padding overwrote shared frequency buffer at index %d: %.1f", i, backing[i])
		}
	}
}

func TestStreamDecoderEmitsProgressiveImagesBeforeFrameCompletion(t *testing.T) {
	const rate = 48_000
	for _, modeID := range []string{Robot36ID, "martin_m1", "pd120"} {
		t.Run(modeID, func(t *testing.T) {
			encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), modeID)
			if err != nil {
				t.Fatal(err)
			}
			decoder, err := NewStreamDecoder(rate)
			if err != nil {
				t.Fatal(err)
			}
			var updateCount int
			var lastRows int
			var midpoint *StreamUpdate
			var final []StreamEvent
			for start := 0; start < len(encoded); start += 4096 {
				end := min(start+4096, len(encoded))
				events, feedErr := decoder.Feed(context.Background(), encoded[start:end])
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				for _, update := range decoder.DrainUpdates() {
					updateCount++
					if update.Image != nil {
						t.Fatal("Feed decoded preview pixels synchronously; preview work must stay off the audio path")
					}
					if len(update.FrequencyChunks) == 0 || update.FrequencyLength <= 0 || update.SampleRate != rate {
						t.Fatalf("preview request is missing an immutable frequency chunk snapshot: %+v", update)
					}
					if update.Rows <= lastRows || update.Rows >= update.TotalRows {
						t.Fatalf("progressive row count did not advance monotonically: previous=%d update=%d/%d", lastRows, update.Rows, update.TotalRows)
					}
					if update.Rows-lastRows > max(8, update.TotalRows/30) {
						t.Fatalf("progressive update gap too large: previous=%d update=%d/%d", lastRows, update.Rows, update.TotalRows)
					}
					lastRows = update.Rows
					if midpoint == nil && update.Rows >= update.TotalRows/2 {
						copyOfUpdate := update
						midpoint = &copyOfUpdate
					}
				}
				final = append(final, events...)
			}
			if updateCount < 20 {
				t.Fatalf("got %d progressive updates, want at least 20 before frame completion", updateCount)
			}
			if len(final) != 1 {
				t.Fatalf("got %d final frames, want one", len(final))
			}
			if midpoint == nil {
				t.Fatal("no progressive update reached half the image height")
			}
			previewImage, decodeErr := midpoint.Decode(context.Background())
			if decodeErr != nil {
				t.Fatalf("decode progressive preview: %v", decodeErr)
			}
			checkRow := max(0, midpoint.Rows-3)
			for _, x := range []int{16, 80, 160, 240, 304} {
				got, want := previewImage.NRGBAAt(x, checkRow), final[0].Image.NRGBAAt(x, checkRow)
				if absCh(got.R, want.R) > 8 || absCh(got.G, want.G) > 8 || absCh(got.B, want.B) > 8 {
					t.Fatalf("progressive pixel (%d,%d)=%v differs from final %v", x, checkRow, got, want)
				}
			}
		})
	}
}

func TestProgressiveSnapshotsShareImmutableFrequencyChunks(t *testing.T) {
	const rate = 48_000
	encoded, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	var previous *StreamUpdate
	var last StreamUpdate
	for start := 0; start < len(encoded); start += 4096 {
		end := min(start+4096, len(encoded))
		if _, err := decoder.Feed(context.Background(), encoded[start:end]); err != nil {
			t.Fatal(err)
		}
		for _, update := range decoder.DrainUpdates() {
			if previous != nil && previous.FrameID == update.FrameID {
				if len(update.FrequencyChunks) < len(previous.FrequencyChunks) {
					t.Fatalf("chunk count moved backwards: %d -> %d", len(previous.FrequencyChunks), len(update.FrequencyChunks))
				}
				if len(previous.FrequencyChunks) > 0 && len(update.FrequencyChunks) > 0 {
					if &previous.FrequencyChunks[0][0] != &update.FrequencyChunks[0][0] {
						t.Fatal("progressive update copied old frequency samples instead of sharing immutable chunks")
					}
				}
			}
			copyOfUpdate := update
			previous = &copyOfUpdate
			last = update
		}
	}
	if len(last.FrequencyChunks) == 0 || last.FrequencyLength <= 0 {
		t.Fatal("no frequency chunks in latest progressive update")
	}
	// Simulate the production preview worker receiving only the newest update
	// after older latest-wins queue entries were replaced.
	var assembled []float64
	for _, chunk := range last.FrequencyChunks {
		assembled = append(assembled, chunk...)
	}
	preview, err := last.DecodeWithFrequency(context.Background(), assembled)
	if err != nil {
		t.Fatalf("decode latest-only chunk snapshot: %v", err)
	}
	if preview.Bounds().Dx() != last.Mode.Width || preview.Bounds().Dy() != last.Mode.Height {
		t.Fatalf("preview dimensions = %v", preview.Bounds())
	}
}

func TestVISDetectionWithLowLevelAndBroadbandNoise(t *testing.T) {
	const rate = 48_000
	g, err := NewToneGenerator(rate, 0.18)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteVIS(g, 8); err != nil {
		t.Fatal(err)
	}
	samples := append(make([]int16, 257), g.Samples()...)
	samples = append(samples, make([]int16, rate)...)
	state := uint32(0xA5C31E27)
	for i := range samples {
		state = state*1664525 + 1013904223
		noise := int32((state>>16)&0x1fff) - 4096 // about +/- 0.125 full scale
		value := int32(samples[i]) + noise
		if value > 32767 {
			value = 32767
		}
		if value < -32768 {
			value = -32768
		}
		samples[i] = int16(value)
	}
	spec, vis, _, err := DetectModeHeader(samples, rate)
	if err != nil {
		t.Fatalf("noisy VIS detection failed: %v", err)
	}
	if spec.ID != Robot36ID || vis != 8 {
		t.Fatalf("detected %s / VIS %d, want Robot 36 / VIS 8", spec.ID, vis)
	}
}

func TestVISDetectionToleratesFrequencyOffsetAndArbitraryStart(t *testing.T) {
	const rate = 48_000
	for _, offset := range []float64{-45, -25, 25, 45} {
		t.Run(fmt.Sprintf("offset_%+.0fHz", offset), func(t *testing.T) {
			g, err := NewToneGenerator(rate, 0.55)
			if err != nil {
				t.Fatal(err)
			}
			write := func(hz, ms float64) {
				if err := g.ToneMS(hz+offset, ms); err != nil {
					t.Fatal(err)
				}
			}
			write(1900, 300)
			write(1200, 10)
			write(1900, 300)
			write(1200, 30) // start bit
			code := uint8(8)
			ones := 0
			for bit := 0; bit < 7; bit++ {
				if code&(1<<bit) != 0 {
					write(1100, 30)
					ones++
				} else {
					write(1300, 30)
				}
			}
			if ones%2 == 0 {
				write(1300, 30)
			} else {
				write(1100, 30)
			}
			write(1200, 30)
			prefix := make([]int16, 377)
			samples := append(prefix, g.Samples()...)
			// Keep enough trailing audio for the Auto detector to rule out longer
			// MMSSTV extended/N-VIS headers before committing to standard VIS.
			samples = append(samples, make([]int16, rate/2)...)
			// Add deterministic low-level broadband noise to model a real input.
			state := uint32(0x12345678)
			for i := range samples {
				state = state*1664525 + 1013904223
				noise := int16(int32((state>>16)&0x7ff) - 1024)
				value := int32(samples[i]) + int32(noise)
				if value > 32767 {
					value = 32767
				}
				if value < -32768 {
					value = -32768
				}
				samples[i] = int16(value)
			}
			spec, vis, _, err := DetectModeHeader(samples, rate)
			if err != nil {
				t.Fatalf("DetectModeHeader failed: %v", err)
			}
			if spec.ID != Robot36ID || vis != 8 {
				t.Fatalf("detected %s / VIS %d, want Robot 36 / VIS 8", spec.ID, vis)
			}
		})
	}
}

func TestFrequencyTrackerMatchesSingleBufferDemodulation(t *testing.T) {
	const rate = 48_000
	wave, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	want := instantaneousFrequency(wave, rate)
	tracker := newFrequencyTracker(rate)
	var got []float64
	for start := 0; start < len(wave); start += 997 {
		end := min(start+997, len(wave))
		got = append(got, tracker.Process(wave[start:end])...)
	}
	if len(got) != len(want) {
		t.Fatalf("stream frequency count=%d, whole-buffer count=%d", len(got), len(want))
	}
	for _, index := range []int{0, 1, 14399, 14400, 50000, len(wave) / 2, len(wave) - 1} {
		if got[index] != want[index] {
			t.Fatalf("frequency at sample %d = %.6f, whole-buffer %.6f", index, got[index], want[index])
		}
	}
}

func TestStreamDecoderCanDeferFinalRasterDecode(t *testing.T) {
	const rate = 48_000
	wave, err := NewEncoder(rate).Encode(context.Background(), makeTestSSTVImage(), Robot36ID)
	if err != nil {
		t.Fatal(err)
	}
	syncDecoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	synchronous, err := syncDecoder.Feed(context.Background(), wave)
	if err != nil || len(synchronous) != 1 || synchronous[0].Image == nil {
		t.Fatalf("synchronous reference decode failed: frames=%d err=%v", len(synchronous), err)
	}
	decoder, err := NewStreamDecoder(rate)
	if err != nil {
		t.Fatal(err)
	}
	decoder.SetDeferredFrameDecode(true)
	frames, err := decoder.Feed(context.Background(), wave)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("deferred Feed returned %d frames, want 1", len(frames))
	}
	if frames[0].Image != nil || frames[0].DecodeRequest == nil {
		t.Fatalf("deferred event = %+v, want DecodeRequest and no synchronous Image", frames[0])
	}
	img, err := frames[0].DecodeRequest.DecodeFinal(context.Background())
	if err != nil {
		t.Fatalf("decode deferred raster: %v", err)
	}
	if img == nil || img.Bounds().Dx() != frames[0].Mode.Width || img.Bounds().Dy() != frames[0].Mode.Height {
		t.Fatalf("deferred image bounds = %v, want %dx%d", img.Bounds(), frames[0].Mode.Width, frames[0].Mode.Height)
	}
	if !bytes.Equal(img.Pix, synchronous[0].Image.Pix) {
		t.Fatal("deferred final decode differs from the original synchronous decoder output")
	}
}
