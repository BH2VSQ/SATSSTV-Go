package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/audio"
	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/ptt"
	"github.com/BH2VSQ/SATSSTV-Go/internal/sstv"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// AudioSettings stores device and radio keying preferences. VOX is the
// first-run default so the radio can key from the SSTV audio without a manual
// PTT action.
type AudioSettings struct {
	SampleRate       int    `json:"sampleRate"`
	CaptureDeviceID  string `json:"captureDeviceId"`
	PlaybackDeviceID string `json:"playbackDeviceId"`
	PTTMode          string `json:"pttMode"`
	RigctlAddress    string `json:"rigctlAddress"`
	SerialPort       string `json:"serialPort"`
	ActiveHigh       bool   `json:"activeHigh"`
	PTTLeadMS        int    `json:"pttLeadMs"`
	PTTTailMS        int    `json:"pttTailMs"`
}

type AudioDeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

type AudioDeviceLists struct {
	Inputs  []AudioDeviceInfo `json:"inputs"`
	Outputs []AudioDeviceInfo `json:"outputs"`
}

type AudioState struct {
	Connected  bool   `json:"connected"`
	RXActive   bool   `json:"rxActive"`
	TXActive   bool   `json:"txActive"`
	SampleRate int    `json:"sampleRate"`
	PTTMode    string `json:"pttMode"`
}

type appAudioRuntime interface {
	DeviceLists() (inputs, outputs []AudioDeviceInfo, err error)
	StartReceive(ctx context.Context, onSamples func([]float32)) error
	StopReceive() error
	Transmit(ctx context.Context, samples []float32, onProgress func(float64)) error
	Close() error
}

func defaultAudioSettings() AudioSettings {
	return AudioSettings{SampleRate: 48_000, PTTMode: string(ptt.ModeVOX), ActiveHigh: true, PTTLeadMS: 300, PTTTailMS: 250}
}

func (a *App) audioSettingsPath() (string, error) {
	dir, err := a.settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "audio.json"), nil
}

func (a *App) readAudioSettings() (AudioSettings, error) {
	path, err := a.audioSettingsPath()
	if err != nil {
		return AudioSettings{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultAudioSettings(), nil
	}
	if err != nil {
		return AudioSettings{}, err
	}
	cfg := defaultAudioSettings()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return AudioSettings{}, fmt.Errorf("音频设置文件格式错误：%w", err)
	}
	return normalizeAudioSettings(cfg)
}

func normalizeAudioSettings(cfg AudioSettings) (AudioSettings, error) {
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 48_000
	}
	if cfg.SampleRate < 8_000 || cfg.SampleRate > 192_000 {
		return AudioSettings{}, fmt.Errorf("采样率必须在 8000 至 192000 Hz 之间")
	}
	cfg.PTTMode = strings.ToLower(strings.TrimSpace(cfg.PTTMode))
	if cfg.PTTMode == "" || cfg.PTTMode == "manual" {
		// Migrate old v3.5 manual settings to VOX. Manual PTT is no longer a
		// selectable control mode because it cannot automate SSTV transmission.
		cfg.PTTMode = string(ptt.ModeVOX)
	}
	switch ptt.Mode(cfg.PTTMode) {
	case ptt.ModeHamlib, ptt.ModeRTS, ptt.ModeDTR, ptt.ModeVOX:
	default:
		return AudioSettings{}, fmt.Errorf("未知 PTT 控制方式：%s", cfg.PTTMode)
	}
	cfg.RigctlAddress = strings.TrimSpace(cfg.RigctlAddress)
	cfg.SerialPort = strings.TrimSpace(cfg.SerialPort)
	if ptt.Mode(cfg.PTTMode) == ptt.ModeHamlib && cfg.RigctlAddress == "" {
		return AudioSettings{}, errors.New("Hamlib PTT 需要填写 rigctld 地址")
	}
	if (ptt.Mode(cfg.PTTMode) == ptt.ModeRTS || ptt.Mode(cfg.PTTMode) == ptt.ModeDTR) && cfg.SerialPort == "" {
		return AudioSettings{}, errors.New("RTS/DTR PTT 需要填写串口名称")
	}
	if cfg.PTTLeadMS < 0 || cfg.PTTLeadMS > 10_000 || cfg.PTTTailMS < 0 || cfg.PTTTailMS > 10_000 {
		return AudioSettings{}, errors.New("PTT 前后延时必须在 0 至 10000 ms 之间")
	}
	return cfg, nil
}

func (a *App) GetAudioSettings() (AudioSettings, error) {
	a.audioMu.Lock()
	if a.audioSession != nil {
		cfg := a.audioConfig
		a.audioMu.Unlock()
		return cfg, nil
	}
	a.audioMu.Unlock()
	cfg, err := a.readAudioSettings()
	if err != nil {
		return AudioSettings{}, err
	}
	a.audioMu.Lock()
	a.audioConfig = cfg
	a.audioMu.Unlock()
	return cfg, nil
}

func (a *App) saveAudioSettings(input AudioSettings) (AudioSettings, error) {
	a.txOpMu.Lock()
	defer a.txOpMu.Unlock()
	cfg, err := normalizeAudioSettings(input)
	if err != nil {
		return AudioSettings{}, err
	}
	a.audioMu.Lock()
	connected := a.audioSession != nil
	current := a.audioConfig
	a.audioMu.Unlock()
	if connected && cfg != current {
		return AudioSettings{}, errors.New("音频设备运行期间无法直接保存变更，请使用“保存并应用”以安全重建音频流")
	}
	path, err := a.audioSettingsPath()
	if err != nil {
		return AudioSettings{}, err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return AudioSettings{}, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return AudioSettings{}, err
	}
	if err := replaceFilePreservingPrevious(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return AudioSettings{}, err
	}
	a.audioMu.Lock()
	a.audioConfig = cfg
	a.audioMu.Unlock()
	return cfg, nil
}

func (a *App) GetAudioDevices() (AudioDeviceLists, error) { return enumerateAppAudioDevices() }

// InitializeAudio is invoked automatically when the workbench opens. It opens
// the configured shared-mode audio endpoints and starts continuous RX.
func (a *App) InitializeAudio(rxModeID string) error {
	a.audioMu.Lock()
	connected := a.audioSession != nil
	rxActive := a.rxCancel != nil
	a.audioMu.Unlock()
	if !connected {
		if err := a.connectAudio(); err != nil {
			return err
		}
	}
	if rxActive {
		return a.setRXMode(rxModeID)
	}
	return a.startRX(rxModeID)
}

