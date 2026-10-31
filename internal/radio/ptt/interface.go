// Package ptt defines radio push-to-talk control interfaces.
package ptt

import "context"

// Controller controls the transmitter keying line. Implementations must make
// SetPTT(ctx, false) idempotent and release hardware resources in Close.
type Controller interface {
	SetPTT(ctx context.Context, enabled bool) error
	Close() error
}

// Mode identifies how the transmitter is keyed during an SSTV transmission.
type Mode string

const (
	ModeHamlib Mode = "hamlib"
	ModeRTS    Mode = "rts"
	ModeDTR    Mode = "dtr"
	ModeVOX    Mode = "vox"
)
