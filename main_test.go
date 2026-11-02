package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAlbumSaveListAndPreview(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	img.Set(4, 5, color.RGBA{R: 0x5b, G: 0xce, B: 0xfa, A: 0xff})
	app := &App{}
	entry, err := app.saveAlbumImage(img, "rx", "robot36", "测试归档")
	if err != nil {
		t.Fatalf("saveAlbumImage() error = %v", err)
	}
	if !strings.HasPrefix(entry.Filename, "RX_") || !strings.HasSuffix(entry.Filename, ".png") {
		t.Fatalf("timestamp filename was not generated: %q", entry.Filename)
	}
	if entry.DisplayTime == "" || entry.Timestamp == "" {
		t.Fatal("timestamp fields must not be empty")
	}

	items, err := app.ListAlbum()
	if err != nil {
		t.Fatalf("ListAlbum() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != entry.ID {
		t.Fatalf("unexpected album entries: %+v", items)
	}

	preview, err := app.LoadAlbumPreview(entry.ID)
	if err != nil {
		t.Fatalf("LoadAlbumPreview() error = %v", err)
	}
	parts := strings.SplitN(preview, ",", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "data:image/png;base64") {
		t.Fatalf("unexpected preview data URL: %q", preview[:min(len(preview), 40)])
	}
	if _, err := base64.StdEncoding.DecodeString(parts[1]); err != nil {
		t.Fatalf("preview payload is not valid base64: %v", err)
	}
	if _, err := os.Stat(entry.Path); err != nil {
		t.Fatalf("stored image not found: %v", err)
	}
}

func TestAlbumRejectsInvalidKind(t *testing.T) {
	app := &App{}
	_, err := app.saveAlbumImage(image.NewRGBA(image.Rect(0, 0, 1, 1)), "other", "", "")
	if err == nil {
		t.Fatal("expected invalid album kind to be rejected")
	}
}

func TestAlbumDoesNotAllowManualRXHistoryImport(t *testing.T) {
	app := &App{}
	if _, err := app.ImportAlbumImage("rx"); err == nil {
		t.Fatal("manual RX history import should be disabled")
	}
}

func TestStationSettingsPersistAndNormalize(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	app := &App{}
	initial, err := app.GetStationSettings()
	if err != nil {
		t.Fatalf("GetStationSettings() on first run: %v", err)
	}
	if initial.MyCall != defaultStationCallsign || initial.Locator != defaultStationLocator {
		t.Fatalf("unexpected first-run station defaults: got %+v, want %s / %s", initial, defaultStationCallsign, defaultStationLocator)
	}

	saved, err := app.SaveStationSettings(StationSettings{MyCall: " n0call ", Locator: "aa00aa "})
	if err != nil {
		t.Fatalf("SaveStationSettings(): %v", err)
	}
	if saved.MyCall != "N0CALL" || saved.Locator != "AA00AA" {
		t.Fatalf("settings were not normalized: %+v", saved)
	}

	loaded, err := app.GetStationSettings()
	if err != nil {
		t.Fatalf("GetStationSettings() after save: %v", err)
	}
	if loaded != saved {
		t.Fatalf("persisted settings mismatch: got %+v, want %+v", loaded, saved)
	}
}

func TestStationSettingsRejectOversizedValues(t *testing.T) {
	app := &App{}
	if _, err := app.SaveStationSettings(StationSettings{MyCall: strings.Repeat("A", 25)}); err == nil {
		t.Fatal("expected oversized callsign to be rejected")
	}
	if _, err := app.SaveStationSettings(StationSettings{Locator: strings.Repeat("A", 13)}); err == nil {
		t.Fatal("expected oversized locator to be rejected")
	}
}

func TestAudioSettingsPersistAndNormalize(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	app := &App{}
	initial, err := app.GetAudioSettings()
	if err != nil {
		t.Fatalf("GetAudioSettings() on first run: %v", err)
	}
	if initial.SampleRate != 48_000 || initial.PTTMode != "vox" {
		t.Fatalf("unexpected safe first-run audio defaults: %+v", initial)
	}
	if initial.RigctlAddress != "" || initial.SerialPort != "" {
		t.Fatalf("first-run audio defaults must not contain radio endpoint examples: %+v", initial)
	}

	want := AudioSettings{SampleRate: 44_100, CaptureDeviceID: "capture-example-id", PlaybackDeviceID: "playback-example-id", PTTMode: "vox", ActiveHigh: true, PTTLeadMS: 400, PTTTailMS: 300}
	saved, err := app.saveAudioSettings(want)
	if err != nil {
		t.Fatalf("SaveAudioSettings(): %v", err)
	}
	if saved != want {
		t.Fatalf("saved audio settings mismatch: got %+v, want %+v", saved, want)
	}
	loaded, err := app.GetAudioSettings()
	if err != nil {
		t.Fatalf("GetAudioSettings() after save: %v", err)
	}
	if loaded != want {
		t.Fatalf("persisted audio settings mismatch: got %+v, want %+v", loaded, want)
	}
}

func TestLegacyManualPTTSettingsMigrateToVOX(t *testing.T) {
	cfg, err := normalizeAudioSettings(AudioSettings{SampleRate: 48_000, PTTMode: "manual"})
	if err != nil {
		t.Fatalf("legacy manual PTT settings should migrate: %v", err)
	}
	if cfg.PTTMode != "vox" {
		t.Fatalf("legacy manual PTT should migrate to VOX, got %q", cfg.PTTMode)
	}
}

func TestAudioSettingsRejectInvalidConfigurations(t *testing.T) {
	cases := []AudioSettings{
		{SampleRate: 1_000, PTTMode: "manual"},
		{SampleRate: 48_000, PTTMode: "hamlib"},
		{SampleRate: 48_000, PTTMode: "rts"},
		{SampleRate: 48_000, PTTMode: "not-a-mode"},
		{SampleRate: 48_000, PTTMode: "manual", PTTLeadMS: 10_001},
	}
	for i, cfg := range cases {
		if _, err := normalizeAudioSettings(cfg); err == nil {
			t.Fatalf("configuration %d should have been rejected: %+v", i, cfg)
		}
	}
}

func TestAudioTelemetryHelpersReturnFiniteValues(t *testing.T) {
	quiet := make([]float32, 512)
	if got := rmsDBFS(quiet); got > -100 {
		t.Fatalf("quiet signal dBFS = %f, expected a deep noise floor", got)
	}
	for i := range quiet {
		quiet[i] = float32(0.4 * math.Sin(2*math.Pi*1900*float64(i)/48000))
	}
	level := rmsDBFS(quiet)
	if math.IsNaN(level) || math.IsInf(level, 0) || level >= 0 {
		t.Fatalf("unexpected dBFS value: %f", level)
	}
	bins := makeSpectrum(quiet, 48_000)
	if len(bins) != spectrumBins {
		t.Fatalf("spectrum bin count = %d, want %d", len(bins), spectrumBins)
	}
	for i, value := range bins {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			t.Fatalf("invalid spectrum bin %d: %f", i, value)
		}
	}
}

