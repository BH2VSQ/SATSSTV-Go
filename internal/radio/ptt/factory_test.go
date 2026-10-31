package ptt

import "testing"

func TestNewControllerForVOXAndManual(t *testing.T) {
	for _, mode := range []Mode{ModeVOX} {
		controller, err := NewController(Config{Mode: mode})
		if err != nil {
			t.Fatalf("create %s controller: %v", mode, err)
		}
		if err := controller.SetPTT(nil, true); err != nil {
			t.Fatalf("set PTT for %s: %v", mode, err)
		}
		if err := controller.SetPTT(nil, false); err != nil {
			t.Fatalf("clear PTT for %s: %v", mode, err)
		}
		if err := controller.Close(); err != nil {
			t.Fatalf("close %s: %v", mode, err)
		}
	}
}

func TestNewControllerValidatesConfiguration(t *testing.T) {
	if _, err := NewController(Config{Mode: ModeHamlib}); err == nil {
		t.Fatal("expected missing rigctld address to fail")
	}
	if _, err := NewController(Config{Mode: ModeRTS}); err == nil {
		t.Fatal("expected missing serial port to fail")
	}
	if _, err := NewController(Config{Mode: "other"}); err == nil {
		t.Fatal("expected unknown PTT mode to fail")
	}
}
