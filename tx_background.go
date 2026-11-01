package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// txBackgroundPath stores the currently selected TX background independently
// of the four template slots. Switching templates therefore never changes the
// background, and the last selected image survives application restarts.
func (a *App) txBackgroundPath() (string, error) {
	dir, err := a.settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tx-background.json"), nil
}

// GetTXBackground returns the globally selected TX background. A nil result
// means that the user has not selected a background yet.
func (a *App) GetTXBackground() (*TemplateObject, error) {
	path, err := a.txBackgroundPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 TX 底图失败：%w", err)
	}
	var background TemplateObject
	if err := json.Unmarshal(data, &background); err != nil {
		return nil, fmt.Errorf("TX 底图配置损坏：%w", err)
	}
	if err := validateTXBackground(background); err != nil {
		return nil, fmt.Errorf("TX 底图配置无效：%w", err)
	}
	return &background, nil
}

// SaveTXBackground persists the global TX background independently of any
// template slot. Image data is embedded so moving/deleting the source library
// entry cannot break an already selected background.
func (a *App) SaveTXBackground(background TemplateObject) (TemplateObject, error) {
	background.Type = "background"
	background.ImageName = strings.TrimSpace(background.ImageName)
	if background.ImageName == "" {
		background.ImageName = "图库图片"
	}
	if background.Width == 0 {
		background.Width = 100
	}
	if err := validateTXBackground(background); err != nil {
		return TemplateObject{}, err
	}
	path, err := a.txBackgroundPath()
	if err != nil {
		return TemplateObject{}, err
	}
	data, err := json.MarshalIndent(background, "", "  ")
	if err != nil {
		return TemplateObject{}, fmt.Errorf("序列化 TX 底图失败：%w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return TemplateObject{}, fmt.Errorf("写入 TX 底图失败：%w", err)
	}
	if err := replaceFilePreservingPrevious(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return TemplateObject{}, fmt.Errorf("保存 TX 底图失败：%w", err)
	}
	return background, nil
}

// ClearTXBackground explicitly clears the global background. Template changes
// do not call this method; only the user's clear-background action does.
func (a *App) ClearTXBackground() error {
	path, err := a.txBackgroundPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清除 TX 底图失败：%w", err)
	}
	if err := os.Remove(path + ".bak"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清除 TX 底图备份失败：%w", err)
	}
	if err := os.Remove(path + ".tmp"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清除 TX 底图临时文件失败：%w", err)
	}
	return nil
}

func validateTXBackground(background TemplateObject) error {
	if background.Type != "background" {
		return fmt.Errorf("底图类型无效")
	}
	if background.X < -150 || background.X > 150 || background.Y < -150 || background.Y > 150 || background.Width < 100 || background.Width > 400 {
		return fmt.Errorf("底图缩放或偏移参数无效")
	}
	total := 0
	return validateTemplateImage("全局 TX 底图", 0, background.ImageData, &total)
}
