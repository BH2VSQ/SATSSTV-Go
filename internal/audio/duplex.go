// Package audio contains independent capture/playback orchestration for
// satellite full-duplex SSTV operation.
package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/ptt"
)

var (
	ErrClosed        = errors.New("full-duplex audio controller is closed")
	ErrTransmitBusy  = errors.New("an SSTV transmission is already active")
	ErrReceiveActive = errors.New("receive stream is already active")
)

// CaptureStream owns an input device. Its callback must not block on DSP or UI
// work. Implementations should copy/reuse buffers safely before returning.
// A nil sample slice is an ordered marker that input samples were discarded;
// consumers must discard partial decoder state before processing later audio.
type CaptureStream interface {
	StartCapture(ctx context.Context, onSamples func([]float32)) error
	StopCapture() error
}

// PlaybackStream owns an output device and must honor ctx while playing.
// CaptureStream and PlaybackStream are separate resources and may run together.
type PlaybackStream interface {
	Play(ctx context.Context, samples []float32, sampleRate int) error
	StopPlayback() error
}

// enqueueCaptureBlock keeps recent audio bounded. When the queue is full it
// replaces the stale backlog with a discontinuity marker followed by the newest
// block. The queue must have capacity for at least two entries.
func enqueueCaptureBlock(queue chan []float32, block []float32) uint64 {
	select {
	case queue <- block:
		return 0
	default:
	}
	var discarded uint64
	for {
		select {
		case stale := <-queue:
			if stale != nil {
				discarded++
			}
		default:
			if discarded == 0 {
				// The consumer may have drained the queue between the initial
				// full check and this loop. Still flag an uncertain discontinuity.
				discarded = 1
			}
			queue <- nil
			queue <- block
			return discarded
		}
	}
}

// TxTiming configures PTT lead-in and tail intervals.
type TxTiming struct {
	PTTLead             time.Duration
	PTTTail             time.Duration
	MaxTransmitDuration time.Duration // zero = waveform duration plus a 30 s safety margin
}

// FullDuplex keeps RX capture alive independently of TX playback and PTT.
// This is the central software guarantee required for satellite operation:
// Transmit never calls StopCapture and never resets the receive callback.
type FullDuplex struct {
	capture  CaptureStream
	playback PlaybackStream
	ptt      ptt.Controller
	rate     int
	timing   TxTiming

	mu        sync.Mutex
	rxMu      sync.Mutex
	txMu      sync.Mutex
	closed    bool
	rxActive  bool
	rxCancel  context.CancelFunc
	txActive  bool
	txCancel  context.CancelFunc
	rootCtx   context.Context
	rootClose context.CancelFunc
}

func NewFullDuplex(capture CaptureStream, playback PlaybackStream, pttController ptt.Controller, sampleRate int, timing TxTiming) (*FullDuplex, error) {
	if capture == nil {
		return nil, errors.New("capture stream is required")
	}
	if playback == nil {
		return nil, errors.New("playback stream is required")
	}
	if pttController == nil {
		return nil, errors.New("PTT controller is required")
	}
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("sample rate %d is outside 8000..192000 Hz", sampleRate)
	}
	if timing.PTTLead < 0 || timing.PTTLead > 10*time.Second || timing.PTTTail < 0 || timing.PTTTail > 10*time.Second {
		return nil, errors.New("PTT lead and tail must be between 0 and 10 seconds")
	}
	if timing.MaxTransmitDuration < 0 || timing.MaxTransmitDuration > 20*time.Minute {
		return nil, errors.New("maximum transmit duration must be between 0 and 20 minutes")
	}
	rootCtx, rootClose := context.WithCancel(context.Background())
	return &FullDuplex{
		capture: capture, playback: playback, ptt: pttController,
		rate: sampleRate, timing: timing,
		rootCtx: rootCtx, rootClose: rootClose,
	}, nil
}

// StartReceive starts the RX path exactly once. It remains active throughout
// Transmit calls and continues forwarding input while the TX audio is playing.
func (d *FullDuplex) StartReceive(ctx context.Context, onSamples func([]float32)) error {
	if d == nil {
		return ErrClosed
	}
	if onSamples == nil {
		return errors.New("receive callback is required")
	}
	d.rxMu.Lock()
	defer d.rxMu.Unlock()
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrClosed
	}
	if d.rxActive {
		d.mu.Unlock()
		return ErrReceiveActive
	}
	d.rxActive = true
	streamCtx, cancel := context.WithCancel(d.rootCtx)
	d.rxCancel = cancel
	d.mu.Unlock()

	if err := d.capture.StartCapture(streamCtx, onSamples); err != nil {
		cancel()
		d.mu.Lock()
		d.rxActive = false
		d.rxCancel = nil
		d.mu.Unlock()
		return fmt.Errorf("start SSTV receive capture: %w", err)
	}
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = d.StopReceive()
			case <-streamCtx.Done():
			}
		}()
	}
	return nil
}

// StopReceive stops only the receive stream. It is intentionally independent
// of TX and should not be called automatically when an image is transmitted.
func (d *FullDuplex) StopReceive() error {
	if d == nil {
		return nil
	}
	d.rxMu.Lock()
	defer d.rxMu.Unlock()
	d.mu.Lock()
	active := d.rxActive
	cancel := d.rxCancel
	d.rxActive = false
	d.rxCancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if !active {
		return nil
	}
	return d.capture.StopCapture()
}

