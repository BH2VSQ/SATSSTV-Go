package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/ptt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const pttTestTimeout = 15 * time.Second

// StartPTTTest asserts the configured hardware PTT for at most 15 seconds.
// The configuration is taken from the open settings form, so the user can
// verify the selected RTS/DTR or rigctld endpoint before saving it.
func (a *App) StartPTTTest(input AudioSettings) error {
	if a.shuttingDown.Load() {
		return errors.New("应用正在关闭，不能启动 PTT 测试")
	}
	cfg, err := normalizeAudioSettings(input)
	if err != nil {
		return err
	}
	if ptt.Mode(cfg.PTTMode) == ptt.ModeVOX {
		return errors.New("VOX 不提供软件控制的 PTT 测试")
	}

	// Serialize against starting a real TX. The test owns the PTT line until it
	// is explicitly stopped or the watchdog releases it.
	a.txOpMu.Lock()
	defer a.txOpMu.Unlock()
	if a.shuttingDown.Load() {
		return errors.New("应用正在关闭，不能启动 PTT 测试")
	}
	a.audioMu.Lock()
	txActive := a.txCancel != nil
	a.audioMu.Unlock()
	if txActive {
		return errors.New("正在发射，不能同时进行 PTT 测试")
	}

	a.pttTestMu.Lock()
	defer a.pttTestMu.Unlock()
	if a.pttTestController != nil {
		return errors.New("PTT 测试已经在运行")
	}

	controller, err := ptt.NewController(ptt.Config{
		Mode: ptt.Mode(cfg.PTTMode), RigctlAddress: cfg.RigctlAddress,
		SerialPort: cfg.SerialPort, ActiveHigh: cfg.ActiveHigh,
	})
	if err != nil {
		return fmt.Errorf("初始化 PTT 测试失败：%w", err)
	}
	setCtx, setCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = controller.SetPTT(setCtx, true)
	setCancel()
	if err != nil {
		_ = controller.Close()
		return fmt.Errorf("触发 PTT 失败：%w", err)
	}

	baseCtx := a.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	testCtx, cancel := context.WithCancel(baseCtx)
	a.pttTestGeneration++
	generation := a.pttTestGeneration
	a.pttTestController = controller
	a.pttTestCancel = cancel
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "ptt:test", map[string]any{"active": true, "durationSeconds": int(pttTestTimeout / time.Second), "mode": cfg.PTTMode})
	}
	go func() {
		if waitPTTTestWatchdog(testCtx, pttTestTimeout) {
			a.finishPTTTest(generation)
		}
	}()
	return nil
}

// StopPTTTest requests an immediate release. The 15-second watchdog remains
// authoritative even if the UI is closed or the frontend stops responding.
func (a *App) StopPTTTest() error {
	a.pttTestMu.Lock()
	generation := a.pttTestGeneration
	active := a.pttTestController != nil
	a.pttTestMu.Unlock()
	if !active {
		return nil
	}
	return a.finishPTTTest(generation)
}

func (a *App) finishPTTTest(generation uint64) error {
	a.pttTestMu.Lock()
	if a.pttTestController == nil || generation != a.pttTestGeneration {
		a.pttTestMu.Unlock()
		return nil
	}
	controller := a.pttTestController
	cancel := a.pttTestCancel
	a.pttTestController = nil
	a.pttTestCancel = nil
	a.pttTestMu.Unlock()

	if cancel != nil {
		cancel()
	}
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
	releaseErr := controller.SetPTT(releaseCtx, false)
	releaseCancel()
	closeErr := controller.Close()
	if releaseErr != nil {
		releaseErr = fmt.Errorf("释放 PTT 失败：%w", releaseErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("关闭 PTT 控制器失败：%w", closeErr)
	}
	err := errors.Join(releaseErr, closeErr)
	if a.ctx != nil {
		message := "PTT 测试已停止"
		if err != nil {
			message = strings.TrimSpace(err.Error())
		}
		runtime.EventsEmit(a.ctx, "ptt:test", map[string]any{"active": false, "error": errorString(err), "message": message})
	}
	return err
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (a *App) isPTTTestActive() bool {
	a.pttTestMu.Lock()
	defer a.pttTestMu.Unlock()
	return a.pttTestController != nil
}

// waitPTTTestWatchdog returns true only when the maximum hold duration expires.
// Cancellation is used for a deliberate early stop and is handled by the caller.
func waitPTTTestWatchdog(ctx context.Context, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
