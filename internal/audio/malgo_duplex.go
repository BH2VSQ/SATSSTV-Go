//go:build cgo

package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

// AudioDevice describes an input or output endpoint enumerated by miniaudio.
type AudioDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// MalgoDuplex owns independent native capture and playback devices under one
// miniaudio context. It implements both CaptureStream and PlaybackStream, so
// it can be passed as both directions to NewFullDuplex.
//
// Capture is deliberately kept alive while playback is running. No TX method
// stops, pauses, or resets the capture device.
type MalgoDuplex struct {
	ctxMu            sync.RWMutex
	ctx              *malgo.AllocatedContext
	sampleRate       uint32
	captureDeviceID  string
	playbackDeviceID string

	captureMu     sync.Mutex
	capture       *malgo.Device
	captureStop   chan struct{}
	captureCancel context.CancelFunc
	captureDone   chan struct{}
	droppedBlocks atomic.Uint64
	captureError  atomic.Value // string

	playbackLifeMu sync.Mutex
	playbackMu     sync.Mutex
	playback       *malgo.Device
	playbackJob    *malgoPlaybackJob
	closed         atomic.Bool
}

type malgoPlaybackJob struct {
	done     chan struct{}
	samples  []float32
	offset   int
	finished bool
	err      error
}

// NewMalgoDuplex initializes one miniaudio context. Capture and playback are
// initialized independently, which supports simultaneous RX/TX with separate
// sound-card endpoints and with radios exposing full-duplex USB audio.
func NewMalgoDuplex(captureDeviceID, playbackDeviceID string, sampleRate int, logf func(string, ...any)) (*MalgoDuplex, error) {
	if sampleRate < 8_000 || sampleRate > 192_000 {
		return nil, fmt.Errorf("sample rate %d is outside 8000..192000 Hz", sampleRate)
	}
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		if logf != nil {
			logf("audio backend: %s", message)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("initialize miniaudio context: %w", err)
	}
	return &MalgoDuplex{
		ctx: ctx, sampleRate: uint32(sampleRate),
		captureDeviceID: captureDeviceID, playbackDeviceID: playbackDeviceID,
	}, nil
}

// DeviceLists returns the currently enumerated input and output endpoints.
func (m *MalgoDuplex) DeviceLists() (inputs, outputs []AudioDevice, err error) {
	if m == nil {
		return nil, nil, errors.New("audio engine is closed")
	}
	m.ctxMu.RLock()
	defer m.ctxMu.RUnlock()
	if m.ctx == nil || m.closed.Load() {
		return nil, nil, errors.New("audio engine is closed")
	}
	inputs, err = listMalgoDevices(m.ctx.Context, malgo.Capture)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate capture devices: %w", err)
	}
	outputs, err = listMalgoDevices(m.ctx.Context, malgo.Playback)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate playback devices: %w", err)
	}
	return inputs, outputs, nil
}

func listMalgoDevices(ctx malgo.Context, kind malgo.DeviceType) ([]AudioDevice, error) {
	infos, err := ctx.Devices(kind)
	if err != nil {
		return nil, err
	}
	devices := make([]AudioDevice, 0, len(infos))
	for i := range infos {
		devices = append(devices, AudioDevice{
			ID: infos[i].ID.String(), Name: infos[i].Name(), IsDefault: infos[i].IsDefault != 0,
		})
	}
	return devices, nil
}

func (m *MalgoDuplex) selectedDeviceID(kind malgo.DeviceType, wanted string) (unsafe.Pointer, error) {
	if wanted == "" {
		return nil, nil
	}
	infos, err := m.ctx.Context.Devices(kind)
	if err != nil {
		return nil, err
	}
	for i := range infos {
		if infos[i].ID.String() == wanted {
			return infos[i].ID.Pointer(), nil
		}
	}
	return nil, fmt.Errorf("selected audio device %q no longer exists; refresh the device list", wanted)
}

