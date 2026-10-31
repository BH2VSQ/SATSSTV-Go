package sstv

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
)

const (
	robot36SyncMS       = 9.0
	robot36SyncPorchMS  = 3.0
	robot36LumaMS       = 88.0
	robot36SeparatorMS  = 4.5
	robot36ColorPorchMS = 1.5
	robot36ChromaMS     = 44.0
	robot36LineMS       = 150.0
)

// Encoder encodes analog SSTV images into mono PCM.
type Encoder struct {
	SampleRate int
	Amplitude  float64
}

func NewEncoder(sampleRate int) *Encoder {
	if sampleRate == 0 {
		sampleRate = DefaultRate
	}
	return &Encoder{SampleRate: sampleRate, Amplitude: DefaultLevel}
}

// Encode converts an image into the selected analog SSTV mode. The image is
// center-cropped to the mode aspect ratio and bilinearly resized to its raster.
func (e *Encoder) Encode(ctx context.Context, src image.Image, modeID string) ([]int16, error) {
	return e.EncodeWithProgress(ctx, src, modeID, nil)
}

// EncodeWithProgress encodes an image and reports completed raster rows. The
// callback runs on the encoder goroutine and must not block for long.
func (e *Encoder) EncodeWithProgress(ctx context.Context, src image.Image, modeID string, onProgress func(done, total int)) ([]int16, error) {
	if src == nil {
		return nil, errors.New("input image is nil")
	}
	if src.Bounds().Dx() <= 0 || src.Bounds().Dy() <= 0 {
		return nil, errors.New("input image has an empty or invalid rectangle")
	}
	if e == nil {
		return nil, errors.New("nil SSTV encoder")
	}
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	spec, err := LookupMode(strings.TrimSpace(strings.ToLower(modeID)))
	if err != nil {
		return nil, err
	}
	if e.SampleRate < 8_000 || e.SampleRate > 192_000 {
		return nil, fmt.Errorf("invalid sample rate %d", e.SampleRate)
	}
	if !spec.CodecReady {
		return nil, fmt.Errorf("SSTV mode %q is catalogued but its waveform codec is not implemented yet", spec.Name)
	}
	amplitude := e.Amplitude
	if amplitude == 0 {
		amplitude = DefaultLevel
	}
	generator, err := NewToneGenerator(e.SampleRate, amplitude)
	if err != nil {
		return nil, err
	}
	switch spec.Header {
	case HeaderVIS:
		if err := WriteVIS(generator, spec.VIS); err != nil {
			return nil, fmt.Errorf("write VIS header: %w", err)
		}
	case HeaderMMSSTV:
		if err := WriteMMSSTVHeader(generator, spec.ExtendedVIS); err != nil {
			return nil, fmt.Errorf("write MMSSTV extended VIS header: %w", err)
		}
	case HeaderNarrowVIS:
		if err := WriteNarrowVIS(generator, spec.NarrowVIS); err != nil {
			return nil, fmt.Errorf("write N-VIS header: %w", err)
		}
	case HeaderAVT:
		if err := WriteAVT90Header(generator); err != nil {
			return nil, fmt.Errorf("write AVT90 sync header: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported SSTV header kind %q", spec.Header)
	}

	rgb := resizeCropToRGB(src, spec.Width, spec.Height)
	yuv := rgbToYCbCrRaster(rgb)
	switch spec.Family {
	case FamilyRobot:
		for row := 0; row < spec.Height; row++ {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			var lineErr error
			if spec.Monochrome {
				lineErr = encodeMonochromeLine(generator, yuv[row], spec)
			} else if spec.RobotSequential {
				lineErr = encodeRobot72Line(generator, yuv[row], spec)
			} else {
				lineErr = encodeRobot36Line(generator, yuv, row, spec)
			}
			if lineErr != nil {
				return nil, fmt.Errorf("encode %s line %d: %w", spec.Name, row, lineErr)
			}
			if onProgress != nil {
				onProgress(row+1, spec.Height)
			}
		}
	case FamilyMartin, FamilyScottie, FamilyWraase, FamilyPasokon:
		for row := 0; row < spec.Height; row++ {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			if err := encodeRGBLine(generator, rgb, row, spec); err != nil {
				return nil, fmt.Errorf("encode %s line %d: %w", spec.Name, row, err)
			}
			if onProgress != nil {
				onProgress(row+1, spec.Height)
			}
		}
	case FamilyPD:
		for row := 0; row < spec.Height; row += 2 {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			if err := encodePDPair(generator, yuv, row, spec); err != nil {
				return nil, fmt.Errorf("encode %s line pair %d: %w", spec.Name, row/2, err)
			}
			if onProgress != nil {
				onProgress(min(row+2, spec.Height), spec.Height)
			}
		}
	case FamilyMP:
		for row := 0; row < spec.Height; row += 2 {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			if err := encodeMPPair(generator, yuv, row, spec); err != nil {
				return nil, fmt.Errorf("encode %s super-line %d: %w", spec.Name, row/2, err)
			}
			if onProgress != nil {
				onProgress(min(row+2, spec.Height), spec.Height)
			}
		}
	case FamilyMRML:
		for row := 0; row < spec.Height; row++ {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			if err := encodeMRMLLine(generator, yuv[row], spec); err != nil {
				return nil, fmt.Errorf("encode %s line %d: %w", spec.Name, row, err)
			}
			if onProgress != nil {
				onProgress(row+1, spec.Height)
			}
		}
	case FamilyMCN, FamilyAVT:
		for row := 0; row < spec.Height; row++ {
			if err := ctxErr(ctx); err != nil {
				return nil, err
			}
			if err := encodeRGBProfileLine(generator, rgb[row], spec); err != nil {
				return nil, fmt.Errorf("encode %s line %d: %w", spec.Name, row, err)
			}
			if onProgress != nil {
				onProgress(row+1, spec.Height)
			}
		}
	default:
		return nil, fmt.Errorf("no encoder implementation for family %q", spec.Family)
	}
	return generator.Samples(), nil
}

// EncodeRobot36 is a convenience wrapper for the first supported SSTV mode.
func EncodeRobot36(ctx context.Context, src image.Image, sampleRate int) ([]int16, error) {
	return NewEncoder(sampleRate).Encode(ctx, src, Robot36ID)
}

type ycbcrPixel struct {
	y  uint8
	cb uint8
	cr uint8
}

func resizeCropToYCbCr(src image.Image, width, height int) [][]ycbcrPixel {
	result := make([][]ycbcrPixel, height)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	targetAR := float64(width) / float64(height)
	sourceAR := float64(sw) / float64(sh)

	cropW, cropH := float64(sw), float64(sh)
	if sourceAR > targetAR {
		cropW = float64(sh) * targetAR
	} else {
		cropH = float64(sw) / targetAR
	}
	cropX := float64(sb.Min.X) + (float64(sw)-cropW)/2
	cropY := float64(sb.Min.Y) + (float64(sh)-cropH)/2

	for y := 0; y < height; y++ {
		result[y] = make([]ycbcrPixel, width)
		sy := cropY + (float64(y)+0.5)*cropH/float64(height) - 0.5
		for x := 0; x < width; x++ {
			sx := cropX + (float64(x)+0.5)*cropW/float64(width) - 0.5
			r, g, b := sampleBilinearRGB(src, sx, sy)
			yv, cb, cr := color.RGBToYCbCr(r, g, b)
			result[y][x] = ycbcrPixel{y: yv, cb: cb, cr: cr}
		}
	}
	return result
}

func sampleBilinearRGB(src image.Image, x, y float64) (uint8, uint8, uint8) {
	b := src.Bounds()
	minX, minY := b.Min.X, b.Min.Y
	maxX, maxY := b.Max.X-1, b.Max.Y-1
	x = math.Max(float64(minX), math.Min(float64(maxX), x))
	y = math.Max(float64(minY), math.Min(float64(maxY), y))
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := min(x0+1, maxX), min(y0+1, maxY)
	dx, dy := x-float64(x0), y-float64(y0)

	p00 := toOpaqueRGB(src.At(x0, y0))
	p10 := toOpaqueRGB(src.At(x1, y0))
	p01 := toOpaqueRGB(src.At(x0, y1))
	p11 := toOpaqueRGB(src.At(x1, y1))
	return uint8(interpolate4(float64(p00.R), float64(p10.R), float64(p01.R), float64(p11.R), dx, dy)),
		uint8(interpolate4(float64(p00.G), float64(p10.G), float64(p01.G), float64(p11.G), dx, dy)),
		uint8(interpolate4(float64(p00.B), float64(p10.B), float64(p01.B), float64(p11.B), dx, dy))
}

func toOpaqueRGB(c color.Color) color.RGBA {
	// Composite transparency over white. SSTV has no alpha channel, and a
	// white matte is generally more useful for callsign cards and photographs.
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	a := uint32(n.A)
	return color.RGBA{
		R: uint8((uint32(n.R)*a + 255*(255-a) + 127) / 255),
		G: uint8((uint32(n.G)*a + 255*(255-a) + 127) / 255),
		B: uint8((uint32(n.B)*a + 255*(255-a) + 127) / 255),
		A: 255,
	}
}

func interpolate4(p00, p10, p01, p11, dx, dy float64) float64 {
	top := p00 + (p10-p00)*dx
	bottom := p01 + (p11-p01)*dx
	return math.Max(0, math.Min(255, math.Round(top+(bottom-top)*dy)))
}

func encodeMonochromeLine(g *ToneGenerator, line []ycbcrPixel, spec ModeSpec) error {
	if len(line) != spec.Width {
		return fmt.Errorf("invalid raster width %d", len(line))
	}
	if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
		return err
	}
	for _, pixel := range line {
		if err := g.ToneMS(byteToVideoFrequencyForMode(pixel.y, spec), spec.ScanMS/float64(len(line))); err != nil {
			return err
		}
	}
	return nil
}

