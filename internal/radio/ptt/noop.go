package ptt

import (
	"context"
	"fmt"
)

// NoopController represents radio-side VOX. The software does not send a
// hardware PTT command; SSTV audio triggers the radio transmitter.
type NoopController struct {
	Mode Mode
}

func NewNoopController(mode Mode) (*NoopController, error) {
	if mode != ModeVOX {
		return nil, fmt.Errorf("noop PTT controller only supports VOX mode, got %q", mode)
	}
	return &NoopController{Mode: mode}, nil
}

func (c *NoopController) SetPTT(_ context.Context, _ bool) error { return nil }

// RequiresPTTLeadTime reports that VOX has no software-controlled keying line.
// Audio should therefore begin immediately rather than waiting for PTT lead.
func (c *NoopController) RequiresPTTLeadTime() bool { return false }
func (c *NoopController) Close() error              { return nil }