// SetRXEnabled controls RX capture independently from the shared audio
// session. The session remains available for TX when reception is disabled.
func (a *App) SetRXEnabled(enabled bool, rxModeID string) error {
	if enabled {
		return a.InitializeAudio(rxModeID)
	}
	a.audioMu.Lock()
	connected := a.audioSession != nil
	a.audioMu.Unlock()
	if !connected {
		if err := a.connectAudio(); err != nil {
			return err
		}
	}
	return a.stopRX()
}

// ApplyAudioSettings saves updated device/PTT preferences, recreates the
// streams, and immediately resumes RX with the currently selected RX mode.
func (a *App) ApplyAudioSettings(input AudioSettings, rxModeID string) error {
	if err := a.StopPTTTest(); err != nil {
		return fmt.Errorf("停止 PTT 测试失败：%w", err)
	}
	a.audioMu.Lock()
	txActive := a.txCancel != nil
	wasRXActive := a.rxCancel != nil
	a.audioMu.Unlock()
	if txActive {
		return errors.New("正在发射，结束 TX 后才能应用音频设置")
	}
	if err := a.stopRX(); err != nil {
		return fmt.Errorf("停止旧 RX 流失败：%w", err)
	}
	if err := a.disconnectAudio(); err != nil {
		return fmt.Errorf("释放旧音频资源失败：%w", err)
	}
	if _, err := a.saveAudioSettings(input); err != nil {
		return err
	}
	if err := a.connectAudio(); err != nil {
		return err
	}
	if !wasRXActive {
		return nil
	}
	return a.startRX(rxModeID)
}

func (a *App) connectAudio() error {
	a.txOpMu.Lock()
	defer a.txOpMu.Unlock()
	cfg, err := a.readAudioSettings()
	if err != nil {
		return err
	}
	if _, err := normalizeAudioSettings(cfg); err != nil {
		return err
	}
	a.audioMu.Lock()
	if a.audioSession != nil {
		a.audioMu.Unlock()
		return errors.New("音频设备已经连接")
	}
	a.audioMu.Unlock()
	session, err := newAppAudioRuntime(cfg)
	if err != nil {
		return fmt.Errorf("连接音频设备失败：%w", err)
	}
	// Prepare the shared output endpoint while the user is connecting audio,
	// not after they press TX. The device emits silence while idle, avoiding the
	// first-transmission initialization delay. RX remains usable if the output
	// device cannot be prepared; TX will report the concrete error on demand.
	if playbackPrimer, ok := session.(interface{ PreparePlayback() error }); ok {
		if primeErr := playbackPrimer.PreparePlayback(); primeErr != nil && a.ctx != nil {
			runtime.EventsEmit(a.ctx, "audio:warning", "TX 输出设备预热失败，首次发射时将重新初始化："+primeErr.Error())
		}
	}
	a.audioMu.Lock()
	if a.audioSession != nil {
		a.audioMu.Unlock()
		_ = session.Close()
		return errors.New("音频设备连接状态发生变化，请重试")
	}
	a.audioSession, a.audioConfig = session, cfg
	a.audioMu.Unlock()
	a.emitAudioState()
	return nil
}

func (a *App) disconnectAudio() error {
	a.rxOpMu.Lock()
	defer a.rxOpMu.Unlock()
	a.txOpMu.Lock()
	defer a.txOpMu.Unlock()
	// A PTT test may have started just before the shutdown flag was observed.
	_ = a.StopPTTTest()
	a.audioMu.Lock()
	session, rxCancel, txCancel := a.audioSession, a.rxCancel, a.txCancel
	a.audioSession, a.rxCancel, a.txCancel = nil, nil, nil
	a.txGeneration++ // invalidate an in-flight worker before a new session can start
	a.audioMu.Unlock()
	if rxCancel != nil {
		rxCancel()
	}
	if txCancel != nil {
		txCancel()
	}
	var err error
	if session != nil {
		err = session.Close()
	}
	a.rxDecoderMu.Lock()
	a.rxDecoder = nil
	a.rxDecoderMu.Unlock()
	a.emitAudioState()
	return err
}

func (a *App) GetAudioState() AudioState {
	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	state := AudioState{Connected: a.audioSession != nil, SampleRate: a.audioConfig.SampleRate, PTTMode: a.audioConfig.PTTMode, RXActive: a.rxCancel != nil, TXActive: a.txCancel != nil}
	return state
}

func (a *App) emitAudioState() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "audio:state", a.GetAudioState())
	}
}

func (a *App) setRXMode(modeID string) error {
	modeID = strings.TrimSpace(modeID)
	if modeID != "" {
		spec, err := sstv.LookupMode(modeID)
		if err != nil {
			return err
		}
		if !spec.CodecReady {
			return fmt.Errorf("%s 解码器尚未实现", spec.Name)
		}
		modeID = spec.ID
	}
	// Applying a mode while Feed is decoding a long frame used to block the
	// Wails call until the full DSP pass ended. Publish the request atomically;
	// the RX worker applies it at the next block boundary under rxDecoderMu.
	requested := modeID
	a.rxModeRequest.Store(&requested)
	return nil
}