func encodeRobot36Line(g *ToneGenerator, pixels [][]ycbcrPixel, row int, spec ModeSpec) error {
	line := pixels[row]
	if len(line) != spec.Width {
		return fmt.Errorf("invalid raster width %d", len(line))
	}
	if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
		return err
	}
	if err := g.ToneMS(VideoMinHz, spec.SyncPorchMS); err != nil {
		return err
	}
	for _, p := range line {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.LumaMS/float64(len(line))); err != nil {
			return err
		}
	}

	separator := VideoMinHz // even rows carry Cr
	if row%2 == 1 {
		separator = VideoMaxHz // odd rows carry Cb
	}
	if err := g.ToneMS(separator, spec.SeparatorMS); err != nil {
		return err
	}
	if err := g.ToneMS(VideoCenterHz, spec.ColorPorchMS); err != nil {
		return err
	}

	pairedRow := row ^ 1
	for x, p := range line {
		pair := pixels[pairedRow][x]
		chroma := uint8((uint16(p.cr) + uint16(pair.cr) + 1) / 2)
		if row%2 == 1 {
			chroma = uint8((uint16(p.cb) + uint16(pair.cb) + 1) / 2)
		}
		if err := g.ToneMS(byteToVideoFrequencyForMode(chroma, spec), spec.ChromaMS/float64(len(line))); err != nil {
			return err
		}
	}
	return nil
}

