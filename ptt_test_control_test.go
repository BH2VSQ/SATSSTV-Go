package main

import (
	"context"
	"testing"
	"time"
)

func TestPTTTestWatchdogExpiresAtConfiguredDuration(t *testing.T) {
	if pttTestTimeout != 15*time.Second {
		t.Fatalf("PTT test timeout = %s, want 15s", pttTestTimeout)
	}
	if !waitPTTTestWatchdog(context.Background(), 10*time.Millisecond) {
		t.Fatal("watchdog did not report timeout")
	}
}

func TestPTTTestWatchdogCanBeCancelledEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitPTTTestWatchdog(ctx, time.Second) {
		t.Fatal("cancelled watchdog must not report timeout")
	}
}