func (a *App) startRX(modeID string) error {
	a.rxOpMu.Lock()
	defer a.rxOpMu.Unlock()
	a.audioMu.Lock()
	session, cfg := a.audioSession, a.audioConfig
	if session == nil {
		a.audioMu.Unlock()
		return errors.New("RX 音频流尚未初始化，请检查设备设置中的输入设备")
	}
	if a.rxCancel != nil {
		a.audioMu.Unlock()
		return errors.New("RX 已经处于运行状态")
	}
	a.audioMu.Unlock()

	decoder, err := sstv.NewStreamDecoder(cfg.SampleRate)
	if err == nil {
		err = decoder.SetMode(modeID)
	}
	if err != nil {
		return err
	}
	// Completed raster reconstruction is CPU-heavy and must not execute on
	// the ordered capture/decoder goroutine. Progressive and final frames are
	// both reconstructed by the dedicated frame worker.
	decoder.SetDeferredFrameDecode(true)
	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	rxCtx, cancel := context.WithCancel(baseCtx)
	// Keep the end-to-end backlog below a few capture periods. SSTV timing is
	// sensitive, but multi-second buffering is worse: it makes RX appear frozen
	// and eventually overflows the native queue. If this short queue fills, the
	// sender inserts an explicit discontinuity marker and keeps the newest data.
	queue := make(chan []float32, 16)
	a.rxQueueDrops.Store(0)

	a.rxDecoderMu.Lock()
	a.rxDecoder = decoder
	a.rxDecoderMu.Unlock()
	a.audioMu.Lock()
	if a.audioSession != session || a.rxCancel != nil {
		a.audioMu.Unlock()
		cancel()
		a.rxDecoderMu.Lock()
		a.rxDecoder = nil
		a.rxDecoderMu.Unlock()
		return errors.New("音频设备状态已变化，请重试启动 RX")
	}
	a.rxCancel = cancel
	a.audioMu.Unlock()

	if err := session.StartReceive(rxCtx, func(block []float32) {
		// Do not block the capture worker behind DSP/UI work. Bound latency and
		// explicitly reset the decoder if an exceptional overload loses samples.
		if rxCtx.Err() == nil {
			if dropped := enqueueRXBlock(queue, block); dropped > 0 {
				a.rxQueueDrops.Add(dropped)
			}
		}
	}); err != nil {
		cancel()
		a.rxDecoderMu.Lock()
		a.rxDecoder = nil
		a.rxDecoderMu.Unlock()
		a.audioMu.Lock()
		a.rxCancel = nil
		a.audioMu.Unlock()
		return err
	}
	fskidDecoder, err := sstv.NewFSKIDDecoder(cfg.SampleRate)
	if err != nil {
		_ = a.stopRX()
		return err
	}
	a.rxModeRequest.Store(&modeID)
	go a.runRXWorker(rxCtx, queue, decoder, fskidDecoder, cfg.SampleRate, session, modeID)
	a.emitAudioState()
	return nil
}

// enqueueRXBlock is the application-side low-latency queue policy. When full,
// all stale blocks are discarded, then a nil discontinuity marker and the
// newest block are queued in order. This prevents back-pressure from filling
// the native miniaudio queue while making any sample loss visible to RX.
func enqueueRXBlock(queue chan []float32, block []float32) uint64 {
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
				discarded = 1
			}
			queue <- nil
			if block != nil {
				queue <- block
			}
			return discarded
		}
	}
}

func (a *App) stopRX() error {
	a.rxOpMu.Lock()
	defer a.rxOpMu.Unlock()
	a.audioMu.Lock()
	cancel, session := a.rxCancel, a.audioSession
	a.rxCancel = nil
	a.audioMu.Unlock()
	if cancel != nil {
		cancel()
	}
	var err error
	if session != nil {
		err = session.StopReceive()
	}
	a.rxDecoderMu.Lock()
	a.rxDecoder = nil
	a.rxDecoderMu.Unlock()
	a.emitAudioState()
	return err
}

type captureDiagnostics interface {
	DroppedCaptureBlocks() uint64
	CaptureError() string
}

type rxTelemetryBlock struct {
	Samples         []float32
	ReceivedSamples uint64
	DroppedBlocks   uint64
	Discontinuity   bool
}

// rxFSKIDWork is routed to its own worker so the sliding one-millisecond
// FSKID search can never hold the ordered SSTV PCM decoder. Samples are
// immutable after enqueue; reset/armed state is serialized by the worker.
type rxFSKIDWork struct {
	reset   bool
	armed   bool
	finish  bool
	cycleID uint64
	samples []int16
}

type rxFSKIDListenState uint8

const (
	rxFSKIDIdle rxFSKIDListenState = iota
	rxFSKIDSearching
	rxFSKIDDraining
	rxFSKIDDecoded
	rxFSKIDExpired
)

// rxFSKIDCycle models one station-ID opportunity per completed SSTV picture.
// A generation ID prevents a late result from an older FSKID worker batch from
// disarming the listener belonging to a newer picture.
type rxFSKIDCycle struct {
	id               uint64
	state            rxFSKIDListenState
	samplesRemaining int
}

func (c *rxFSKIDCycle) Start(sampleRate int) uint64 {
	if c == nil || sampleRate <= 0 {
		return 0
	}
	c.id++
	if c.id == 0 { // Reserve zero for "no decoded cycle" in the atomic signal.
		c.id++
	}
	c.state = rxFSKIDSearching
	c.samplesRemaining = sampleRate * 8
	return c.id
}

func (c *rxFSKIDCycle) ID() uint64 {
	if c == nil {
		return 0
	}
	return c.id
}

func (c *rxFSKIDCycle) Searching() bool {
	return c != nil && c.state == rxFSKIDSearching && c.samplesRemaining > 0
}

func (c *rxFSKIDCycle) Take(sampleCount int) int {
	if !c.Searching() || sampleCount <= 0 {
		return 0
	}
	take := min(sampleCount, c.samplesRemaining)
	c.samplesRemaining -= take
	if c.samplesRemaining == 0 {
		// The capture side has reached the end of this picture's listen window,
		// but the FSKID worker may still have queued PCM to inspect. Keep the
		// cycle decodable until it acknowledges the ordered finish marker.
		c.state = rxFSKIDDraining
	}
	return take
}

func (c *rxFSKIDCycle) MarkDecoded(cycleID uint64) bool {
	if c == nil || cycleID == 0 || cycleID != c.id || (c.state != rxFSKIDSearching && c.state != rxFSKIDDraining) {
		return false
	}
	c.state = rxFSKIDDecoded
	c.samplesRemaining = 0
	return true
}

func (c *rxFSKIDCycle) MarkFinished(cycleID uint64) bool {
	if c == nil || cycleID == 0 || cycleID != c.id || c.state != rxFSKIDDraining {
		return false
	}
	c.state = rxFSKIDExpired
	return true
}

func (c *rxFSKIDCycle) ResetAfterCaptureGap() bool {
	return c != nil && c.Searching()
}