// encodeRobot72Line transmits all Y, Cr (R-Y) and Cb (B-Y) components on
// every 320-pixel scan line. This removes Robot 36's alternating chroma
// reconstruction while preserving the Robot family's 1200 Hz line sync.
func encodeRobot72Line(g *ToneGenerator, line []ycbcrPixel, spec ModeSpec) error {
	if len(line) != spec.Width {
		return fmt.Errorf("invalid raster width %d", len(line))
	}
	if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
		return err
	}
	if err := g.ToneMS(VideoMinHz, spec.SyncPorchMS); err != nil {
		return err
	}
	for _, p := range line {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.LumaMS/float64(len(line))); err != nil {
			return err
		}
	}
	for index, component := range []struct{ value func(ycbcrPixel) uint8 }{
		{value: func(p ycbcrPixel) uint8 { return p.cr }},
		{value: func(p ycbcrPixel) uint8 { return p.cb }},
	} {
		// Robot YCrCb uses distinct separator tones so a receiver can tell
		// the two colour-difference channels apart after sync recovery:
		// 1500 Hz marks Cr (R-Y), 2300 Hz marks Cb (B-Y).
		separatorHz := VideoMinHz
		if index == 1 {
			separatorHz = VideoMaxHz
		}
		if err := g.ToneMS(separatorHz, spec.SeparatorMS); err != nil {
			return err
		}
		if err := g.ToneMS(VideoCenterHz, spec.ColorPorchMS); err != nil {
			return err
		}
		for _, p := range line {
			if err := g.ToneMS(byteToVideoFrequencyForMode(component.value(p), spec), spec.ChromaMS/float64(len(line))); err != nil {
				return err
			}
		}
	}
	return nil
}

