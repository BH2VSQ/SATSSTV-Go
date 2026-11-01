package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestBackgroundTemplateLayerPersistsZoomAndOffsets(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{}
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	slots := defaultTemplateSlots()
	slots[0].Objects = []TemplateObject{
		{Type: "background", ImageData: dataURL, ImageName: "底图.png", X: -18.5, Y: 27.25, Width: 175},
		{Type: "image", ImageData: dataURL, ImageName: "图层.png", X: 10, Y: 12, Width: 35},
		{Type: "last_rx", ImageName: "上一次 RX 图像", X: 54, Y: 14, Width: 38},
	}
	if _, err := app.SaveTemplateSlots(slots); err != nil {
		t.Fatalf("save template with background/ratio-only images: %v", err)
	}
	loaded, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatalf("reload templates: %v", err)
	}
	if len(loaded[0].Objects) != 3 {
		t.Fatalf("loaded %d objects, want 3", len(loaded[0].Objects))
	}
	background := loaded[0].Objects[0]
	if background.Type != "background" || background.X != -18.5 || background.Y != 27.25 || background.Width != 175 || !strings.HasPrefix(background.ImageData, "data:image/png;base64,") {
		t.Fatalf("background layer did not persist: %+v", background)
	}
	if loaded[0].Objects[2].Height != 0 {
		t.Fatalf("ratio-based last_rx layer unexpectedly requires height: %+v", loaded[0].Objects[2])
	}
}

func TestTemplateRejectsMultipleBackgroundLayers(t *testing.T) {
	slots := defaultTemplateSlots()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	slots[0].Objects = []TemplateObject{
		{Type: "background", ImageData: dataURL, X: 0, Y: 0, Width: 100},
		{Type: "background", ImageData: dataURL, X: 0, Y: 0, Width: 100},
	}
	if err := validateTemplateSlots(slots); err == nil {
		t.Fatal("template with two background layers should be rejected")
	}
}

func TestGlobalTXBackgroundPersistsAcrossTemplateReloads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{}
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	img.Set(2, 3, color.RGBA{G: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	want := TemplateObject{
		Type: "background", ImageData: "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()),
		ImageName: "全局背景.png", X: -23.5, Y: 18.25, Width: 185,
	}
	if got, err := app.SaveTXBackground(want); err != nil {
		t.Fatalf("save global background: %v", err)
	} else if got.Width != want.Width || got.X != want.X || got.Y != want.Y {
		t.Fatalf("saved background changed geometry: %+v", got)
	}
	loaded, err := app.GetTXBackground()
	if err != nil {
		t.Fatalf("reload global background: %v", err)
	}
	if loaded == nil || loaded.ImageData != want.ImageData || loaded.ImageName != want.ImageName || loaded.X != want.X || loaded.Y != want.Y || loaded.Width != want.Width {
		t.Fatalf("global background did not persist: %+v", loaded)
	}
	if err := app.ClearTXBackground(); err != nil {
		t.Fatalf("clear global background: %v", err)
	}
	loaded, err = app.GetTXBackground()
	if err != nil || loaded != nil {
		t.Fatalf("background after clear = %+v, err=%v; want nil", loaded, err)
	}
}