func (a *App) runRXWorker(ctx context.Context, queue <-chan []float32, decoder *sstv.StreamDecoder, fskidDecoder *sstv.FSKIDDecoder, sampleRate int, session appAudioRuntime, initialMode string) {
	var receivedSamples uint64
	lastProgress := time.Time{}
	telemetryQueue := make(chan rxTelemetryBlock, 8)
	previewQueue := make(chan sstv.StreamUpdate, 1) // latest preview wins; never back-pressure DSP
	frameQueue := make(chan sstv.StreamEvent, 2)
	// Forty-millisecond batches keep channel overhead low. A 512-entry queue
	// holds more than twenty seconds of FSKID audio, comfortably exceeding the
	// eight-second post-frame listen budget even with short native callbacks.
	const fskidBatchMS = 40
	fskidQueue := make(chan rxFSKIDWork, 512)
	fskidBatchSamples := max(1, sampleRate*fskidBatchMS/1000)
	fskidBatch := make([]int16, 0, fskidBatchSamples*2)
	var latestPreviewSequence atomic.Uint64
	var completedPreviewFrame atomic.Uint64
	var fskidDecodedCycle atomic.Uint64
	var fskidFinishedCycle atomic.Uint64
	go a.runRXTelemetryWorker(ctx, telemetryQueue, sampleRate, session)
	go a.runRXPreviewWorker(ctx, previewQueue, &latestPreviewSequence, &completedPreviewFrame)
	go a.runRXFrameWorker(ctx, frameQueue)
	go a.runRXFSKIDWorker(ctx, fskidQueue, fskidDecoder, &fskidDecodedCycle, &fskidFinishedCycle)
	activeMode := initialMode
	var fskidCycle rxFSKIDCycle
	for {
		select {
		case <-ctx.Done():
			return
		case block := <-queue:
			if block == nil {
				// The capture backend discarded a stale queue backlog. The SSTV
				// raster must be reset because its line/sample clock crossed a gap.
				// However, if a picture has already completed, MMSSTV sends FSKID
				// *after* that picture (often following a 300 ms leader). A short
				// capture gap during that post-picture window must reset only the
				// FSKID candidate timing, not disarm the per-picture listener.
				decoder.Reset()
				latestPreviewSequence.Add(1) // invalidate any preview already decoding
				resetRXFSKIDAfterCaptureDiscontinuity(
					fskidQueue, &fskidCycle, &fskidBatch, &fskidDecodedCycle,
				)
				enqueueRXTelemetry(telemetryQueue, rxTelemetryBlock{Discontinuity: true})
				lastProgress = time.Time{}
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:progress", map[string]any{"active": false, "percent": 0})
					runtime.EventsEmit(a.ctx, "rx:error", "音频处理发生积压，已丢弃过期音频并重置解码器，正在等待下一次 SSTV 信号")
				}
				continue
			}
			if len(block) == 0 {
				continue
			}
			receivedSamples += uint64(len(block))
			pcm := audio.Float32ToPCM16(block)
			var droppedBlocks uint64
			if diagnostics, ok := session.(captureDiagnostics); ok {
				droppedBlocks = diagnostics.DroppedCaptureBlocks()
			}
			telemetry := rxTelemetryBlock{Samples: block, ReceivedSamples: receivedSamples, DroppedBlocks: droppedBlocks}
			// Spectrum is visualization-only and must never hold SSTV decoding.
			enqueueRXTelemetry(telemetryQueue, telemetry)
			a.appendCaptureDump(block, sampleRate*10)

			// FSKID is transmitted after the analog picture. Scanning every live
			// SSTV pixel as a possible preamble is expensive and can false-lock on
			// long image tones. Arm only after a completed frame and seed the FSKID
			// scanner with StreamDecoder's exact unconsumed post-frame audio tail.
			a.rxDecoderMu.Lock()
			if requested := a.rxModeRequest.Load(); requested != nil && *requested != activeMode {
				if modeErr := decoder.SetMode(*requested); modeErr != nil {
					a.rxDecoderMu.Unlock()
					if a.ctx != nil {
						runtime.EventsEmit(a.ctx, "rx:error", "切换 RX 模式失败："+modeErr.Error())
					}
					continue
				}
				activeMode = *requested
				latestPreviewSequence.Add(1) // do not paint a stale image after mode changes
			}
			events, err := decoder.Feed(ctx, pcm)
			progress, progressActive := decoder.Progress()
			progressiveUpdates := decoder.DrainUpdates()
			var trailingFrameAudio []int16
			if len(events) > 0 {
				// A completed frame can share its final capture block with the next
				// protocol preamble. Preserve every post-raster sample even if the
				// SSTV detector has tentatively locked onto a narrowband FSKID leader.
				trailingFrameAudio = decoder.PendingAudio()
			}
			a.rxDecoderMu.Unlock()

			// Process the completion signal before checking ActiveMode. Feed may
			// finish one frame and detect the next frame from the tail of the same
			// capture block; the old branch order silently ignored the completed
			// frame in that case and never armed FSKID for its following trailer.
			if decodedCycleID := fskidDecodedCycle.Swap(0); decodedCycleID != 0 && fskidCycle.MarkDecoded(decodedCycleID) {
				fskidBatch = fskidBatch[:0]
				enqueueRXFSKIDWork(fskidQueue, rxFSKIDWork{reset: true, armed: false, cycleID: fskidCycle.ID()})
			}
			if finishedCycleID := fskidFinishedCycle.Swap(0); finishedCycleID != 0 {
				fskidCycle.MarkFinished(finishedCycleID)
			}
			if len(events) > 0 {
				// Every completed image starts a new, independent FSKID opportunity.
				// The cycle generation is carried through the side-channel queue so
				// late work/results from a previous image cannot affect this one.
				cycleID := fskidCycle.Start(sampleRate)
				fskidBatch = fskidBatch[:0]
				// Reset/arm first, then enqueue post-raster audio in chronological order.
				enqueueRXFSKIDWork(fskidQueue, rxFSKIDWork{reset: true, armed: true, cycleID: cycleID})
				take := fskidCycle.Take(len(trailingFrameAudio))
				appendRXFSKIDCycleBatch(fskidQueue, &fskidBatch, trailingFrameAudio[:take], fskidBatchSamples, cycleID)
			} else if fskidCycle.Searching() {
				// The eight-second window advances by captured samples, never wall time.
				take := fskidCycle.Take(len(pcm))
				if take > 0 {
					appendRXFSKIDCycleBatch(fskidQueue, &fskidBatch, pcm[:take], fskidBatchSamples, fskidCycle.ID())
				}
				if fskidCycle.state == rxFSKIDDraining {
					flushRXFSKIDCycleBatch(fskidQueue, &fskidBatch, fskidCycle.ID())
					// Ordered finish acknowledgement: the worker signals completion
					// only after it has inspected every PCM batch for this picture.
					enqueueRXFSKIDWork(fskidQueue, rxFSKIDWork{finish: true, cycleID: fskidCycle.ID()})
				}
			}

			// JPEG/base64 conversion and Wails event delivery run on a separate
			// latest-wins worker. A slow webview can skip an obsolete preview, but
			// it can never stall the ordered PCM decoder and overflow audio queues.
			for _, update := range progressiveUpdates {
				latestPreviewSequence.Store(update.Sequence)
				enqueueLatestRXPreview(previewQueue, update)
			}
			if progressActive && a.ctx != nil && time.Since(lastProgress) >= 100*time.Millisecond {
				runtime.EventsEmit(a.ctx, "rx:progress", map[string]any{"active": true, "mode": progress.Mode.Name, "modeId": progress.Mode.ID, "percent": progress.Percent, "rows": progress.Rows, "totalRows": progress.TotalRows, "receivedSamples": progress.Received, "totalSamples": progress.Total})
				lastProgress = time.Now()
			}
			if err != nil {
				if ctx.Err() == nil && a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:error", err.Error())
				}
				continue
			}
			for _, event := range events {
				completedPreviewFrame.Store(event.FrameID)
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:progress", map[string]any{"active": false, "percent": 100, "mode": event.Mode.Name, "modeId": event.Mode.ID, "rows": event.Mode.Height, "totalRows": event.Mode.Height, "complete": true})
				}
				select {
				case frameQueue <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// resetRXFSKIDAfterCaptureDiscontinuity clears only partial FSKID timing after
// a lost capture interval. The current per-picture listen cycle and its sample
// budget remain active; the reset is tagged with that cycle's generation.
func resetRXFSKIDAfterCaptureDiscontinuity(
	queue chan rxFSKIDWork,
	cycle *rxFSKIDCycle,
	batch *[]int16,
	decoded *atomic.Uint64,
) {
	if cycle == nil {
		return
	}
	if decoded != nil {
		if decodedID := decoded.Swap(0); decodedID != 0 {
			cycle.MarkDecoded(decodedID)
		}
	}
	if batch != nil {
		*batch = (*batch)[:0]
	}
	enqueueRXFSKIDWork(queue, rxFSKIDWork{
		reset: true, armed: cycle.Searching(), cycleID: cycle.ID(),
	})
}

// appendRXFSKIDCycleBatch preserves ordered PCM and tags every batch with the
// image cycle that owns it. This lets the worker ignore stale batches after a
// new image has opened another FSKID window.
func appendRXFSKIDCycleBatch(queue chan rxFSKIDWork, batch *[]int16, samples []int16, batchSamples int, cycleID uint64) {
	if batch == nil || batchSamples <= 0 || len(samples) == 0 {
		return
	}
	*batch = append(*batch, samples...)
	for len(*batch) >= batchSamples {
		chunk := append([]int16(nil), (*batch)[:batchSamples]...)
		enqueueRXFSKIDWork(queue, rxFSKIDWork{armed: true, cycleID: cycleID, samples: chunk})
		remainder := (*batch)[batchSamples:]
		copy(*batch, remainder)
		*batch = (*batch)[:len(remainder)]
	}
}

func flushRXFSKIDCycleBatch(queue chan rxFSKIDWork, batch *[]int16, cycleID uint64) {
	if batch == nil || len(*batch) == 0 {
		return
	}
	chunk := append([]int16(nil), (*batch)...)
	*batch = (*batch)[:0]
	enqueueRXFSKIDWork(queue, rxFSKIDWork{armed: true, cycleID: cycleID, samples: chunk})
}

func enqueueRXFSKIDWork(queue chan rxFSKIDWork, work rxFSKIDWork) {
	select {
	case queue <- work:
		return
	default:
	}
	// FSKID is optional metadata. If its side-channel queue ever fills, discard
	// only stale FSKID work, reset its timing state, and keep the newest block;
	// never block or reset the SSTV image decoder because of this queue.
	for {
		select {
		case <-queue:
		default:
			work.reset = true
			work.armed = work.armed || len(work.samples) > 0
			queue <- work
			return
		}
	}
}

func processRXFSKIDWork(decoder *sstv.FSKIDDecoder, armed *bool, activeCycleID *uint64, work rxFSKIDWork) []sstv.FSKIDEvent {
	if decoder == nil || armed == nil || activeCycleID == nil {
		return nil
	}
	if work.reset {
		decoder.Reset()
		*armed = work.armed
		*activeCycleID = work.cycleID
	}
	if !*armed || work.cycleID != *activeCycleID || len(work.samples) == 0 {
		return nil
	}
	events := decoder.Feed(work.samples)
	if len(events) > 0 {
		*armed = false
	}
	return events
}

func (a *App) runRXFSKIDWorker(ctx context.Context, queue <-chan rxFSKIDWork, decoder *sstv.FSKIDDecoder, decodedCycle, finishedCycle *atomic.Uint64) {
	armed := false
	var activeCycleID uint64
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-queue:
			if work.finish {
				// Overflow recovery may coalesce a finish marker with a reset.
				// Apply that generation before acknowledging its ordered boundary.
				if work.reset {
					decoder.Reset()
					armed = false
					activeCycleID = work.cycleID
				}
				if work.cycleID == activeCycleID {
					decoder.Reset()
					armed = false
					if finishedCycle != nil {
						finishedCycle.Store(activeCycleID)
					}
				}
				continue
			}
			for _, identity := range processRXFSKIDWork(decoder, &armed, &activeCycleID, work) {
				if decodedCycle != nil {
					decodedCycle.Store(activeCycleID)
				}
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:callsign", map[string]any{"callsign": identity.Callsign, "source": "MMSSTV FSKID"})
				}
			}
		}
	}
}

