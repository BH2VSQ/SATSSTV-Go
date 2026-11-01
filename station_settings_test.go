package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func useTempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
}

func TestGetStationSettingsDefaultsOnFirstRun(t *testing.T) {
	useTempConfigDir(t)
	got, err := (&App{}).GetStationSettings()
	if err != nil {
		t.Fatalf("GetStationSettings() error = %v", err)
	}
	if got.MyCall != "N0CALL" || got.Locator != "JJ00" {
		t.Fatalf("unexpected first-run defaults: %+v", got)
	}
}

func TestGetStationSettingsMigratesEmptyLegacyValues(t *testing.T) {
	useTempConfigDir(t)
	app := &App{}
	path, err := app.stationSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"myCall":"  ","locator":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := app.GetStationSettings()
	if err != nil {
		t.Fatalf("GetStationSettings() error = %v", err)
	}
	if got.MyCall != "N0CALL" || got.Locator != "JJ00" {
		t.Fatalf("legacy empty values were not migrated: %+v", got)
	}
}

func TestSaveStationSettingsUsesDefaultsForBlankValues(t *testing.T) {
	useTempConfigDir(t)
	app := &App{}
	got, err := app.SaveStationSettings(StationSettings{})
	if err != nil {
		t.Fatalf("SaveStationSettings() error = %v", err)
	}
	if got.MyCall != "N0CALL" || got.Locator != "JJ00" {
		t.Fatalf("unexpected normalized settings: %+v", got)
	}
	loaded, err := app.GetStationSettings()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != got {
		t.Fatalf("saved settings = %+v, loaded = %+v", got, loaded)
	}
}