// resizeCropToRGB creates the native raster with a center crop and bilinear
// sampling. Alpha is composited over white by sampleBilinearRGB.
func resizeCropToRGB(src image.Image, width, height int) [][]color.RGBA {
	result := make([][]color.RGBA, height)
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	targetAR := float64(width) / float64(height)
	sourceAR := float64(sw) / float64(sh)
	cropW, cropH := float64(sw), float64(sh)
	if sourceAR > targetAR {
		cropW = float64(sh) * targetAR
	} else {
		cropH = float64(sw) / targetAR
	}
	cropX := float64(sb.Min.X) + (float64(sw)-cropW)/2
	cropY := float64(sb.Min.Y) + (float64(sh)-cropH)/2
	for y := 0; y < height; y++ {
		result[y] = make([]color.RGBA, width)
		sy := cropY + (float64(y)+0.5)*cropH/float64(height) - 0.5
		for x := 0; x < width; x++ {
			sx := cropX + (float64(x)+0.5)*cropW/float64(width) - 0.5
			r, g, b := sampleBilinearRGB(src, sx, sy)
			result[y][x] = color.RGBA{R: r, G: g, B: b, A: 255}
		}
	}
	return result
}

func rgbToYCbCrRaster(rgb [][]color.RGBA) [][]ycbcrPixel {
	out := make([][]ycbcrPixel, len(rgb))
	for y := range rgb {
		out[y] = make([]ycbcrPixel, len(rgb[y]))
		for x, p := range rgb[y] {
			yv, cb, cr := color.RGBToYCbCr(p.R, p.G, p.B)
			out[y][x] = ycbcrPixel{y: yv, cb: cb, cr: cr}
		}
	}
	return out
}

func encodeRGBScan(g *ToneGenerator, line []color.RGBA, channel byte, scanMS float64) error {
	for _, p := range line {
		var value uint8
		switch channel {
		case 'R':
			value = p.R
		case 'G':
			value = p.G
		case 'B':
			value = p.B
		default:
			return fmt.Errorf("unknown RGB channel %q", channel)
		}
		if err := g.ToneMS(byteToVideoFrequency(value), scanMS/float64(len(line))); err != nil {
			return err
		}
	}
	return nil
}