func enqueueLatestRXPreview(queue chan sstv.StreamUpdate, update sstv.StreamUpdate) {
	select {
	case queue <- update:
		return
	default:
	}
	select {
	case <-queue:
	default:
	}
	select {
	case queue <- update:
	default:
	}
}

func (a *App) runRXPreviewWorker(ctx context.Context, queue <-chan sstv.StreamUpdate, latestSequence, completedFrame *atomic.Uint64) {
	// Reassemble immutable frequency chunks incrementally. Every update carries
	// the full slice of chunk headers (not sample copies), so if the latest-wins
	// queue replaces an older update, this worker can still append all missing
	// chunks without forcing the RX/audio worker to clone a multi-megabyte track.
	var previewFrameID uint64
	var nextChunk int
	var previewFrequency []float64
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-queue:
			if a.ctx == nil || update.FrameID <= completedFrame.Load() || update.Sequence != latestSequence.Load() {
				continue
			}
			if previewFrameID != update.FrameID {
				previewFrameID = update.FrameID
				nextChunk = 0
				previewFrequency = previewFrequency[:0]
			}
			for nextChunk < len(update.FrequencyChunks) {
				previewFrequency = append(previewFrequency, update.FrequencyChunks[nextChunk]...)
				nextChunk++
			}
			img, err := update.DecodeWithFrequency(ctx, previewFrequency)
			if err != nil || img == nil {
				continue
			}
			var preview bytes.Buffer
			if err := jpeg.Encode(&preview, img, &jpeg.Options{Quality: 78}); err != nil {
				continue
			}
			// Do not deliver a preview that became stale while it was being
			// reconstructed/encoded or after the complete frame arrived.
			if update.FrameID <= completedFrame.Load() || update.Sequence != latestSequence.Load() || ctx.Err() != nil {
				continue
			}
			dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(preview.Bytes())
			runtime.EventsEmit(a.ctx, "rx:preview", map[string]any{"image": dataURL, "mode": update.Mode.Name, "modeId": update.Mode.ID, "width": update.Mode.Width, "height": update.Mode.Height, "rows": update.Rows, "totalRows": update.TotalRows})
		}
	}
}

