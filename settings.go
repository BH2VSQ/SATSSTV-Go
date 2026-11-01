package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// StationSettings contains operator information reused across SSTV sessions.
type StationSettings struct {
	MyCall  string `json:"myCall"`
	Locator string `json:"locator"`
}

const (
	defaultStationCallsign = "N0CALL"
	defaultStationLocator  = "JJ00"
)

func normalizeStationSettings(settings StationSettings) StationSettings {
	settings.MyCall = strings.ToUpper(strings.TrimSpace(settings.MyCall))
	settings.Locator = strings.ToUpper(strings.TrimSpace(settings.Locator))
	if settings.MyCall == "" {
		settings.MyCall = defaultStationCallsign
	}
	if settings.Locator == "" {
		settings.Locator = defaultStationLocator
	}
	return settings
}

// replaceFilePreservingPrevious installs a fully-written temporary file while
// retaining a recoverable copy of the previous settings/index on platforms
// where os.Rename cannot replace an existing destination (notably Windows).
func replaceFilePreservingPrevious(tmpPath, targetPath string) error {
	backupPath := targetPath + ".bak"
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		if _, backupErr := os.Stat(backupPath); backupErr == nil {
			if restoreErr := os.Rename(backupPath, targetPath); restoreErr != nil {
				return fmt.Errorf("restore previous file: %w", restoreErr)
			}
		}
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale backup: %w", err)
	}
	if err := os.Rename(targetPath, backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backup previous file: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		if _, statErr := os.Stat(backupPath); statErr == nil {
			if restoreErr := os.Rename(backupPath, targetPath); restoreErr != nil {
				return fmt.Errorf("install new file: %w; restore previous file: %v", err, restoreErr)
			}
		}
		return fmt.Errorf("install new file: %w", err)
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		// The new file is already installed. A stale backup is harmless and
		// can be removed on the next successful write.
		return nil
	}
	return nil
}

func (a *App) settingsDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("读取用户配置目录失败：%w", err)
	}
	dir := filepath.Join(base, "SATSSTV-Go")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建设置目录失败：%w", err)
	}
	return dir, nil
}

func (a *App) stationSettingsPath() (string, error) {
	dir, err := a.settingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// GetStationSettings loads the saved callsign and Maidenhead locator.
// A missing settings file is treated as a first-run configuration.
func (a *App) GetStationSettings() (StationSettings, error) {
	path, err := a.stationSettingsPath()
	if err != nil {
		return StationSettings{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return StationSettings{MyCall: defaultStationCallsign, Locator: defaultStationLocator}, nil
	}
	if err != nil {
		return StationSettings{}, fmt.Errorf("读取本站信息失败：%w", err)
	}
	var settings StationSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return StationSettings{}, fmt.Errorf("本站信息文件格式错误：%w", err)
	}
	settings = normalizeStationSettings(settings)
	return settings, nil
}

// SaveStationSettings persists the operator callsign and locator in the
// platform's user configuration directory, outside the application folder.
func (a *App) SaveStationSettings(settings StationSettings) (StationSettings, error) {
	settings = normalizeStationSettings(settings)
	if utf8.RuneCountInString(settings.MyCall) > 24 {
		return StationSettings{}, fmt.Errorf("本台呼号不能超过 24 个字符")
	}
	if utf8.RuneCountInString(settings.Locator) > 12 {
		return StationSettings{}, fmt.Errorf("网格定位不能超过 12 个字符")
	}

	path, err := a.stationSettingsPath()
	if err != nil {
		return StationSettings{}, err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return StationSettings{}, fmt.Errorf("序列化本站信息失败：%w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return StationSettings{}, fmt.Errorf("写入本站信息失败：%w", err)
	}
	// Windows rename does not replace an existing target. Keep the previous
	// file until the new contents have been fully written.
	if err := replaceFilePreservingPrevious(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return StationSettings{}, fmt.Errorf("保存本站信息失败：%w", err)
	}
	return settings, nil
}