func encodeRGBLine(g *ToneGenerator, pixels [][]color.RGBA, row int, spec ModeSpec) error {
	line := pixels[row]
	porch := func() error { return g.ToneMS(VideoMinHz, spec.PorchMS) }
	scan := func(i int, ch byte) error {
		duration := spec.RGBScanMS[i]
		if duration <= 0 {
			duration = spec.ScanMS
		}
		return encodeRGBScan(g, line, ch, duration)
	}
	switch spec.Family {
	case FamilyMartin:
		if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
			return err
		}
		if err := porch(); err != nil {
			return err
		}
		for i, ch := range []byte("GBR") {
			if err := scan(i, ch); err != nil {
				return err
			}
			if err := porch(); err != nil {
				return err
			}
		}
	case FamilyScottie:
		if err := porch(); err != nil {
			return err
		}
		if err := scan(0, 'G'); err != nil {
			return err
		}
		if err := porch(); err != nil {
			return err
		}
		if err := scan(1, 'B'); err != nil {
			return err
		}
		if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
			return err
		}
		if err := porch(); err != nil {
			return err
		}
		if err := scan(2, 'R'); err != nil {
			return err
		}
	case FamilyWraase:
		if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
			return err
		}
		for i, ch := range []byte("RGB") {
			if i == 0 || spec.PorchEachChannel {
				if err := porch(); err != nil {
					return err
				}
			}
			if err := scan(i, ch); err != nil {
				return err
			}
		}
		if spec.PorchEachChannel {
			if err := porch(); err != nil {
				return err
			}
		}
	case FamilyPasokon:
		if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
			return err
		}
		for i, ch := range []byte("RGB") {
			if err := porch(); err != nil {
				return err
			}
			if err := scan(i, ch); err != nil {
				return err
			}
		}
		if err := porch(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("not an RGB line family: %q", spec.Family)
	}
	return nil
}