func materializeRXFrameEvent(ctx context.Context, event sstv.StreamEvent) (sstv.StreamEvent, error) {
	if event.Image == nil && event.DecodeRequest != nil {
		img, err := event.DecodeRequest.DecodeFinal(ctx)
		if err != nil {
			return event, err
		}
		event.Image = img
	}
	if event.Image == nil {
		return event, errors.New("completed RX frame has neither an image nor a decode request")
	}
	return event, nil
}

func (a *App) runRXFrameWorker(ctx context.Context, queue <-chan sstv.StreamEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-queue:
			var materializeErr error
			event, materializeErr = materializeRXFrameEvent(ctx, event)
			if materializeErr != nil {
				if ctx.Err() == nil && a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:error", "SSTV 图像重建失败："+materializeErr.Error())
				}
				continue
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, event.Image); err != nil {
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:error", "PNG 图像编码失败："+err.Error())
				}
				continue
			}
			dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
			now := time.Now()
			_, saveErr := a.saveAlbumImage(event.Image, "rx", event.Mode.ID, "实时接收完成")
			payload := map[string]any{"image": dataURL, "mode": event.Mode.Name, "modeId": event.Mode.ID, "width": event.Mode.Width, "height": event.Mode.Height, "timestamp": now.Format("2006-01-02 15:04:05"), "albumSaved": saveErr == nil}
			if saveErr != nil {
				payload["albumError"] = saveErr.Error()
			}
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "rx:frame", payload)
			}
		}
	}
}

func enqueueRXTelemetry(queue chan rxTelemetryBlock, item rxTelemetryBlock) {
	select {
	case queue <- item:
		return
	default:
	}
	for {
		select {
		case <-queue:
		default:
			queue <- rxTelemetryBlock{Discontinuity: true}
			if !item.Discontinuity {
				queue <- item
			}
			return
		}
	}
}

// runRXTelemetryWorker computes UI-only spectrum data away from the ordered
// decoder path. Intermediate telemetry blocks may be coalesced; the SSTV PCM
// stream itself is never coalesced here.
func (a *App) runRXTelemetryWorker(ctx context.Context, queue <-chan rxTelemetryBlock, sampleRate int, session appAudioRuntime) {
	lastTelemetry := time.Time{}
	lastCaptureError := ""
	receivedSamples := uint64(0)
	spectrumWindow := make([]float32, 0, spectrumFFTSize)
	windowSamples := sstv.MMSSTVSpectrumWindowSamples(sampleRate)
	cadence := time.Duration(float64(windowSamples) / float64(max(1, sampleRate)) * float64(time.Second))
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-queue:
			if item.Discontinuity {
				spectrumWindow = spectrumWindow[:0]
				lastTelemetry = time.Time{}
				continue
			}
			if len(item.Samples) == 0 {
				continue
			}
			receivedSamples = item.ReceivedSamples
			spectrumWindow = append(spectrumWindow, item.Samples...)
			if len(spectrumWindow) > windowSamples {
				spectrumWindow = append(spectrumWindow[:0], spectrumWindow[len(spectrumWindow)-windowSamples:]...)
			}
			if len(spectrumWindow) < windowSamples || time.Since(lastTelemetry) < cadence {
				continue
			}
			level := rmsDBFS(item.Samples)
			spectrum := makeSpectrum(spectrumWindow, sampleRate)
			peakHz, crest := spectrumPeak(spectrum)
			droppedBlocks := item.DroppedBlocks + a.rxQueueDrops.Swap(0)
			if diagnostics, ok := session.(captureDiagnostics); ok {
				droppedBlocks = diagnostics.DroppedCaptureBlocks()
				if captureErr := diagnostics.CaptureError(); captureErr != "" && captureErr != lastCaptureError {
					lastCaptureError = captureErr
					if a.ctx != nil {
						runtime.EventsEmit(a.ctx, "rx:error", "音频采集回调异常："+captureErr)
					}
				}
			}
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "audio:telemetry", map[string]any{"dbfs": level, "spectrum": spectrum, "peakHz": peakHz, "crest": crest, "droppedBlocks": droppedBlocks, "samplesReceived": receivedSamples, "blockSamples": len(item.Samples)})
			}
			lastTelemetry = time.Now()
		}
	}
}

func rmsDBFS(samples []float32) float64 {
	if len(samples) == 0 {
		return -120
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v * v)
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	if rms < 1e-6 {
		return -120
	}
	return 20 * math.Log10(rms)
}

