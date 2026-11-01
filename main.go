package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BH2VSQ/SATSSTV-Go/internal/radio/ptt"
	"github.com/BH2VSQ/SATSSTV-Go/internal/sstv"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend
var assets embed.FS

type App struct {
	ctx context.Context

	audioMu           sync.Mutex
	rxOpMu            sync.Mutex
	txOpMu            sync.Mutex // serializes TX start and audio-session replacement/close
	audioSession      appAudioRuntime
	audioConfig       AudioSettings
	rxCancel          context.CancelFunc
	txCancel          context.CancelFunc
	txGeneration      uint64 // prevents an older TX worker from clearing a newer TX state
	pttTestMu         sync.Mutex
	pttTestController ptt.Controller
	pttTestCancel     context.CancelFunc
	pttTestGeneration uint64
	shuttingDown      atomic.Bool
	rxDecoderMu       sync.Mutex
	rxDecoder         *sstv.StreamDecoder
	rxModeRequest     atomic.Pointer[string]
	rxQueueDrops      atomic.Uint64
	albumMu           sync.Mutex
	imageLibraryMu    sync.Mutex

	dumpMu    sync.Mutex
	dumpBuf   []float32 // fixed-size circular buffer of recent raw captured float samples
	dumpWrite int
	dumpCount int
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if cfg, err := a.readAudioSettings(); err == nil {
		a.audioConfig = cfg
	} else {
		a.audioConfig = defaultAudioSettings()
	}
}

type Mode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Ready       bool   `json:"ready"`
	Description string `json:"description"`
}

type AlbumEntry struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // rx or tx
	Mode        string `json:"mode"`
	Timestamp   string `json:"timestamp"`
	DisplayTime string `json:"displayTime"`
	Filename    string `json:"filename"`
	Status      string `json:"status"`
	Path        string `json:"path"`
}

func (a *App) GetModes() []Mode {
	out := make([]Mode, 0)
	for _, m := range sstv.Modes() {
		out = append(out, Mode{m.ID, m.Name, m.Width, m.Height, m.CodecReady, m.Description})
	}
	return out
}

// ListSerialPorts returns the COM ports currently available for serial PTT.
func (a *App) ListSerialPorts() []string { return listSerialPorts() }

func (a *App) SelectImage() string {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "选择 SSTV 发送背景图片",
		Filters: []runtime.FileFilter{{DisplayName: "图片文件", Pattern: "*.png;*.jpg;*.jpeg"}},
	})
	if err != nil {
		return ""
	}
	return p
}

func (a *App) LoadImagePreview(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("图片文件为空")
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return "", fmt.Errorf("读取图片失败：%w", err)
	}
	mime := "image/png"
	if ext := strings.ToLower(filepath.Ext(path)); ext == ".jpg" || ext == ".jpeg" {
		mime = "image/jpeg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (a *App) EncodeImageData(dataURL, fileName, mode string, rate int) (string, error) {
	img, err := decodeDataURL(dataURL)
	if err != nil {
		return "", err
	}
	return a.encodeAndSave(img, fileName, mode, rate)
}

func decodeDataURL(dataURL string) (image.Image, error) {
	if strings.TrimSpace(dataURL) == "" {
		return nil, fmt.Errorf("发送画面为空")
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
		return nil, fmt.Errorf("图像数据格式无效")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("解码图像数据失败：%w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解析图像失败：%w", err)
	}
	return img, nil
}

func (a *App) encodeAndSave(img image.Image, fileName, mode string, rate int) (string, error) {
	if rate < 8000 || rate > 192000 {
		return "", fmt.Errorf("采样率应为 8000 至 192000 Hz")
	}
	spec, err := sstv.LookupMode(mode)
	if err != nil {
		return "", err
	}
	if !spec.CodecReady {
		return "", fmt.Errorf("%s 的编码器尚未实现", spec.Name)
	}
	samples, err := sstv.NewEncoder(rate).Encode(a.ctx, img, mode)
	if err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	if base == "" || base == "." {
		base = "SATSSTV-Go"
	}
	out, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "保存 SSTV 音频", DefaultFilename: base + "-" + mode + ".wav",
		Filters: []runtime.FileFilter{{DisplayName: "WAV 音频", Pattern: "*.wav"}},
	})
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("已取消保存")
	}
	wf, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer wf.Close()
	if err := sstv.WriteWAV(wf, samples, rate); err != nil {
		return "", err
	}
	return out, nil
}

func (a *App) albumDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "SATSSTV-Go", "album")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (a *App) albumIndexPath() (string, error) {
	dir, err := a.albumDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "album.json"), nil
}

func (a *App) readAlbumIndex() ([]AlbumEntry, error) {
	indexPath, err := a.albumIndexPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		return []AlbumEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []AlbumEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("相册索引损坏：%w", err)
	}
	return entries, nil
}