// StartCapture starts the dedicated RX device. Its native callback only copies
// float samples into a bounded queue; application/DSP work executes elsewhere.
func (m *MalgoDuplex) StartCapture(ctx context.Context, onSamples func([]float32)) error {
	if m == nil {
		return errors.New("audio engine is closed")
	}
	m.ctxMu.RLock()
	defer m.ctxMu.RUnlock()
	if m.ctx == nil || m.closed.Load() {
		return errors.New("audio engine is closed")
	}
	if onSamples == nil {
		return errors.New("capture callback is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	m.captureMu.Lock()
	defer m.captureMu.Unlock()
	if m.capture != nil {
		return errors.New("capture device is already running")
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1
	cfg.Capture.ShareMode = malgo.Shared
	cfg.SampleRate = m.sampleRate
	deviceID, err := m.selectedDeviceID(malgo.Capture, m.captureDeviceID)
	if err != nil {
		return fmt.Errorf("select capture device: %w", err)
	}
	if deviceID != nil {
		cfg.Capture.DeviceID = deviceID
		defer freeMalgoDeviceID(deviceID)
	}

	// Keep a modest jitter buffer on the native callback side. The application
	// worker has its own 16-block queue; 16 blocks absorb short scheduler/UI
	// pauses without hiding seconds of stale audio. Sustained overload still
	// produces an explicit discontinuity marker rather than unbounded latency.
	queue := make(chan []float32, 16)
	workerCtx, cancel := context.WithCancel(context.Background())
	dev, err := malgo.InitDevice(m.ctx.Context, cfg, malgo.DeviceCallbacks{Data: func(_, input []byte, _ uint32) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.captureError.Store(fmt.Sprintf("capture callback panic: %v", recovered))
			}
		}()
		if len(input) < 4 || workerCtx.Err() != nil {
			return
		}
		block := make([]float32, len(input)/4)
		for i := range block {
			block[i] = math.Float32frombits(binary.LittleEndian.Uint32(input[i*4 : i*4+4]))
		}
		if workerCtx.Err() != nil {
			return
		}
		if discarded := enqueueCaptureBlock(queue, block); discarded > 0 {
			// An ordered nil marker tells the downstream worker to reset its
			// decoder state before consuming fresh audio.
			m.droppedBlocks.Add(discarded)
		}
	}})
	if err != nil {
		cancel()
		return fmt.Errorf("initialize capture device: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		cancel()
		return fmt.Errorf("start capture device: %w", err)
	}

	stopWatch := make(chan struct{})
	done := make(chan struct{})
	m.capture, m.captureStop, m.captureCancel = dev, stopWatch, cancel
	m.captureDone = done
	m.droppedBlocks.Store(0)
	m.captureError.Store("")

	go func() {
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				m.captureError.Store(fmt.Sprintf("capture worker panic: %v", recovered))
			}
		}()
		for {
			select {
			case <-workerCtx.Done():
				return
			case block := <-queue:
				// Nil marks a discontinuity and must reach the application layer.
				onSamples(block)
			}
		}
	}()

	// If the owner cancels its RX context, stop the native capture device too.
	go func() {
		select {
		case <-ctx.Done():
			_ = m.StopCapture()
		case <-stopWatch:
		}
	}()
	return nil
}

// StopCapture stops only the RX device and waits for its worker to exit.
func (m *MalgoDuplex) StopCapture() error {
	if m == nil {
		return nil
	}
	m.captureMu.Lock()
	dev, stopWatch, cancel, done := m.capture, m.captureStop, m.captureCancel, m.captureDone
	m.capture, m.captureStop, m.captureCancel, m.captureDone = nil, nil, nil, nil
	if stopWatch != nil {
		close(stopWatch)
	}
	if cancel != nil {
		cancel()
	}
	m.captureMu.Unlock()

	var stopErr error
	if dev != nil {
		stopErr = dev.Stop()
		dev.Uninit()
	}
	if done != nil {
		<-done
	}
	return stopErr
}

// Play sends one mono float32 waveform to the playback device. The output
// device remains started and emits digital silence between transmissions.
// Playback is independent of the capture device, enabling software full-duplex.
func (m *MalgoDuplex) Play(ctx context.Context, samples []float32, sampleRate int) error {
	return m.PlayWithProgress(ctx, samples, sampleRate, nil)
}

// PreparePlayback initializes and starts the shared output endpoint ahead of
// TX. Between transmissions the native callback emits silence, so the first
// waveform does not pay device-open/start latency after PTT has been asserted.
func (m *MalgoDuplex) PreparePlayback() error {
	if m == nil {
		return errors.New("audio engine is closed")
	}
	m.ctxMu.RLock()
	defer m.ctxMu.RUnlock()
	if m.ctx == nil || m.closed.Load() {
		return errors.New("audio engine is closed")
	}
	m.playbackLifeMu.Lock()
	defer m.playbackLifeMu.Unlock()
	m.playbackMu.Lock()
	if m.playbackJob != nil {
		m.playbackMu.Unlock()
		return ErrTransmitBusy
	}
	if m.playback == nil {
		dev, err := m.newPlaybackDeviceLocked()
		if err != nil {
			m.playbackMu.Unlock()
			return err
		}
		m.playback = dev
	}
	dev := m.playback
	m.playbackMu.Unlock()
	if dev.IsStarted() {
		return nil
	}
	if err := dev.Start(); err != nil {
		m.playbackMu.Lock()
		if m.playback == dev && m.playbackJob == nil {
			m.playback = nil
		}
		m.playbackMu.Unlock()
		dev.Uninit()
		return fmt.Errorf("start prepared playback device: %w", err)
	}
	return nil
}