// makeSpectrum uses MMSSTV's default FQC frequency-occupancy algorithm rather
// than a magnitude FFT. At 48 kHz this uses 8192 raw samples (2048 after /4
// decimation), a 400 Hz discriminator LPF and a 700..2700 Hz display range.
const (
	spectrumFFTSize = 8192 // maximum raw window at 48 kHz
	spectrumBins    = 2048
	spectrumMinHz   = 700.0
	spectrumMaxHz   = 2700.0
)

func makeSpectrum(samples []float32, sampleRate int) []float64 {
	out := make([]float64, spectrumBins)
	if len(samples) < 16 || sampleRate <= 0 {
		return out
	}
	return sstv.MMSSTVFrequencyHistogram(audio.Float32ToPCM16(samples), sampleRate, spectrumBins, spectrumMinHz, spectrumMaxHz)
}

// spectrumPeak reports the dominant frequency and a "peakiness" crest factor
// (peak frequency-occupancy bin divided by the mean bin). A clean narrow-band
// signal has a high crest; broad/noisy audio has a lower crest.
func spectrumPeak(spectrum []float64) (peakHz, crest float64) {
	if len(spectrum) == 0 {
		return 0, 0
	}
	peakIdx := 0
	var sum float64
	for i, v := range spectrum {
		sum += v
		if v > spectrum[peakIdx] {
			peakIdx = i
		}
	}
	mean := sum / float64(len(spectrum))
	peakHz = spectrumMinHz + float64(peakIdx)*(spectrumMaxHz-spectrumMinHz)/float64(len(spectrum)-1)
	if mean > 0 {
		crest = spectrum[peakIdx] / mean
	}
	return peakHz, crest
}
func (a *App) StartTX(dataURL, modeID string, sampleRate int) error {
	img, err := decodeDataURL(dataURL)
	if err != nil {
		return err
	}
	spec, err := sstv.LookupMode(modeID)
	if err != nil {
		return err
	}
	if !spec.CodecReady {
		return fmt.Errorf("%s 尚未实现编码器", spec.Name)
	}
	a.txOpMu.Lock()
	if a.isPTTTestActive() {
		a.txOpMu.Unlock()
		return errors.New("PTT 测试正在运行，请先停止测试")
	}
	a.audioMu.Lock()
	session := a.audioSession
	if session == nil {
		a.audioMu.Unlock()
		a.txOpMu.Unlock()
		return errors.New("TX 音频输出尚未初始化，请在设备设置中检查所选输出设备")
	}
	if a.txCancel != nil {
		a.audioMu.Unlock()
		a.txOpMu.Unlock()
		return errors.New("TX 已经处于运行状态")
	}
	if sampleRate != a.audioConfig.SampleRate {
		a.audioMu.Unlock()
		a.txOpMu.Unlock()
		return fmt.Errorf("工作台采样率 %d Hz 与音频设备采样率 %d Hz 不一致", sampleRate, a.audioConfig.SampleRate)
	}
	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	txCtx, cancel := context.WithCancel(baseCtx)
	a.txGeneration++
	txGeneration := a.txGeneration
	a.txCancel = cancel
	a.audioMu.Unlock()
	a.txOpMu.Unlock()
	a.emitAudioState()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "tx:state", map[string]any{"state": "encoding", "message": "正在编码 SSTV 音频…"})
	}
	go func() {
		defer func() {
			cancel()
			a.finishTX(txGeneration)
		}()
		samples, encodeErr := sstv.NewEncoder(sampleRate).EncodeWithProgress(txCtx, img, modeID, func(done, total int) {
			if total > 0 {
				a.emitTXProgress("encoding", 50*float64(done)/float64(total), "正在编码 SSTV 图像")
			}
		})
		if encodeErr != nil {
			if errors.Is(encodeErr, context.Canceled) || errors.Is(txCtx.Err(), context.Canceled) {
				a.emitTXState("stopped", "编码阶段已取消发射")
			} else {
				a.emitTXState("error", encodeErr.Error())
			}
			return
		}
		station, stationErr := a.GetStationSettings()
		if stationErr != nil {
			a.emitTXState("error", "读取本站呼号失败："+stationErr.Error())
			return
		}
		samples, encodeErr = sstv.AppendMMSSTVFSKID(samples, sampleRate, station.MyCall, spec.Header == sstv.HeaderNarrowVIS)
		if encodeErr != nil {
			a.emitTXState("error", "生成 MMSSTV FSKID 失败："+encodeErr.Error())
			return
		}
		if station.MyCall != "" {
			a.emitTXProgress("encoding", 55, "SSTV 图像编码完成，已附加 FSKID 呼号")
		} else {
			a.emitTXProgress("encoding", 55, "SSTV 图像编码完成，未设置本站呼号，跳过 FSKID")
		}
		if err := txCtx.Err(); err != nil {
			a.emitTXState("stopped", "发射已取消")
			return
		}
		a.emitTXState("playing", "正在播放 SSTV 图像与呼号识别数据…")
		if playErr := session.Transmit(txCtx, audio.PCM16ToFloat32(samples), func(fraction float64) {
			a.emitTXProgress("playing", 55+45*math.Max(0, math.Min(1, fraction)), "正在发射 SSTV")
		}); playErr != nil {
			if errors.Is(playErr, context.Canceled) {
				a.emitTXState("stopped", "发射已停止")
			} else {
				a.emitTXState("error", playErr.Error())
			}
			return
		}
		entry, saveErr := a.saveAlbumImage(img, "tx", modeID, "音频已播放至选定输出设备")
		msg := "音频播放完成"
		if saveErr == nil {
			msg += "，已保存到 TX 相册"
		}
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "tx:complete", map[string]any{"message": msg, "albumEntry": entry, "albumSaved": saveErr == nil})
		}
		a.emitTXState("completed", msg)
	}()
	return nil
}