func encodePDPair(g *ToneGenerator, pixels [][]ycbcrPixel, row int, spec ModeSpec) error {
	if row+1 >= len(pixels) {
		return errors.New("PD mode requires an even number of image rows")
	}
	even, odd := pixels[row], pixels[row+1]
	if len(even) != spec.Width || len(odd) != spec.Width {
		return errors.New("PD raster width mismatch")
	}
	if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
		return err
	}
	if err := g.ToneMS(VideoMinHz, spec.PorchMS); err != nil {
		return err
	}
	for _, p := range even {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	for x := 0; x < spec.Width; x++ {
		v := uint8((uint16(even[x].cr) + uint16(odd[x].cr) + 1) / 2)
		if err := g.ToneMS(byteToVideoFrequencyForMode(v, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	for x := 0; x < spec.Width; x++ {
		v := uint8((uint16(even[x].cb) + uint16(odd[x].cb) + 1) / 2)
		if err := g.ToneMS(byteToVideoFrequencyForMode(v, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	for _, p := range odd {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	return nil
}

func encodeModeChroma(g *ToneGenerator, values []uint8, durationMS float64, spec ModeSpec) error {
	if len(values) == 0 {
		return errors.New("empty chroma scan")
	}
	for _, value := range values {
		if err := g.ToneMS(byteToVideoFrequencyForMode(value, spec), durationMS/float64(len(values))); err != nil {
			return err
		}
	}
	return nil
}

// modeSyncTone and modePorchTone select the line-start tones for the MP
// family. The narrow-band MP variants use the MMSSTV narrow video band and
// consequently use 1900/2044 Hz instead of the ordinary 1200/1500 Hz pair.
func modeSyncTone(spec ModeSpec) float64 {
	if spec.NarrowBand {
		return visLeaderHz
	}
	return HorizontalSync
}

func modePorchTone(spec ModeSpec) float64 {
	if spec.NarrowBand {
		return NarrowVideoMinHz
	}
	return VideoMinHz
}

// MP modes transmit two luminance rows with a vertically averaged chroma pair.
func encodeMPPair(g *ToneGenerator, rows [][]ycbcrPixel, row int, spec ModeSpec) error {
	if row+1 >= len(rows) {
		return errors.New("MP modes require an even image height")
	}
	if err := g.ToneMS(modeSyncTone(spec), spec.SyncMS); err != nil {
		return err
	}
	if err := g.ToneMS(modePorchTone(spec), spec.PorchMS); err != nil {
		return err
	}
	for _, p := range rows[row] {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	cr, cb := make([]uint8, spec.Width), make([]uint8, spec.Width)
	for x := 0; x < spec.Width; x++ {
		a, b := rows[row][x], rows[row+1][x]
		cr[x] = uint8((uint16(a.cr) + uint16(b.cr) + 1) / 2)
		cb[x] = uint8((uint16(a.cb) + uint16(b.cb) + 1) / 2)
	}
	if err := encodeModeChroma(g, cr, spec.ChannelScanMS, spec); err != nil {
		return err
	}
	if err := encodeModeChroma(g, cb, spec.ChannelScanMS, spec); err != nil {
		return err
	}
	for _, p := range rows[row+1] {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	return nil
}

// MR/ML modes send full-width Y and half-width Cr/Cb on every scan line.
func encodeMRMLLine(g *ToneGenerator, line []ycbcrPixel, spec ModeSpec) error {
	if len(line) != spec.Width {
		return errors.New("MR/ML raster width mismatch")
	}
	if err := g.ToneMS(HorizontalSync, spec.SyncMS); err != nil {
		return err
	}
	if err := g.ToneMS(VideoMinHz, spec.PorchMS); err != nil {
		return err
	}
	for _, p := range line {
		if err := g.ToneMS(byteToVideoFrequencyForMode(p.y, spec), spec.ChannelScanMS/float64(spec.Width)); err != nil {
			return err
		}
	}
	if err := g.ToneMS(byteToVideoFrequencyForMode(line[len(line)-1].y, spec), spec.PixelHoldMS); err != nil {
		return err
	}
	cr, cb := make([]uint8, spec.Width/2), make([]uint8, spec.Width/2)
	for x := range cr {
		cr[x] = uint8((uint16(line[x*2].cr) + uint16(line[x*2+1].cr) + 1) / 2)
		cb[x] = uint8((uint16(line[x*2].cb) + uint16(line[x*2+1].cb) + 1) / 2)
	}
	chromaMS := spec.ChannelScanMS / 2
	if err := encodeModeChroma(g, cr, chromaMS, spec); err != nil {
		return err
	}
	if err := g.ToneMS(byteToVideoFrequencyForMode(cr[len(cr)-1], spec), spec.PixelHoldMS); err != nil {
		return err
	}
	if err := encodeModeChroma(g, cb, chromaMS, spec); err != nil {
		return err
	}
	return g.ToneMS(byteToVideoFrequencyForMode(cb[len(cb)-1], spec), spec.PixelHoldMS)
}

func encodeRGBProfileLine(g *ToneGenerator, line []color.RGBA, spec ModeSpec) error {
	if len(line) != spec.Width {
		return errors.New("RGB raster width mismatch")
	}
	if spec.Family == FamilyMCN {
		if err := g.ToneMS(1_900, spec.SyncMS); err != nil {
			return err
		}
		if err := g.ToneMS(NarrowVideoMinHz, spec.PorchMS); err != nil {
			return err
		}
	} else if spec.Family != FamilyAVT {
		return fmt.Errorf("unsupported RGB profile %q", spec.Family)
	}
	for i, channel := range []byte("RGB") {
		duration := spec.RGBScanMS[i]
		if duration <= 0 {
			duration = spec.ScanMS
		}
		if err := encodeRGBScanRange(g, line, channel, duration, spec); err != nil {
			return err
		}
	}
	return nil
}

func encodeRGBScanRange(g *ToneGenerator, line []color.RGBA, channel byte, scanMS float64, spec ModeSpec) error {
	for _, p := range line {
		var value uint8
		switch channel {
		case 'R':
			value = p.R
		case 'G':
			value = p.G
		case 'B':
			value = p.B
		default:
			return fmt.Errorf("unknown channel %q", channel)
		}
		if err := g.ToneMS(byteToVideoFrequencyForMode(value, spec), scanMS/float64(len(line))); err != nil {
			return err
		}
	}
	return nil
}

func byteToVideoFrequency(value uint8) float64 {
	return VideoMinHz + float64(value)*(VideoMaxHz-VideoMinHz)/255.0
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