// PlayWithProgress plays an SSTV waveform and polls the native callback's
// consumed sample offset from a non-real-time goroutine. The native callback
// stays lock-bounded and never calls Wails runtime code.
func (m *MalgoDuplex) PlayWithProgress(ctx context.Context, samples []float32, sampleRate int, onProgress func(float64)) error {
	if m == nil {
		return errors.New("audio engine is closed")
	}
	if len(samples) == 0 {
		return errors.New("playback waveform is empty")
	}
	if sampleRate != int(m.sampleRate) {
		return fmt.Errorf("playback sample rate %d does not match audio engine rate %d", sampleRate, m.sampleRate)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Hold the context read lock only while touching native context/device
	// creation. Release it during the potentially minutes-long TX so CloseAudio
	// can cancel the job and shut down the native device immediately.
	m.ctxMu.RLock()
	if m.ctx == nil || m.closed.Load() {
		m.ctxMu.RUnlock()
		return errors.New("audio engine is closed")
	}
	m.playbackLifeMu.Lock()
	m.playbackMu.Lock()
	if m.playbackJob != nil {
		m.playbackMu.Unlock()
		m.playbackLifeMu.Unlock()
		m.ctxMu.RUnlock()
		return ErrTransmitBusy
	}
	if m.playback == nil {
		dev, err := m.newPlaybackDeviceLocked()
		if err != nil {
			m.playbackMu.Unlock()
			m.playbackLifeMu.Unlock()
			m.ctxMu.RUnlock()
			return err
		}
		m.playback = dev
	}
	dev := m.playback
	job := &malgoPlaybackJob{done: make(chan struct{}), samples: append([]float32(nil), samples...)}
	m.playbackJob = job
	wasStarted := dev.IsStarted()
	m.playbackMu.Unlock()
	m.playbackLifeMu.Unlock()

	if !wasStarted {
		m.playbackLifeMu.Lock()
		m.playbackMu.Lock()
		stillActive := m.playbackJob == job && m.playback == dev
		m.playbackMu.Unlock()
		if stillActive {
			if err := dev.Start(); err != nil {
				m.finishPlaybackJob(job, err)
				m.playbackLifeMu.Unlock()
				m.ctxMu.RUnlock()
				return fmt.Errorf("start playback device: %w", err)
			}
		}
		m.playbackLifeMu.Unlock()
	}
	m.ctxMu.RUnlock()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-job.done:
			m.playbackMu.Lock()
			jobErr := job.err
			offset := len(samples)
			m.playbackMu.Unlock()
			if onProgress != nil && jobErr == nil {
				onProgress(1)
			}
			_ = offset
			return jobErr
		case <-ticker.C:
			if onProgress != nil {
				m.playbackMu.Lock()
				offset, total := job.offset, len(samples)
				m.playbackMu.Unlock()
				if total > 0 {
					onProgress(math.Max(0, math.Min(1, float64(offset)/float64(total))))
				}
			}
		case <-ctx.Done():
			m.finishPlaybackJob(job, ctx.Err())
			return ctx.Err()
		}
	}
}

