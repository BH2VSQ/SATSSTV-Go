package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/audio"
	"github.com/BH2VSQ/SATSSTV-Go/internal/sstv"
)

type streamReplayFrame struct {
	Index                 int     `json:"index"`
	Mode                  string  `json:"mode"`
	ModeID                string  `json:"mode_id"`
	Width                 int     `json:"width"`
	Height                int     `json:"height"`
	VIS                   uint8   `json:"vis"`
	AFCInitialHz          float64 `json:"afc_initial_hz"`
	AFCFinalOffsetHz      float64 `json:"afc_final_offset_hz"`
	AFCFinalCorrectionHz  float64 `json:"afc_final_correction_hz"`
	AFCCorrectionCount    int     `json:"afc_correction_count"`
	DecodeMS              float64 `json:"decode_ms"`
	ImagePath             string  `json:"image_path"`
	WaterfallPath         string  `json:"waterfall_path,omitempty"`
	Error                 string  `json:"error,omitempty"`
}

type streamReplayReport struct {
	InputPath           string              `json:"input_path"`
	SampleRate          int                 `json:"sample_rate"`
	Samples             int                 `json:"samples"`
	DurationSeconds     float64             `json:"duration_seconds"`
	ChunkMilliseconds   float64             `json:"chunk_milliseconds"`
	ChunkCount          int                 `json:"chunk_count"`
	FeedTotalMS         float64             `json:"feed_total_ms"`
	FeedAverageMS       float64             `json:"feed_average_ms"`
	FeedP95MS           float64             `json:"feed_p95_ms"`
	FeedMaxMS           float64             `json:"feed_max_ms"`
	ChunksOverBudget    int                 `json:"chunks_over_budget"`
	FeedUtilization     float64             `json:"feed_utilization"`
	ReplayElapsedMS     float64             `json:"replay_elapsed_ms"`
	AttenuationMaxDB    float64             `json:"attenuation_max_db"`
	AttenuationAverageDB float64            `json:"attenuation_average_db"`
	Frames              []streamReplayFrame `json:"frames"`
	Notes               []string            `json:"notes"`
}