// StartTone transmits a 1500 Hz calibration tone for ten seconds. It uses
// the normal PTT lifecycle and can be cancelled through StopTX.
func (a *App) StartTone(sampleRate int) error {
	a.txOpMu.Lock()
	if a.isPTTTestActive() {
		a.txOpMu.Unlock()
		return errors.New("PTT 测试正在运行，请先停止测试")
	}
	a.audioMu.Lock()
	session := a.audioSession
	if session == nil {
		a.audioMu.Unlock()
		a.txOpMu.Unlock()
		return errors.New("TX 音频输出尚未初始化，请检查所选输出设备")
	}
	if a.txCancel != nil {
		a.audioMu.Unlock()
		a.txOpMu.Unlock()
		return errors.New("已有 TX 操作正在运行")
	}
	if sampleRate != a.audioConfig.SampleRate {
		a.audioMu.Unlock()
		a.txOpMu.Unlock()
		return fmt.Errorf("工作台采样率 %d Hz 与音频设备采样率 %d Hz 不一致", sampleRate, a.audioConfig.SampleRate)
	}
	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	txCtx, cancel := context.WithCancel(baseCtx)
	a.txGeneration++
	txGeneration := a.txGeneration
	a.txCancel = cancel
	a.audioMu.Unlock()
	a.txOpMu.Unlock()

	a.emitAudioState()
	a.emitTXProgress("playing", 0, "正在发送 1500 Hz 单音（10 秒）")
	go func() {
		defer func() {
			cancel()
			a.finishTX(txGeneration)
		}()
		generator, err := sstv.NewToneGenerator(sampleRate, 0.35)
		if err != nil {
			a.emitTXState("error", "生成单音失败："+err.Error())
			return
		}
		if err := generator.Tone(1500, 10*time.Second); err != nil {
			a.emitTXState("error", "生成单音失败："+err.Error())
			return
		}
		if err := txCtx.Err(); err != nil {
			a.emitTXState("stopped", "1500 Hz 单音已停止")
			return
		}
		a.emitTXState("playing", "正在发送 1500 Hz 单音（10 秒）")
		if err := session.Transmit(txCtx, audio.PCM16ToFloat32(generator.Samples()), nil); err != nil {
			if errors.Is(err, context.Canceled) {
				a.emitTXState("stopped", "1500 Hz 单音已停止")
			} else {
				a.emitTXState("error", "1500 Hz 单音发送失败："+err.Error())
			}
			return
		}
		a.emitTXState("completed", "1500 Hz 单音发送完成")
	}()
	return nil
}

// finishTX clears the active TX marker only when it still belongs to the
// worker being completed. Disconnect/reconnect or a subsequent transmission
// increments txGeneration, protecting the new worker from stale cleanup.
func (a *App) finishTX(generation uint64) {
	a.audioMu.Lock()
	if a.txGeneration == generation {
		a.txCancel = nil
	}
	a.audioMu.Unlock()
	a.emitAudioState()
}

func (a *App) StopTX() {
	a.audioMu.Lock()
	cancel := a.txCancel
	a.audioMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) emitTXProgress(stage string, percent float64, message string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "tx:progress", map[string]any{"stage": stage, "percent": math.Max(0, math.Min(100, percent)), "message": message})
	}
}

func (a *App) emitTXState(state, message string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "tx:state", map[string]any{"state": state, "message": message})
	}
}

func (a *App) shutdown(context.Context) {
	a.shuttingDown.Store(true)
	_ = a.StopPTTTest()
	a.rxOpMu.Lock()
	defer a.rxOpMu.Unlock()
	a.txOpMu.Lock()
	defer a.txOpMu.Unlock()
	// A PTT test may have started just before the shutdown flag was observed.
	_ = a.StopPTTTest()
	a.audioMu.Lock()
	session, rxCancel, txCancel := a.audioSession, a.rxCancel, a.txCancel
	a.audioSession, a.rxCancel, a.txCancel = nil, nil, nil
	a.txGeneration++
	a.audioMu.Unlock()
	if rxCancel != nil {
		rxCancel()
	}
	if txCancel != nil {
		txCancel()
	}
	if session != nil {
		_ = session.Close()
	}
	a.rxDecoderMu.Lock()
	a.rxDecoder = nil
	a.rxDecoderMu.Unlock()
}

// appendCaptureDump maintains an ordered rolling window without moving the
// entire buffer for every new block. It is called only from the RX worker.
func (a *App) appendCaptureDump(samples []float32, capacity int) {
	if a == nil || len(samples) == 0 || capacity <= 0 {
		return
	}
	a.dumpMu.Lock()
	defer a.dumpMu.Unlock()
	if len(a.dumpBuf) != capacity {
		a.dumpBuf = make([]float32, capacity)
		a.dumpWrite = 0
		a.dumpCount = 0
	}
	if len(samples) >= capacity {
		copy(a.dumpBuf, samples[len(samples)-capacity:])
		a.dumpWrite = 0
		a.dumpCount = capacity
		return
	}
	first := copy(a.dumpBuf[a.dumpWrite:], samples)
	if first < len(samples) {
		copy(a.dumpBuf, samples[first:])
	}
	a.dumpWrite = (a.dumpWrite + len(samples)) % capacity
	a.dumpCount = min(capacity, a.dumpCount+len(samples))
}

// captureDumpSnapshot returns the retained audio in chronological order.
func (a *App) captureDumpSnapshot() []float32 {
	if a == nil {
		return nil
	}
	a.dumpMu.Lock()
	defer a.dumpMu.Unlock()
	if a.dumpCount == 0 || len(a.dumpBuf) == 0 {
		return nil
	}
	out := make([]float32, a.dumpCount)
	start := (a.dumpWrite - a.dumpCount + len(a.dumpBuf)) % len(a.dumpBuf)
	first := copy(out, a.dumpBuf[start:])
	if first < len(out) {
		copy(out[first:], a.dumpBuf[:len(out)-first])
	}
	return out
}

// SaveCapturedAudio writes the last few seconds of raw captured audio to a WAV
// file in the settings directory. It is a diagnostic helper: the exported file
// shows exactly what the capture device produced, so capture/level/sample-rate
// issues can be separated from decode bugs.
func (a *App) SaveCapturedAudio() (string, error) {
	a.audioMu.Lock()
	rate := a.audioConfig.SampleRate
	a.audioMu.Unlock()
	if rate <= 0 {
		rate = 48_000
	}
	buf := a.captureDumpSnapshot()
	if len(buf) < rate/10 {
		return "", fmt.Errorf("采集缓冲不足（仅 %d 采样）", len(buf))
	}
	samples := make([]int16, len(buf))
	for i, v := range buf {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		samples[i] = int16(v * 32767)
	}
	dir, err := a.settingsDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "capture.wav")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := sstv.WriteWAV(f, samples, rate); err != nil {
		return "", err
	}
	return path, nil
}
