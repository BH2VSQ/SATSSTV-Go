package ptt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/rigctl"
	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/serialptt"
)

// Config selects one PTT transport. RigctlAddress is host:port, SerialPort is
// a COM port on Windows, and ActiveHigh defines the RTS/DTR transmit level.
type Config struct {
	Mode          Mode
	RigctlAddress string
	SerialPort    string
	ActiveHigh    bool
}

// NewController constructs the requested PTT implementation.
func NewController(config Config) (Controller, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(string(config.Mode)))) {
	case ModeHamlib:
		if strings.TrimSpace(config.RigctlAddress) == "" {
			return nil, errors.New("Hamlib mode requires a rigctld TCP address")
		}
		client := rigctl.New(config.RigctlAddress)
		return rigctl.NewPTTController(client)
	case ModeRTS, ModeDTR:
		if strings.TrimSpace(config.SerialPort) == "" {
			return nil, fmt.Errorf("%s mode requires a serial port", config.Mode)
		}
		return &serialController{
			controller: serialptt.New(),
			port:       strings.TrimSpace(config.SerialPort),
			mode:       string(config.Mode),
			activeHigh: config.ActiveHigh,
		}, nil
	case ModeVOX:
		return NewNoopController(Mode(strings.ToLower(strings.TrimSpace(string(config.Mode)))))
	default:
		return nil, fmt.Errorf("unsupported PTT mode %q", config.Mode)
	}
}

type serialController struct {
	controller *serialptt.Controller
	port       string
	mode       string
	activeHigh bool
}

func (c *serialController) SetPTT(_ context.Context, enabled bool) error {
	return c.controller.Set(c.port, c.mode, enabled, c.activeHigh)
}

func (c *serialController) Close() error { return c.controller.Close() }