func decodeStream(args []string) error {
	flags := flag.NewFlagSet("decode-stream", flag.ContinueOnError)
	input := flags.String("in", "", "input mono/stereo 16-bit PCM WAV file")
	outputDir := flags.String("out-dir", "stream-replay", "output directory for decoded PNG frames and report")
	afcMax := flags.Float64("afc-max", 1_000, "AFC capture range in Hz (20..1000)")
	chunkMS := flags.Float64("chunk-ms", 20, "ordered decoder input block duration in milliseconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return errors.New("decode-stream requires -in")
	}
	if *chunkMS < 2 || *chunkMS > 100 {
		return fmt.Errorf("chunk-ms must be between 2 and 100, got %.1f", *chunkMS)
	}
	if *afcMax < 20 || *afcMax > 1_000 {
		return fmt.Errorf("afc-max must be between 20 and 1000, got %.1f", *afcMax)
	}
	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.Open(*input)
	if err != nil {
		return fmt.Errorf("open input WAV: %w", err)
	}
	samples, sampleRate, err := sstv.ReadWAV(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return errors.New("input WAV contains no PCM samples")
	}

	decoder, err := sstv.NewStreamDecoder(sampleRate)
	if err != nil {
		return err
	}
	decoder.SetAFC(true, *afcMax)
	decoder.SetDeferredFrameDecode(true)
	attenuator := audio.NewAutoAttenuator(sampleRate, audio.DefaultAutoAttenuatorConfig())
	chunkSamples := max(1, int(math.Round(float64(sampleRate)*(*chunkMS)/1000)))
	report := streamReplayReport{
		InputPath: *input,
		SampleRate: sampleRate,
		Samples: len(samples),
		DurationSeconds: float64(len(samples)) / float64(sampleRate),
		ChunkMilliseconds: float64(chunkSamples) * 1000 / float64(sampleRate),
		Notes: []string{
			"Uses the current Go StreamDecoder with AFC enabled and deferred final-frame reconstruction.",
			"PCM passes through the production AutoAttenuator in ordered chunks before StreamDecoder.Feed.",
			"Offline replay measures Feed processing cost against each chunk's real-time duration; it does not simulate the desktop audio device or Wails UI scheduler.",
		},
	}

	ctx := context.Background()
	feedDurations := make([]float64, 0, (len(samples)+chunkSamples-1)/chunkSamples)
	var totalAttenuation, maxAttenuation float64
	var attenuationBlocks int
	replayStarted := time.Now()
	for start := 0; start < len(samples); start += chunkSamples {
		end := min(len(samples), start+chunkSamples)
		raw := make([]float32, end-start)
		for i, sample := range samples[start:end] {
			raw[i] = float32(sample) / 32768
		}
		processed, attenuation := attenuator.Process(raw)
		totalAttenuation += attenuation.AttenuationDB
		attenuationBlocks++
		if attenuation.AttenuationDB > maxAttenuation {
			maxAttenuation = attenuation.AttenuationDB
		}
		pcm := make([]int16, len(processed))
		for i, sample := range processed {
			v := math.Max(-1, math.Min(1, float64(sample)))
			pcm[i] = int16(math.Round(v * 32767))
		}

		feedStarted := time.Now()
		events, feedErr := decoder.Feed(ctx, pcm)
		feedMS := float64(time.Since(feedStarted)) / float64(time.Millisecond)
		feedDurations = append(feedDurations, feedMS)
		report.FeedTotalMS += feedMS
		if feedMS > report.FeedMaxMS {
			report.FeedMaxMS = feedMS
		}
		if feedMS > float64(len(pcm))*1000/float64(sampleRate) {
			report.ChunksOverBudget++
		}
		report.ChunkCount++
		if feedErr != nil {
			return fmt.Errorf("Feed at sample %d: %w", start, feedErr)
		}
		for _, event := range events {
			if event.DecodeRequest == nil {
				continue
			}
			frameStarted := time.Now()
			img, offsets, decodeErr := event.DecodeRequest.DecodeFinalWithAFC(ctx)
			frame := streamReplayFrame{
				Index: len(report.Frames) + 1,
				Mode: event.Mode.Name,
				ModeID: event.Mode.ID,
				Width: event.Mode.Width,
				Height: event.Mode.Height,
				VIS: event.VIS,
				AFCInitialHz: event.DecodeRequest.Offset,
				AFCCorrectionCount: len(offsets),
			}
			if len(offsets) > 0 {
				frame.AFCFinalOffsetHz = offsets[len(offsets)-1].OffsetHz
				frame.AFCFinalCorrectionHz = -frame.AFCFinalOffsetHz
			}
			if decodeErr != nil {
				frame.Error = decodeErr.Error()
				report.Frames = append(report.Frames, frame)
				continue
			}
			frame.DecodeMS = float64(time.Since(frameStarted)) / float64(time.Millisecond)
			imageName := fmt.Sprintf("frame_%02d_%s.png", frame.Index, event.Mode.ID)
			imagePath := filepath.Join(*outputDir, imageName)
			imageFile, createErr := os.Create(imagePath)
			if createErr != nil {
				return createErr
			}
			if encodeErr := png.Encode(imageFile, img); encodeErr != nil {
				_ = imageFile.Close()
				return fmt.Errorf("write %s: %w", imageName, encodeErr)
			}
			if closeErr := imageFile.Close(); closeErr != nil {
				return closeErr
			}
			frame.ImagePath = imagePath

			track := event.DecodeRequest.FrequencyTrack()
			waterfall := sstv.BuildAFCFrameWaterfall(
				track, sampleRate, event.Mode, event.DecodeRequest.BodyStart,
				offsets, 512, 700, 2700,
			)
			if waterfall != nil {
				waterfallName := fmt.Sprintf("frame_%02d_%s_waterfall.png", frame.Index, event.Mode.ID)
				waterfallPath := filepath.Join(*outputDir, waterfallName)
				waterfallFile, createErr := os.Create(waterfallPath)
				if createErr != nil {
					return createErr
				}
				if encodeErr := png.Encode(waterfallFile, waterfall); encodeErr != nil {
					_ = waterfallFile.Close()
					return fmt.Errorf("write %s: %w", waterfallName, encodeErr)
				}
				if closeErr := waterfallFile.Close(); closeErr != nil {
					return closeErr
				}
				frame.WaterfallPath = waterfallPath
			}
			report.Frames = append(report.Frames, frame)
			fmt.Printf("Decoded frame %d: %s (%dx%d), AFC correction %+0.1f Hz, %d correction points, raster decode %.1f ms\n",
				frame.Index, frame.Mode, frame.Width, frame.Height,
				frame.AFCFinalCorrectionHz, frame.AFCCorrectionCount, frame.DecodeMS)
		}
	}
	report.ReplayElapsedMS = float64(time.Since(replayStarted)) / float64(time.Millisecond)
	if report.ChunkCount > 0 {
		report.FeedAverageMS = report.FeedTotalMS / float64(report.ChunkCount)
	}
	if len(feedDurations) > 0 {
		sort.Float64s(feedDurations)
		report.FeedP95MS = feedDurations[int(math.Ceil(float64(len(feedDurations))*0.95))-1]
	}
	if report.DurationSeconds > 0 {
		report.FeedUtilization = (report.FeedTotalMS / 1000) / report.DurationSeconds
	}
	if attenuationBlocks > 0 {
		report.AttenuationAverageDB = totalAttenuation / float64(attenuationBlocks)
	}
	report.AttenuationMaxDB = maxAttenuation
	reportPath := filepath.Join(*outputDir, "stream_replay_report.json")
	reportFile, err := os.Create(reportPath)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(reportFile)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(report)
	closeErr := reportFile.Close()
	if encodeErr != nil {
		return encodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Printf("Replay complete: %.3f s audio, %d chunks, %d frames. Feed total %.1f ms, avg %.3f ms, p95 %.3f ms, max %.3f ms, %d chunks over real-time budget.\n",
		report.DurationSeconds, report.ChunkCount, len(report.Frames),
		report.FeedTotalMS, report.FeedAverageMS, report.FeedP95MS,
		report.FeedMaxMS, report.ChunksOverBudget)
	fmt.Printf("Feed utilization %.4f, attenuation average %.2f dB / max %.2f dB. Report: %s\n",
		report.FeedUtilization, report.AttenuationAverageDB, report.AttenuationMaxDB, reportPath)
	return nil
}
