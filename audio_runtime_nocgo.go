//go:build !cgo

package main

import (
	"context"
	"errors"
)

func enumerateAppAudioDevices() (AudioDeviceLists, error) {
	return AudioDeviceLists{}, errors.New("音频设备访问需要启用 CGO，并使用支持 miniaudio 的构建环境")
}
func newAppAudioRuntime(AudioSettings) (appAudioRuntime, error) {
	return nil, errors.New("实时音频收发需要启用 CGO。Windows 构建请使用带 C 编译器的 Go 环境")
}

type unavailableAudioRuntime struct{}

func (unavailableAudioRuntime) DeviceLists() ([]AudioDeviceInfo, []AudioDeviceInfo, error) {
	return nil, nil, errors.New("CGO unavailable")
}
func (unavailableAudioRuntime) StartReceive(context.Context, func([]float32)) error {
	return errors.New("CGO unavailable")
}
func (unavailableAudioRuntime) StopReceive() error { return nil }
func (unavailableAudioRuntime) Transmit(context.Context, []float32, func(float64)) error {
	return errors.New("CGO unavailable")
}
func (unavailableAudioRuntime) Close() error { return nil }
