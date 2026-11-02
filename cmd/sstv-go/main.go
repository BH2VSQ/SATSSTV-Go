package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/sstv"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "modes":
		listModes()
	case "encode":
		if err := encode(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "decode":
		if err := decode(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "decode-stream":
		if err := decodeStream(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `SATSSTV-Go

Usage:
  sstv-go modes
  sstv-go encode -in image.png -out transmission.wav [-mode robot36] [-rate 48000]
  sstv-go decode-stream -in capture.wav -out-dir decoded [-afc-max 1000] [-chunk-ms 20]

decode-stream replays PCM through the production StreamDecoder and dynamic
attenuator in ordered chunks and reports per-block real-time processing cost.
The CLI lists, encodes, and decodes the registered SSTV mode catalog.`)
}

func listModes() {
	for _, mode := range sstv.Modes() {
		header := fmt.Sprintf("VIS=%d", mode.VIS)
		switch mode.Header {
		case sstv.HeaderMMSSTV:
			header = fmt.Sprintf("MMSSTV=0x%04X", mode.ExtendedVIS)
		case sstv.HeaderNarrowVIS:
			header = fmt.Sprintf("N-VIS=0x%02X", mode.NarrowVIS)
		}
		status := "ready"
		if !mode.CodecReady {
			status = "catalog only"
		}
		fmt.Printf("%-12s %-15s %dx%d  body=%s  [%s] %s\n",
			mode.ID, header, mode.Width, mode.Height,
			time.Duration(sstv.BodyDurationMS(mode)*float64(time.Millisecond)).String(), status, mode.Description)
	}
}

func encode(args []string) error {
	flags := flag.NewFlagSet("encode", flag.ContinueOnError)
	input := flags.String("in", "", "input image (PNG or JPEG)")
	output := flags.String("out", "", "output WAV file")
	mode := flags.String("mode", sstv.Robot36ID, "SSTV mode")
	rate := flags.Int("rate", sstv.DefaultRate, "audio sample rate in Hz")
	amplitude := flags.Float64("amplitude", sstv.DefaultLevel, "PCM amplitude, 0.0..1.0")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" || strings.TrimSpace(*output) == "" {
		return errors.New("both -in and -out are required")
	}

	file, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("open input image: %w", err)
	}
	defer file.Close()
	img, format, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("decode input image: %w", err)
	}
	if format != "png" && format != "jpeg" {
		return fmt.Errorf("unsupported image format %q; use PNG or JPEG", format)
	}
	// Keep codec registrations explicit. These imports also document the
	// supported raster formats at the command-line boundary.
	_ = jpeg.DefaultQuality
	_ = png.BestSpeed

	encoder := sstv.NewEncoder(*rate)
	encoder.Amplitude = *amplitude
	started := time.Now()
	samples, err := encoder.Encode(context.Background(), img, *mode)
	if err != nil {
		return fmt.Errorf("encode SSTV: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filepath.Clean(*output)), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(filepath.Clean(*output)), ".sstv-*.wav.tmp")
	if err != nil {
		return fmt.Errorf("create temporary WAV: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := sstv.WriteWAV(tmp, samples, *rate); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync WAV: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close WAV: %w", err)
	}
	if err := os.Rename(tmpName, *output); err != nil {
		return fmt.Errorf("move WAV into place: %w", err)
	}

	duration := time.Duration(int64(len(samples)) * int64(time.Second) / int64(*rate))
	fmt.Printf("Encoded %s (%dx%d) to %s\n", *mode, img.Bounds().Dx(), img.Bounds().Dy(), *output)
	fmt.Printf("PCM: %d Hz, mono, 16-bit, %d samples, %s\n", *rate, len(samples), duration.Round(time.Millisecond))
	fmt.Printf("Elapsed: %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

func decode(args []string) error {
	flags := flag.NewFlagSet("decode", flag.ContinueOnError)
	input := flags.String("in", "", "input PCM WAV file")
	output := flags.String("out", "", "output PNG image")
	mode := flags.String("mode", "auto", "SSTV mode, or auto for VIS detection")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" || strings.TrimSpace(*output) == "" {
		return errors.New("both -in and -out are required")
	}
	file, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("open input WAV: %w", err)
	}
	defer file.Close()
	samples, rate, err := sstv.ReadWAV(file)
	if err != nil {
		return err
	}
	started := time.Now()
	var img image.Image
	var detected string
	if strings.EqualFold(strings.TrimSpace(*mode), "auto") {
		result, decodeErr := sstv.Decode(context.Background(), samples, rate)
		if decodeErr != nil {
			return fmt.Errorf("decode SSTV: %w", decodeErr)
		}
		img, detected = result.Image, result.Mode.Name
	} else {
		decoded, decodeErr := sstv.DecodeMode(context.Background(), samples, rate, *mode, 0)
		if decodeErr != nil {
			return fmt.Errorf("decode SSTV as %s: %w", *mode, decodeErr)
		}
		img, detected = decoded, *mode
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(*output)), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(filepath.Clean(*output)), ".sstv-*.png.tmp")
	if err != nil {
		return fmt.Errorf("create temporary PNG: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := png.Encode(tmp, img); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write PNG: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync PNG: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close PNG: %w", err)
	}
	if err := os.Rename(tmpName, *output); err != nil {
		return fmt.Errorf("move PNG into place: %w", err)
	}
	fmt.Printf("Decoded %s from %d Hz WAV to %s (%dx%d)\n", detected, rate, *output, img.Bounds().Dx(), img.Bounds().Dy())
	fmt.Printf("Elapsed: %s\n", time.Since(started).Round(time.Millisecond))
	return nil
}

func init() {
	// Register image formats for image.Decode without adding non-standard
	// dependencies to the core encoder.
	_ = jpeg.Options{}
	_ = png.Encoder{}
}
