package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxLibraryImageBytes  = 14 << 20
	maxLibraryImagePixels = 32_000_000
	maxLibraryThumbnail   = 320
)

type ImageLibraryEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Size      int64  `json:"size"`
	AddedAt   string `json:"addedAt"`
	Thumbnail string `json:"thumbnail"`
}

type imageLibraryIndex struct {
	Version int                 `json:"version"`
	Entries []ImageLibraryEntry `json:"entries"`
}

func (a *App) imageLibraryPaths() (string, string, error) {
	dir, err := a.settingsDir()
	if err != nil {
		return "", "", err
	}
	libraryDir := filepath.Join(dir, "image-library")
	if err := os.MkdirAll(libraryDir, 0o700); err != nil {
		return "", "", fmt.Errorf("创建图库目录失败：%w", err)
	}
	return libraryDir, filepath.Join(libraryDir, "index.json"), nil
}

func readImageLibraryIndex(path string) (imageLibraryIndex, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return imageLibraryIndex{Version: 1, Entries: []ImageLibraryEntry{}}, nil
	}
	if err != nil {
		return imageLibraryIndex{}, fmt.Errorf("读取图库索引失败：%w", err)
	}
	var index imageLibraryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return imageLibraryIndex{}, fmt.Errorf("图库索引格式错误：%w", err)
	}
	if index.Version == 0 {
		index.Version = 1
	}
	if index.Entries == nil {
		index.Entries = []ImageLibraryEntry{}
	}
	return index, nil
}

func writeImageLibraryIndex(path string, index imageLibraryIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化图库索引失败：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入图库索引失败：%w", err)
	}
	if err := replaceFilePreservingPrevious(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存图库索引失败：%w", err)
	}
	return nil
}

