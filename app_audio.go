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
	a.rxDecoderMu.Lock()
	defer a.rxDecoderMu.Unlock()
	if a.rxDecoder == nil {
		return nil
	}
	return a.rxDecoder.SetMode(modeID)
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
	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	rxCtx, cancel := context.WithCancel(baseCtx)
	// Keep a short rolling window. If DSP falls behind, the capture callback
	// below drops the oldest block so the worker stays close to live audio.
	queue := make(chan []float32, 8)
	var dropped atomic.Uint64

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
		select {
		case queue <- block:
		default:
			// Replace stale audio rather than accumulating seconds of latency.
			select {
			case <-queue:
				dropped.Add(1)
			default:
			}
			select {
			case queue <- block:
			default:
				dropped.Add(1)
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
	go a.runRXWorker(rxCtx, queue, decoder, fskidDecoder, cfg.SampleRate, &dropped, session)
	a.emitAudioState()
	return nil
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

func (a *App) runRXWorker(ctx context.Context, queue <-chan []float32, decoder *sstv.StreamDecoder, fskidDecoder *sstv.FSKIDDecoder, sampleRate int, dropped *atomic.Uint64, session appAudioRuntime) {
	lastTelemetry := time.Time{}
	var receivedSamples uint64
	lastProgress := time.Time{}
	lastCaptureError := ""
	spectrumWindow := make([]float32, 0, 2048)
	for {
		select {
		case <-ctx.Done():
			return
		case block := <-queue:
			if len(block) == 0 {
				continue
			}
			receivedSamples += uint64(len(block))
			pcm := audio.Float32ToPCM16(block)
			spectrumWindow = append(spectrumWindow, block...)
			if len(spectrumWindow) > 2048 {
				spectrumWindow = append(spectrumWindow[:0], spectrumWindow[len(spectrumWindow)-2048:]...)
			}
			if time.Since(lastTelemetry) >= 80*time.Millisecond {
				level := rmsDBFS(block)
				spectrum := makeSpectrum(spectrumWindow, sampleRate)
				droppedBlocks := dropped.Load()
				if diagnostics, ok := session.(captureDiagnostics); ok {
					droppedBlocks += diagnostics.DroppedCaptureBlocks()
					if captureErr := diagnostics.CaptureError(); captureErr != "" && captureErr != lastCaptureError {
						lastCaptureError = captureErr
						if a.ctx != nil {
							runtime.EventsEmit(a.ctx, "rx:error", "音频采集回调异常："+captureErr)
						}
					}
				}
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "audio:telemetry", map[string]any{"dbfs": level, "spectrum": spectrum, "droppedBlocks": droppedBlocks, "samplesReceived": receivedSamples, "blockSamples": len(block)})
				}
				lastTelemetry = time.Now()
			}
			// setRXMode and stopRX may update or clear the stream decoder while
			// capture is active. Keep the decoder state serialized with those
			// control operations; capture callbacks themselves never take this lock.
			// FSKID is a separate station-identification stream appended after the
			// analog picture. Decode it in parallel with the raster receiver.
			for _, identity := range fskidDecoder.Feed(pcm) {
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:callsign", map[string]any{"callsign": identity.Callsign, "source": "MMSSTV FSKID"})
				}
			}
			a.rxDecoderMu.Lock()
			events, err := decoder.Feed(ctx, pcm)
			progress, progressActive := decoder.Progress()
			progressiveUpdates := decoder.DrainUpdates()
			a.rxDecoderMu.Unlock()
			for _, update := range progressiveUpdates {
				if a.ctx == nil || update.Image == nil {
					continue
				}
				var preview bytes.Buffer
				if jpeg.Encode(&preview, update.Image, &jpeg.Options{Quality: 76}) != nil {
					continue
				}
				dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(preview.Bytes())
				runtime.EventsEmit(a.ctx, "rx:preview", map[string]any{"image": dataURL, "mode": update.Mode.Name, "modeId": update.Mode.ID, "width": update.Mode.Width, "height": update.Mode.Height, "rows": update.Rows, "totalRows": update.TotalRows})
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
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "rx:progress", map[string]any{"active": false, "percent": 100, "mode": event.Mode.Name, "modeId": event.Mode.ID, "rows": event.Mode.Height, "totalRows": event.Mode.Height, "complete": true})
				}
				var buf bytes.Buffer
				if err := png.Encode(&buf, event.Image); err != nil {
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

// makeSpectrum returns a compact logarithmic-independent FFT-like magnitude
// trace over 900..2800 Hz. A Hann-windowed direct DFT is used for the small UI
// telemetry window to avoid introducing another DSP dependency.
// makeSpectrum computes a Hann-windowed FFT trace over 900..2800 Hz.
// Magnitudes are normalized in dBFS so weak SSTV tones remain visible while
// the waterfall keeps a useful noise floor.
func makeSpectrum(samples []float32, sampleRate int) []float64 {
	const bins = 72
	const fftSize = 2048
	const floorDBFS = -90.0
	const ceilingDBFS = 0.0
	out := make([]float64, bins)
	if len(samples) < 16 || sampleRate <= 0 {
		return out
	}
	count := min(len(samples), fftSize)
	start := len(samples) - count
	window := make([]complex128, fftSize)
	var windowSum float64
	for i := 0; i < count; i++ {
		weight := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(max(1, count-1)))
		windowSum += weight
		window[i] = complex(float64(samples[start+i])*weight, 0)
	}
	if windowSum <= 0 {
		return out
	}
	fftInPlace(window)
	for bin := 0; bin < bins; bin++ {
		frequency := 900.0 + float64(bin)*(1900.0/float64(bins-1))
		index := int(math.Round(frequency * float64(fftSize) / float64(sampleRate)))
		if index < 1 || index >= fftSize/2 {
			continue
		}
		amplitude := 2 * math.Hypot(real(window[index]), imag(window[index])) / windowSum
		dbfs := 20 * math.Log10(math.Max(1e-8, amplitude))
		out[bin] = math.Max(0, math.Min(1, (dbfs-floorDBFS)/(ceilingDBFS-floorDBFS)))
	}
	return out
}

func fftInPlace(values []complex128) {
	n := len(values)
	if n < 2 || n&(n-1) != 0 {
		return
	}
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			values[i], values[j] = values[j], values[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		angle := -2 * math.Pi / float64(size)
		step := complex(math.Cos(angle), math.Sin(angle))
		half := size >> 1
		for offset := 0; offset < n; offset += size {
			factor := complex(1.0, 0.0)
			for i := 0; i < half; i++ {
				even := values[offset+i]
				odd := values[offset+i+half] * factor
				values[offset+i] = even + odd
				values[offset+i+half] = even - odd
				factor *= step
			}
		}
	}
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
	a.rxOpMu.Lock()
	defer a.rxOpMu.Unlock()
	a.txOpMu.Lock()
	defer a.txOpMu.Unlock()
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
