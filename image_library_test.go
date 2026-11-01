package main

import (
	"encoding/base64"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestImageLibraryPersistsAndLoadsImages(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{}
	img := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 7), B: 120, A: 255})
		}
	}

	entry, err := app.saveImageLibraryImage("sample-image.jpg", img)
	if err != nil {
		t.Fatalf("save image: %v", err)
	}
	if entry.ID == "" || entry.Width != 48 || entry.Height != 32 || entry.Size == 0 {
		t.Fatalf("unexpected image metadata: %+v", entry)
	}
	if !strings.HasPrefix(entry.Thumbnail, "data:image/jpeg;base64,") {
		t.Fatalf("thumbnail is not a JPEG data URL: %q", entry.Thumbnail[:min(len(entry.Thumbnail), 40)])
	}

	entries, err := app.ListImageLibrary()
	if err != nil {
		t.Fatalf("list image library: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("image library entry was not persisted: %+v", entries)
	}

	dataURL, err := app.LoadImageLibraryImage(entry.ID)
	if err != nil {
		t.Fatalf("load image: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Fatalf("loaded image is not a PNG data URL")
	}
	encoded := strings.TrimPrefix(dataURL, "data:image/png;base64,")
	if _, err := base64.StdEncoding.DecodeString(encoded); err != nil {
		t.Fatalf("loaded image payload is invalid base64: %v", err)
	}

	if err := app.DeleteImageLibraryImage(entry.ID); err != nil {
		t.Fatalf("delete image: %v", err)
	}
	if _, err := app.LoadImageLibraryImage(entry.ID); err == nil {
		t.Fatal("deleted image should not be loadable")
	}
	entries, err = app.ListImageLibrary()
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected empty library after deletion, got %+v, error %v", entries, err)
	}
}

func TestImageLibraryRejectsInvalidIDs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{}
	if _, err := app.LoadImageLibraryImage("../../settings"); err == nil {
		t.Fatal("path traversal ID should be rejected")
	}
	if err := app.DeleteImageLibraryImage("../../settings"); err == nil {
		t.Fatal("path traversal ID should be rejected")
	}
}

func TestImageLibraryMigratesLegacyIndexWithoutThumbnail(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{}
	img := image.NewRGBA(image.Rect(0, 0, 24, 18))
	entry, err := app.saveImageLibraryImage("legacy.png", img)
	if err != nil {
		t.Fatal(err)
	}
	_, indexPath, err := app.imageLibraryPaths()
	if err != nil {
		t.Fatal(err)
	}
	index, err := readImageLibraryIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	index.Entries[0].Thumbnail = "" // Simulate an index from the pre-thumbnail version.
	if err := writeImageLibraryIndex(indexPath, index); err != nil {
		t.Fatal(err)
	}
	entries, err := app.ListImageLibrary()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID || !strings.HasPrefix(entries[0].Thumbnail, "data:image/jpeg;base64,") {
		t.Fatalf("legacy image index was not migrated: %+v", entries)
	}
	index, err = readImageLibraryIndex(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(index.Entries[0].Thumbnail, "data:image/jpeg;base64,") {
		t.Fatal("generated thumbnail was not persisted into the gallery index")
	}
}