func (m *MalgoDuplex) newPlaybackDeviceLocked() (*malgo.Device, error) {
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 1
	cfg.Playback.ShareMode = malgo.Shared
	cfg.SampleRate = m.sampleRate
	deviceID, err := m.selectedDeviceID(malgo.Playback, m.playbackDeviceID)
	if err != nil {
		return nil, fmt.Errorf("select playback device: %w", err)
	}
	if deviceID != nil {
		cfg.Playback.DeviceID = deviceID
		defer freeMalgoDeviceID(deviceID)
	}
	callbacks := malgo.DeviceCallbacks{Data: func(output, _ []byte, frameCount uint32) {
		m.playbackMu.Lock()
		defer m.playbackMu.Unlock()
		job := m.playbackJob
		for frame := uint32(0); frame < frameCount; frame++ {
			var value float32
			if job != nil && !job.finished && job.offset < len(job.samples) {
				value = job.samples[job.offset]
				job.offset++
			}
			byteOffset := int(frame) * 4
			if byteOffset+4 <= len(output) {
				binary.LittleEndian.PutUint32(output[byteOffset:byteOffset+4], math.Float32bits(value))
			}
		}
		if job != nil && !job.finished && job.offset >= len(job.samples) {
			job.finished = true
			job.samples = nil
			close(job.done)
			if m.playbackJob == job {
				m.playbackJob = nil
			}
		}
	}}

	// The output endpoint can report AUDCLNT_E_DEVICE_IN_USE while another
	// application holds it exclusively or the audio service is still
	// renegotiating the shared stream. Retry a few times before giving up.
	const attempts = 5
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		dev, err := malgo.InitDevice(m.ctx.Context, cfg, callbacks)
		if err == nil {
			return dev, nil
		}
		if !errors.Is(err, malgo.ErrBusy) && !errors.Is(err, malgo.ErrAccessDenied) {
			return nil, fmt.Errorf("initialize playback device: %w", err)
		}
		lastErr = err
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	return nil, fmt.Errorf("initialize playback device after %d attempts: %w (output device is busy or held in exclusive mode by another application; close other audio programs or select a different TX output device)", attempts, lastErr)
}

func (m *MalgoDuplex) finishPlaybackJob(job *malgoPlaybackJob, reason error) {
	m.playbackMu.Lock()
	defer m.playbackMu.Unlock()
	if m.playbackJob != job || job.finished {
		return
	}
	job.finished = true
	job.err = reason
	job.samples = nil
	close(job.done)
	m.playbackJob = nil
}

// StopPlayback cancels an in-flight waveform and tears down the playback
// device. Capture is deliberately untouched.
func (m *MalgoDuplex) StopPlayback() error {
	if m == nil {
		return nil
	}
	m.playbackLifeMu.Lock()
	m.playbackMu.Lock()
	job, dev := m.playbackJob, m.playback
	m.playbackJob, m.playback = nil, nil
	if job != nil && !job.finished {
		job.finished = true
		job.err = context.Canceled
		job.samples = nil
		close(job.done)
	}
	m.playbackMu.Unlock()
	m.playbackLifeMu.Unlock()

	if dev == nil {
		return nil
	}
	stopErr := dev.Stop()
	dev.Uninit()
	return stopErr
}

// DroppedCaptureBlocks reports input blocks discarded when the bounded native
// capture queue overflowed. A discontinuity marker is forwarded to RX.
func (m *MalgoDuplex) DroppedCaptureBlocks() uint64 {
	if m == nil {
		return 0
	}
	return m.droppedBlocks.Load()
}

// CaptureError returns the most recent callback/worker panic report.
func (m *MalgoDuplex) CaptureError() string {
	if m == nil {
		return ""
	}
	value := m.captureError.Load()
	if value == nil {
		return ""
	}
	return value.(string)
}

// CaptureInternalSampleRate and PlaybackInternalSampleRate expose the native
// hardware rates for diagnostics. miniaudio converts streams to the requested
// application sample rate configured by NewMalgoDuplex.
func (m *MalgoDuplex) CaptureInternalSampleRate() uint32 {
	m.captureMu.Lock()
	defer m.captureMu.Unlock()
	if m.capture == nil {
		return 0
	}
	return m.capture.CaptureInternalSampleRate()
}

func (m *MalgoDuplex) PlaybackInternalSampleRate() uint32 {
	m.playbackMu.Lock()
	defer m.playbackMu.Unlock()
	if m.playback == nil {
		return 0
	}
	return m.playback.PlaybackInternalSampleRate()
}

// CloseAudio stops both directions and releases the shared miniaudio context.
func (m *MalgoDuplex) CloseAudio() error {
	if m == nil {
		return nil
	}
	m.ctxMu.Lock()
	defer m.ctxMu.Unlock()
	if !m.closed.CompareAndSwap(false, true) {
		return nil
	}
	var errs []error
	if err := m.StopCapture(); err != nil {
		errs = append(errs, fmt.Errorf("stop capture device: %w", err))
	}
	if err := m.StopPlayback(); err != nil {
		errs = append(errs, fmt.Errorf("stop playback device: %w", err))
	}
	if m.ctx != nil {
		if err := m.ctx.Uninit(); err != nil {
			errs = append(errs, fmt.Errorf("uninitialize miniaudio context: %w", err))
		}
		m.ctx.Free()
		m.ctx = nil
	}
	return errors.Join(errs...)
}
