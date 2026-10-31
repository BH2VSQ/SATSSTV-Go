//go:build cgo

package main

import (
	"context"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/audio"
	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/ptt"
)

type malgoAppRuntime struct {
	backend *audio.MalgoDuplex
	duplex  *audio.FullDuplex
}

func enumerateAppAudioDevices() (AudioDeviceLists, error) {
	backend, err := audio.NewMalgoDuplex("", "", 48_000, nil)
	if err != nil {
		return AudioDeviceLists{}, err
	}
	defer backend.CloseAudio()
	inputs, outputs, err := backend.DeviceLists()
	if err != nil {
		return AudioDeviceLists{}, err
	}
	result := AudioDeviceLists{}
	for _, item := range inputs {
		result.Inputs = append(result.Inputs, AudioDeviceInfo{ID: item.ID, Name: item.Name, IsDefault: item.IsDefault})
	}
	for _, item := range outputs {
		result.Outputs = append(result.Outputs, AudioDeviceInfo{ID: item.ID, Name: item.Name, IsDefault: item.IsDefault})
	}
	return result, nil
}

func newAppAudioRuntime(cfg AudioSettings) (appAudioRuntime, error) {
	backend, err := audio.NewMalgoDuplex(cfg.CaptureDeviceID, cfg.PlaybackDeviceID, cfg.SampleRate, nil)
	if err != nil {
		return nil, err
	}
	controller, err := ptt.NewController(ptt.Config{Mode: ptt.Mode(cfg.PTTMode), RigctlAddress: cfg.RigctlAddress, SerialPort: cfg.SerialPort, ActiveHigh: cfg.ActiveHigh})
	if err != nil {
		_ = backend.CloseAudio()
		return nil, err
	}
	duplex, err := audio.NewFullDuplex(backend, backend, controller, cfg.SampleRate, audio.TxTiming{PTTLead: time.Duration(cfg.PTTLeadMS) * time.Millisecond, PTTTail: time.Duration(cfg.PTTTailMS) * time.Millisecond})
	if err != nil {
		_ = controller.Close()
		_ = backend.CloseAudio()
		return nil, err
	}
	return &malgoAppRuntime{backend: backend, duplex: duplex}, nil
}
func (m *malgoAppRuntime) DeviceLists() (inputs, outputs []AudioDeviceInfo, err error) {
	in, out, err := m.backend.DeviceLists()
	if err != nil {
		return nil, nil, err
	}
	for _, v := range in {
		inputs = append(inputs, AudioDeviceInfo{ID: v.ID, Name: v.Name, IsDefault: v.IsDefault})
	}
	for _, v := range out {
		outputs = append(outputs, AudioDeviceInfo{ID: v.ID, Name: v.Name, IsDefault: v.IsDefault})
	}
	return inputs, outputs, nil
}
func (m *malgoAppRuntime) StartReceive(ctx context.Context, cb func([]float32)) error {
	return m.duplex.StartReceive(ctx, cb)
}
func (m *malgoAppRuntime) StopReceive() error { return m.duplex.StopReceive() }
func (m *malgoAppRuntime) Transmit(ctx context.Context, samples []float32, onProgress func(float64)) error {
	return m.duplex.TransmitWithProgress(ctx, samples, onProgress)
}
func (m *malgoAppRuntime) Close() error                 { return m.duplex.Close() }
func (m *malgoAppRuntime) DroppedCaptureBlocks() uint64 { return m.backend.DroppedCaptureBlocks() }
func (m *malgoAppRuntime) CaptureError() string         { return m.backend.CaptureError() }