func TestDisconnectAudioWithoutConnectedSessionIsSafe(t *testing.T) {
	app := &App{}
	if err := app.disconnectAudio(); err != nil {
		t.Fatalf("DisconnectAudio() without an active session: %v", err)
	}
	state := app.GetAudioState()
	if state.Connected || state.RXActive || state.TXActive {
		t.Fatalf("unexpected audio state after disconnect: %+v", state)
	}
}

func TestFinishTXDoesNotClearNewerTransmission(t *testing.T) {
	app := &App{txGeneration: 2}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.txCancel = cancel
	app.finishTX(1)
	if app.txCancel == nil {
		t.Fatal("finishing an old TX worker cleared the newer TX cancellation handle")
	}
	app.finishTX(2)
	if app.txCancel != nil {
		t.Fatal("finishing the active TX worker did not clear its cancellation handle")
	}
}

func TestReplaceFilePreservingPreviousAndRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFilePreservingPrevious(tmp, target); err != nil {
		t.Fatalf("replace existing file: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("target after replace = %q, err=%v, want new", got, err)
	}

	// A missing temporary file forces the second rename to fail. The previous
	// valid file must be restored from the backup in that case.
	if err := replaceFilePreservingPrevious(tmp, target); err == nil {
		t.Fatal("expected replace with a missing temporary file to fail")
	}
	got, err = os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("target after rollback = %q, err=%v, want previous new contents", got, err)
	}
}