// ListImageLibrary returns the persistent image gallery with small thumbnails.
func (a *App) ListImageLibrary() ([]ImageLibraryEntry, error) {
	a.imageLibraryMu.Lock()
	defer a.imageLibraryMu.Unlock()
	libraryDir, indexPath, err := a.imageLibraryPaths()
	if err != nil {
		return nil, err
	}
	index, err := readImageLibraryIndex(indexPath)
	if err != nil {
		return nil, err
	}
	entries := make([]ImageLibraryEntry, 0, len(index.Entries))
	indexChanged := false
	for i, entry := range index.Entries {
		if !validImageLibraryID(entry.ID) {
			continue
		}
		imagePath := filepath.Join(libraryDir, entry.ID+".png")
		if _, statErr := os.Stat(imagePath); statErr != nil {
			// A missing image is skipped instead of making the whole gallery
			// unavailable; the stale index entry is harmless and can be repaired.
			continue
		}
		if !strings.HasPrefix(entry.Thumbnail, "data:image/jpeg;base64,") {
			// Older gallery indexes did not persist thumbnails. Generate them once
			// and update the index so subsequent gallery opens do not decode every
			// full-resolution original image again.
			data, readErr := os.ReadFile(imagePath)
			if readErr != nil {
				continue
			}
			img, _, decodeErr := image.Decode(bytes.NewReader(data))
			if decodeErr != nil {
				continue
			}
			entry.Thumbnail, err = encodeThumbnailDataURL(img)
			if err != nil {
				return nil, err
			}
			index.Entries[i].Thumbnail = entry.Thumbnail
			indexChanged = true
		}
		entries = append(entries, entry)
	}
	if indexChanged {
		if err := writeImageLibraryIndex(indexPath, index); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// ImportImageToLibrary opens a native image picker and stores the selected
// image as PNG in the user's persistent SATSSTV-Go image library.
func (a *App) ImportImageToLibrary() (ImageLibraryEntry, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "导入图片到 SSTV 图库",
		Filters: []runtime.FileFilter{{DisplayName: "图片文件", Pattern: "*.png;*.jpg;*.jpeg"}},
	})
	if err != nil {
		return ImageLibraryEntry{}, err
	}
	if path == "" {
		return ImageLibraryEntry{}, errors.New("已取消导入")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ImageLibraryEntry{}, fmt.Errorf("读取图片失败：%w", err)
	}
	if len(data) == 0 {
		return ImageLibraryEntry{}, errors.New("图片文件为空")
	}
	if len(data) > maxLibraryImageBytes {
		return ImageLibraryEntry{}, fmt.Errorf("图片文件过大，最大支持 %d MiB", maxLibraryImageBytes>>20)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ImageLibraryEntry{}, fmt.Errorf("读取图片尺寸失败：%w", err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > maxLibraryImagePixels {
		return ImageLibraryEntry{}, errors.New("图片尺寸无效或超过 3200 万像素限制")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImageLibraryEntry{}, fmt.Errorf("解析图片失败：%w", err)
	}
	return a.saveImageLibraryImage(filepath.Base(path), img)
}

func (a *App) saveImageLibraryImage(name string, img image.Image) (ImageLibraryEntry, error) {
	if img == nil {
		return ImageLibraryEntry{}, errors.New("图片为空")
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 || int64(width)*int64(height) > maxLibraryImagePixels {
		return ImageLibraryEntry{}, errors.New("图片尺寸无效或超过 3200 万像素限制")
	}
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "图库图片"
	}
	if utf8.RuneCountInString(name) > 100 {
		name = string([]rune(name)[:100])
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return ImageLibraryEntry{}, fmt.Errorf("转换 PNG 失败：%w", err)
	}
	if encoded.Len() > maxLibraryImageBytes {
		return ImageLibraryEntry{}, fmt.Errorf("转换后的图片超过 %d MiB 限制", maxLibraryImageBytes>>20)
	}
	thumbnail, err := encodeThumbnailDataURL(img)
	if err != nil {
		return ImageLibraryEntry{}, fmt.Errorf("生成图库缩略图失败：%w", err)
	}

	a.imageLibraryMu.Lock()
	defer a.imageLibraryMu.Unlock()
	libraryDir, indexPath, err := a.imageLibraryPaths()
	if err != nil {
		return ImageLibraryEntry{}, err
	}
	index, err := readImageLibraryIndex(indexPath)
	if err != nil {
		return ImageLibraryEntry{}, err
	}
	id := fmt.Sprintf("img-%x", time.Now().UnixNano())
	for attempt := 0; ; attempt++ {
		_, statErr := os.Stat(filepath.Join(libraryDir, id+".png"))
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return ImageLibraryEntry{}, fmt.Errorf("检查图库文件名失败：%w", statErr)
		}
		id = fmt.Sprintf("img-%x", time.Now().UnixNano()+int64(len(index.Entries)+attempt+1))
	}
	path := filepath.Join(libraryDir, id+".png")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded.Bytes(), 0o600); err != nil {
		return ImageLibraryEntry{}, fmt.Errorf("写入图库图片失败：%w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return ImageLibraryEntry{}, fmt.Errorf("安装图库图片失败：%w", err)
	}
	entry := ImageLibraryEntry{
		ID: id, Name: name, Width: width, Height: height,
		Size: int64(encoded.Len()), AddedAt: time.Now().Format("2006-01-02 15:04"),
		Thumbnail: thumbnail,
	}
	index.Version = 1
	index.Entries = append([]ImageLibraryEntry{entry}, index.Entries...)
	if err := writeImageLibraryIndex(indexPath, index); err != nil {
		_ = os.Remove(path)
		return ImageLibraryEntry{}, err
	}
	return entry, nil
}

// LoadImageLibraryImage returns the selected full-resolution image as a PNG
// data URL. Templates embed their own copy, so they remain portable even if
// the gallery is later modified.
func (a *App) LoadImageLibraryImage(id string) (string, error) {
	if !validImageLibraryID(id) {
		return "", errors.New("图库图片 ID 无效")
	}
	a.imageLibraryMu.Lock()
	defer a.imageLibraryMu.Unlock()
	libraryDir, indexPath, err := a.imageLibraryPaths()
	if err != nil {
		return "", err
	}
	index, err := readImageLibraryIndex(indexPath)
	if err != nil {
		return "", err
	}
	found := false
	for _, entry := range index.Entries {
		if entry.ID == id {
			found = true
			break
		}
	}
	if !found {
		return "", errors.New("图库中不存在该图片")
	}
	data, err := os.ReadFile(filepath.Join(libraryDir, id+".png"))
	if err != nil {
		return "", fmt.Errorf("读取图库图片失败：%w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// DeleteImageLibraryImage removes a gallery entry. Existing templates remain
// valid because image components and background layers embed their own data.
func (a *App) DeleteImageLibraryImage(id string) error {
	if !validImageLibraryID(id) {
		return errors.New("图库图片 ID 无效")
	}
	a.imageLibraryMu.Lock()
	defer a.imageLibraryMu.Unlock()
	libraryDir, indexPath, err := a.imageLibraryPaths()
	if err != nil {
		return err
	}
	index, err := readImageLibraryIndex(indexPath)
	if err != nil {
		return err
	}
	filtered := make([]ImageLibraryEntry, 0, len(index.Entries))
	found := false
	for _, entry := range index.Entries {
		if entry.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, entry)
	}
	if !found {
		return errors.New("图库中不存在该图片")
	}
	index.Entries = filtered
	if err := writeImageLibraryIndex(indexPath, index); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(libraryDir, id+".png")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除图库图片文件失败：%w", err)
	}
	return nil
}

func validImageLibraryID(id string) bool {
	if len(id) < 5 || len(id) > 64 || !strings.HasPrefix(id, "img-") {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

func encodeThumbnailDataURL(source image.Image) (string, error) {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 1 || height < 1 {
		return "", errors.New("无法生成空图片缩略图")
	}
	scale := float64(maxLibraryThumbnail) / float64(max(width, height))
	thumbWidth := max(1, int(float64(width)*scale))
	thumbHeight := max(1, int(float64(height)*scale))
	thumb := image.NewRGBA(image.Rect(0, 0, thumbWidth, thumbHeight))
	for y := 0; y < thumbHeight; y++ {
		sy := bounds.Min.Y + y*height/thumbHeight
		for x := 0; x < thumbWidth; x++ {
			sx := bounds.Min.X + x*width/thumbWidth
			thumb.Set(x, y, source.At(sx, sy))
		}
	}
	var data bytes.Buffer
	if err := jpeg.Encode(&data, thumb, &jpeg.Options{Quality: 82}); err != nil {
		return "", err
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data.Bytes()), nil
}
