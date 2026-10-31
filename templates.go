package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TemplateObject is one editable text, static image, or dynamic last-RX layer on the SSTV canvas.
type TemplateObject struct {
	Type      string  `json:"type"`
	Text      string  `json:"text,omitempty"`
	ImageData string  `json:"imageData,omitempty"`
	ImageName string  `json:"imageName,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width,omitempty"`
	Height    float64 `json:"height,omitempty"`
	FontSize  int     `json:"fontSize,omitempty"`
	Color     string  `json:"color,omitempty"`
	Bold      bool    `json:"bold,omitempty"`
}

// TemplateSlot is a persistent one-of-four template position.
type TemplateSlot struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Objects []TemplateObject `json:"objects"`
}

func defaultTemplateSlots() []TemplateSlot {
	return []TemplateSlot{
		{ID: "cq", Name: "CQ", Objects: []TemplateObject{
			{Type: "text", Text: "CQ SSTV", X: 6, Y: 8, FontSize: 29, Color: "#f5a9b8", Bold: true},
			{Type: "text", Text: "DE {MYCALL}", X: 6, Y: 24, FontSize: 19, Color: "#ffffff", Bold: true},
			{Type: "text", Text: "{LOCATOR}", X: 6, Y: 86, FontSize: 12, Color: "#ffffff"},
		}},
		{ID: "rst", Name: "RST", Objects: []TemplateObject{
			{Type: "text", Text: "{HISCALL}", X: 6, Y: 8, FontSize: 28, Color: "#f5a9b8", Bold: true},
			{Type: "text", Text: "RST {REPORT}", X: 6, Y: 28, FontSize: 21, Color: "#ffffff", Bold: true},
			{Type: "text", Text: "FROM {MYCALL}", X: 6, Y: 86, FontSize: 14, Color: "#ffffff"},
		}},
		{ID: "73", Name: "73", Objects: []TemplateObject{
			{Type: "text", Text: "73", X: 7, Y: 10, FontSize: 52, Color: "#f5a9b8", Bold: true},
			{Type: "text", Text: "BEST REGARDS", X: 7, Y: 48, FontSize: 17, Color: "#ffffff", Bold: true},
			{Type: "text", Text: "{MYCALL}", X: 7, Y: 84, FontSize: 17, Color: "#ffffff", Bold: true},
		}},
		{ID: "slot4", Name: "空白模板", Objects: []TemplateObject{}},
	}
}

func (a *App) templateSlotsPath() (string, error) {
	dir, err := a.settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "templates.json"), nil
}

func (a *App) GetTemplateSlots() ([]TemplateSlot, error) {
	path, err := a.templateSlotsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaultTemplateSlots(), nil
	}
	if err != nil {
		return nil, err
	}
	var slots []TemplateSlot
	if err := json.Unmarshal(data, &slots); err != nil {
		return nil, fmt.Errorf("模板配置文件损坏：%w", err)
	}
	if err := validateTemplateSlots(slots); err != nil {
		return nil, fmt.Errorf("模板配置文件无效：%w", err)
	}
	// Migrate the built-in v3.5 CQ caption without changing user-authored
	// template text in the other slots.
	if len(slots) > 0 && slots[0].ID == "cq" && len(slots[0].Objects) > 0 {
		if slots[0].Objects[0].Type == "text" && strings.EqualFold(strings.TrimSpace(slots[0].Objects[0].Text), "CQ CQ CQ") {
			slots[0].Objects[0].Text = "CQ SSTV"
		}
	}
	return slots, nil
}

func (a *App) SaveTemplateSlots(slots []TemplateSlot) ([]TemplateSlot, error) {
	if err := validateTemplateSlots(slots); err != nil {
		return nil, err
	}
	path, err := a.templateSlotsPath()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(slots, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, fmt.Errorf("模板文件超过 64 MiB，请缩小模板图片")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return nil, err
	}
	if err := replaceFilePreservingPrevious(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return slots, nil
}

func validateTemplateSlots(slots []TemplateSlot) error {
	if len(slots) != 4 {
		return fmt.Errorf("模板必须恰好包含四个槽位")
	}
	seen := map[string]bool{}
	totalImageBytes := 0
	for i := range slots {
		slot := &slots[i]
		if strings.TrimSpace(slot.ID) == "" || seen[slot.ID] {
			return fmt.Errorf("模板槽位 %d 的 ID 为空或重复", i+1)
		}
		seen[slot.ID] = true
		slot.Name = strings.TrimSpace(slot.Name)
		if slot.Name == "" {
			return fmt.Errorf("模板槽位 %d 必须设置名称", i+1)
		}
		if len(slot.Objects) > 100 {
			return fmt.Errorf("模板 %q 的组件数量不能超过 100", slot.Name)
		}
		for j := range slot.Objects {
			obj := &slot.Objects[j]
			if obj.Type == "" {
				obj.Type = "text" // Backward compatibility with v3.4 text-only templates.
			}
			if obj.X < 0 || obj.X > 100 || obj.Y < 0 || obj.Y > 100 {
				return fmt.Errorf("模板 %q 的组件 %d 位置超出画布范围", slot.Name, j+1)
			}
			switch obj.Type {
			case "text":
				if len(obj.Text) > 4096 || obj.FontSize < 0 || obj.FontSize > 160 {
					return fmt.Errorf("模板 %q 的文字组件 %d 参数无效", slot.Name, j+1)
				}
			case "image", "last_rx":
				if obj.Width <= 0 || obj.Width > 100 || obj.Height <= 0 || obj.Height > 100 {
					return fmt.Errorf("模板 %q 的图像组件 %d 尺寸无效", slot.Name, j+1)
				}
				if obj.Type == "last_rx" {
					// The previous-RX object intentionally stores no image bytes.
					// It resolves to the latest received frame at render time.
					obj.ImageData = ""
					if obj.ImageName == "" {
						obj.ImageName = "上一次 RX 图像"
					}
					continue
				}
				totalImageBytes += len(obj.ImageData)
				if len(obj.ImageData) > 16<<20 || totalImageBytes > 48<<20 {
					return fmt.Errorf("模板图片数据过大，请压缩图片后再保存")
				}
				if !strings.HasPrefix(obj.ImageData, "data:image/") || !strings.Contains(obj.ImageData, ";base64,") {
					return fmt.Errorf("模板 %q 的图像组件 %d 数据无效", slot.Name, j+1)
				}
				if _, err := decodeDataURL(obj.ImageData); err != nil {
					return fmt.Errorf("模板 %q 的图像组件 %d 无法解码：%w", slot.Name, j+1, err)
				}
			default:
				return fmt.Errorf("模板 %q 包含未知组件类型 %q", slot.Name, obj.Type)
			}
		}
	}
	return nil
}