func TestTemplateSlotsDefaultAndPersistence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	app := &App{}
	slots, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatalf("GetTemplateSlots() defaults: %v", err)
	}
	if len(slots) != 4 || slots[0].Name != "CQ" || slots[1].Name != "RST" || slots[2].Name != "73" || slots[3].Name != "空白模板" || len(slots[3].Objects) != 0 {
		t.Fatalf("unexpected default template slots: %+v", slots)
	}

	slots[0].Name = "CQ TEST"
	slots[0].Objects = append(slots[0].Objects, TemplateObject{Type: "text", Text: "{MYCALL}", X: 8, Y: 90, FontSize: 16})
	saved, err := app.SaveTemplateSlots(slots)
	if err != nil {
		t.Fatalf("SaveTemplateSlots(): %v", err)
	}
	loaded, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatalf("GetTemplateSlots() after save: %v", err)
	}
	encodedWant, _ := json.Marshal(saved)
	encodedGot, _ := json.Marshal(loaded)
	if string(encodedWant) != string(encodedGot) {
		t.Fatalf("template persistence mismatch\n got: %s\nwant: %s", encodedGot, encodedWant)
	}
}

func TestTemplateSlotsMigrateLegacyCQCaption(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	app := &App{}
	slots := defaultTemplateSlots()
	slots[0].Objects[0].Text = "CQ CQ CQ"
	if _, err := app.SaveTemplateSlots(slots); err != nil {
		t.Fatalf("save legacy template: %v", err)
	}
	loaded, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatalf("load migrated template: %v", err)
	}
	if got := loaded[0].Objects[0].Text; got != "CQ SSTV" {
		t.Fatalf("legacy built-in CQ caption = %q, want CQ SSTV", got)
	}
}

func TestTemplateImageLayerValidationAndPersistence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	app := &App{}
	slots, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatal(err)
	}
	slots[3].Name = "PHOTO TEST"
	slots[3].Objects = []TemplateObject{{Type: "image", ImageData: dataURL, ImageName: "RX sample", X: 10, Y: 20, Width: 40, Height: 35}}
	if _, err := app.SaveTemplateSlots(slots); err != nil {
		t.Fatalf("valid image layer rejected: %v", err)
	}

	slots[3].Objects[0].ImageData = "data:image/png;base64,not-a-png"
	if _, err := app.SaveTemplateSlots(slots); err == nil {
		t.Fatal("invalid embedded image payload should be rejected")
	}
}

func TestAlbumLatestRXAndDelete(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	app := &App{}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	rx, err := app.saveAlbumImage(img, "rx", "robot36", "RX sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.saveAlbumImage(img, "tx", "robot36", "TX sample"); err != nil {
		t.Fatal(err)
	}
	latest, err := app.GetLatestRXImage()
	if err != nil {
		t.Fatalf("GetLatestRXImage(): %v", err)
	}
	if !strings.HasPrefix(latest, "data:image/png;base64,") {
		t.Fatalf("unexpected latest RX data URL: %q", latest[:min(len(latest), 40)])
	}
	if err := app.DeleteAlbumImage(rx.ID); err != nil {
		t.Fatalf("DeleteAlbumImage(): %v", err)
	}
	entries, err := app.ListAlbum()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "tx" {
		t.Fatalf("unexpected album after RX deletion: %+v", entries)
	}
	if _, err := os.Stat(rx.Path); !os.IsNotExist(err) {
		t.Fatalf("deleted image should no longer exist, stat err = %v", err)
	}
	latest, err = app.GetLatestRXImage()
	if err != nil || latest != "" {
		t.Fatalf("GetLatestRXImage() after deleting last RX = %q, %v; want empty without error", latest, err)
	}
	if err := app.DeleteAlbumImage("../album.json"); err == nil {
		t.Fatal("path-like album ID should be rejected")
	}
}

func TestLastRXTemplateComponentAndEmptyLatestImage(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("APPDATA", tmp)
	t.Setenv("HOME", tmp)

	app := &App{}
	latest, err := app.GetLatestRXImage()
	if err != nil || latest != "" {
		t.Fatalf("empty GetLatestRXImage() = %q, %v; want empty string without error", latest, err)
	}
	slots, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatal(err)
	}
	slots[3].Objects = []TemplateObject{{Type: "last_rx", ImageName: "上一次 RX 图像", X: 8, Y: 18, Width: 42, Height: 42}}
	if _, err := app.SaveTemplateSlots(slots); err != nil {
		t.Fatalf("save last-RX placeholder component: %v", err)
	}
	loaded, err := app.GetTemplateSlots()
	if err != nil {
		t.Fatalf("load last-RX placeholder component: %v", err)
	}
	if got := loaded[3].Objects[0]; got.Type != "last_rx" || got.ImageData != "" || got.Width != 42 || got.Height != 42 {
		t.Fatalf("last-RX template component = %+v", got)
	}
}