func (a *App) writeAlbumIndex(entries []AlbumEntry) error {
	indexPath, err := a.albumIndexPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := indexPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := replaceFilePreservingPrevious(tmp, indexPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (a *App) ListAlbum() ([]AlbumEntry, error) {
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	entries, err := a.readAlbumIndex()
	if err != nil {
		return nil, err
	}
	valid := entries[:0]
	for _, entry := range entries {
		if _, err := os.Stat(entry.Path); err == nil {
			valid = append(valid, entry)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Timestamp > valid[j].Timestamp })
	return valid, nil
}

func (a *App) LoadAlbumPreview(id string) (string, error) {
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	entries, err := a.readAlbumIndex()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			return "", err
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return "", fmt.Errorf("相册记录不存在")
}

// GetLatestRXImage returns the most recent archived RX image as a data URL.
// It is used by the TX template editor to add the previous received image.
func (a *App) GetLatestRXImage() (string, error) {
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	entries, err := a.readAlbumIndex()
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp > entries[j].Timestamp })
	for _, entry := range entries {
		if entry.Kind != "rx" {
			continue
		}
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			return "", err
		}
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	// No previous RX image is a normal state. The frontend renders an empty
	// last-RX placeholder and fills it when the first frame is received.
	return "", nil
}

// DeleteAlbumImage removes one archived image and its index entry.
func (a *App) DeleteAlbumImage(id string) error {
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("无效的相册记录 ID")
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	entries, err := a.readAlbumIndex()
	if err != nil {
		return err
	}
	filtered := make([]AlbumEntry, 0, len(entries))
	var selected *AlbumEntry
	for i := range entries {
		if entries[i].ID == id {
			copy := entries[i]
			selected = &copy
			continue
		}
		filtered = append(filtered, entries[i])
	}
	if selected == nil {
		return fmt.Errorf("相册记录不存在")
	}
	if err := a.writeAlbumIndex(filtered); err != nil {
		return fmt.Errorf("更新相册索引失败：%w", err)
	}
	if err := os.Remove(selected.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("相册记录已移除，但图像文件删除失败：%w", err)
	}
	return nil
}

func (a *App) SaveAlbumImage(dataURL, kind, mode, status string) (AlbumEntry, error) {
	img, err := decodeDataURL(dataURL)
	if err != nil {
		return AlbumEntry{}, err
	}
	return a.saveAlbumImage(img, kind, mode, status)
}

func (a *App) ImportAlbumImage(kind string) (AlbumEntry, error) {
	// RX images are generated from the live receiver and archived automatically.
	// Only TX drafts may be manually imported into the album.
	if kind != "tx" {
		return AlbumEntry{}, fmt.Errorf("仅允许导入 TX 图像，RX 图像由实时接收自动归档")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入 TX 历史图像到相册", Filters: []runtime.FileFilter{{DisplayName: "图片文件", Pattern: "*.png;*.jpg;*.jpeg"}},
	})
	if err != nil {
		return AlbumEntry{}, err
	}
	if path == "" {
		return AlbumEntry{}, fmt.Errorf("已取消导入")
	}
	file, err := os.Open(path)
	if err != nil {
		return AlbumEntry{}, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return AlbumEntry{}, err
	}
	return a.saveAlbumImage(img, kind, "", "从文件导入")
}

func (a *App) saveAlbumImage(img image.Image, kind, mode, status string) (AlbumEntry, error) {
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	if kind != "rx" && kind != "tx" {
		return AlbumEntry{}, fmt.Errorf("相册分类必须为 rx 或 tx")
	}
	dir, err := a.albumDir()
	if err != nil {
		return AlbumEntry{}, err
	}
	now := time.Now()
	id := fmt.Sprintf("%d", now.UnixNano())
	prefix := strings.ToUpper(kind)
	filename := fmt.Sprintf("%s_%s_%03d.png", prefix, now.Format("20060102_150405"), now.Nanosecond()/1e6)
	path := filepath.Join(dir, id+".png")
	f, err := os.Create(path)
	if err != nil {
		return AlbumEntry{}, err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(path)
		return AlbumEntry{}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return AlbumEntry{}, err
	}
	entry := AlbumEntry{ID: id, Kind: kind, Mode: mode, Timestamp: now.Format(time.RFC3339Nano), DisplayTime: now.Format("2006-01-02 15:04:05"), Filename: filename, Status: status, Path: path}
	entries, err := a.readAlbumIndex()
	if err != nil {
		os.Remove(path)
		return AlbumEntry{}, err
	}
	entries = append(entries, entry)
	if err := a.writeAlbumIndex(entries); err != nil {
		os.Remove(path)
		return AlbumEntry{}, err
	}
	return entry, nil
}

func (a *App) ExportAlbumImage(id string) (string, error) {
	// Snapshot the entry under the album lock, then release it before showing
	// the native save dialog. RX archival must remain unblocked while a user
	// chooses the export destination.
	a.albumMu.Lock()
	entries, err := a.readAlbumIndex()
	if err != nil {
		a.albumMu.Unlock()
		return "", err
	}
	var selected *AlbumEntry
	for i := range entries {
		if entries[i].ID == id {
			copy := entries[i]
			selected = &copy
			break
		}
	}
	a.albumMu.Unlock()
	if selected == nil {
		return "", fmt.Errorf("相册记录不存在")
	}

	out, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "导出带时间戳的 SSTV 图像", DefaultFilename: selected.Filename,
		Filters: []runtime.FileFilter{{DisplayName: "PNG 图像", Pattern: "*.png"}},
	})
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("已取消导出")
	}
	in, err := os.Open(selected.Path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	dst, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, in); err != nil {
		return "", err
	}
	return out, nil
}

func main() {
	app := &App{}
	err := wails.Run(&options.App{
		Title: "SATSSTV-Go", Width: 1420, Height: 900, MinWidth: 1050, MinHeight: 680,
		AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, OnShutdown: app.shutdown, Bind: []interface{}{app},
	})
	if err != nil {
		println("启动 GUI 失败:", err.Error())
	}
}