// Transmit keys the uplink, waits for the configured lead-in when the selected
// PTT controller actually keys hardware, plays one SSTV
// waveform and releases PTT after the audio tail. RX is never stopped here.
func (d *FullDuplex) Transmit(ctx context.Context, samples []float32) error {
	return d.TransmitWithProgress(ctx, samples, nil)
}

// TransmitWithProgress reports playback progress from 0 to 1 when the playback
// backend supports per-buffer position reporting. TX lifecycle and PTT cleanup
// remain identical to Transmit.
func (d *FullDuplex) TransmitWithProgress(ctx context.Context, samples []float32, onProgress func(float64)) (retErr error) {
	if d == nil {
		return ErrClosed
	}
	if len(samples) == 0 {
		return errors.New("SSTV waveform is empty")
	}
	if !d.txMu.TryLock() {
		return ErrTransmitBusy
	}
	defer d.txMu.Unlock()

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrClosed
	}
	d.txActive = true
	maxDuration := d.timing.MaxTransmitDuration
	if maxDuration == 0 {
		waveformDuration := time.Duration(int64(len(samples)) * int64(time.Second) / int64(d.rate))
		maxDuration = waveformDuration + 30*time.Second
	}
	txCtx, cancel := context.WithTimeout(d.rootCtx, maxDuration)
	d.txCancel = cancel
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		d.txActive = false
		d.txCancel = nil
		d.mu.Unlock()
	}()
	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				cancel()
			case <-txCtx.Done():
			}
		}()
	}

	if err := d.ptt.SetPTT(txCtx, true); err != nil {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		releaseErr := d.ptt.SetPTT(releaseCtx, false)
		releaseCancel()
		if releaseErr != nil {
			return errors.Join(fmt.Errorf("assert transmit PTT: %w", err), fmt.Errorf("best-effort PTT release also failed: %w; check the radio immediately", releaseErr))
		}
		return fmt.Errorf("assert transmit PTT: %w", err)
	}
	pttAsserted := true
	defer func() {
		if !pttAsserted {
			return
		}
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer releaseCancel()
		if err := d.ptt.SetPTT(releaseCtx, false); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("release transmit PTT: %w; check the radio immediately", err))
		}
	}()

	// VOX has no software keying line: the radio keys from the first audio
	// sample. Waiting for a hardware PTT lead in this mode would create a silent
	// delay before every test tone and SSTV transmission. Hardware PTT modes
	// continue to honor the configured lead interval.
	if policy, ok := d.ptt.(interface{ RequiresPTTLeadTime() bool }); !ok || policy.RequiresPTTLeadTime() {
		if err := waitContext(txCtx, d.timing.PTTLead); err != nil {
			return err
		}
	}
	var playErr error
	if progressPlayback, ok := d.playback.(interface {
		PlayWithProgress(context.Context, []float32, int, func(float64)) error
	}); ok {
		playErr = progressPlayback.PlayWithProgress(txCtx, samples, d.rate, onProgress)
	} else {
		playErr = d.playback.Play(txCtx, samples, d.rate)
		if playErr == nil && onProgress != nil {
			onProgress(1)
		}
	}
	if playErr != nil {
		return fmt.Errorf("play SSTV waveform: %w", playErr)
	}
	if err := waitContext(txCtx, d.timing.PTTTail); err != nil {
		return err
	}
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := d.ptt.SetPTT(releaseCtx, false)
	releaseCancel()
	if err != nil {
		return fmt.Errorf("release transmit PTT: %w; check the radio immediately", err)
	}
	pttAsserted = false
	return nil
}

// State reports stream activity. RXActive must remain true while TXActive is
// true for a software full-duplex session.
type State struct {
	RXActive bool
	TXActive bool
	Closed   bool
}

func (d *FullDuplex) State() State {
	if d == nil {
		return State{Closed: true}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return State{RXActive: d.rxActive, TXActive: d.txActive, Closed: d.closed}
}

// Close cancels playback, stops capture, releases PTT, and closes the PTT
// controller. Both I/O directions are managed separately so a TX cancellation
// cannot accidentally tear down the RX capture device.
func (d *FullDuplex) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	if d.txCancel != nil {
		d.txCancel()
	}
	d.rootClose()
	d.mu.Unlock()

	var errs []error
	if err := d.StopReceive(); err != nil {
		errs = append(errs, fmt.Errorf("stop RX capture: %w", err))
	}
	if err := d.playback.StopPlayback(); err != nil {
		errs = append(errs, fmt.Errorf("stop TX playback: %w", err))
	}
	d.txMu.Lock()
	releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if err := d.ptt.SetPTT(releaseCtx, false); err != nil {
		errs = append(errs, fmt.Errorf("release PTT during shutdown: %w", err))
	}
	cancel()
	if err := d.ptt.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close PTT controller: %w", err))
	}
	d.txMu.Unlock()
	// The native adapter may own a shared miniaudio context in addition to its
	// independent input/output devices. Close it after PTT has been released.
	if closer, ok := d.capture.(interface{ CloseAudio() error }); ok {
		if err := closer.CloseAudio(); err != nil {
			errs = append(errs, fmt.Errorf("close audio backend: %w", err))
		}
	} else if closer, ok := d.playback.(interface{ CloseAudio() error }); ok {
		if err := closer.CloseAudio(); err != nil {
			errs = append(errs, fmt.Errorf("close audio backend: %w", err))
		}
	}
	return errors.Join(errs...)
}

func waitContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	t := time.NewTimer(duration)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
