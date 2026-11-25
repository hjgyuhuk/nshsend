package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"nshsend/core/internal/shared"
)

// QuickSaveMode controls incoming transfer auto-acceptance.
type QuickSaveMode string

const (
	QuickSaveOff QuickSaveMode = "off"
	QuickSaveOn  QuickSaveMode = "on"
)

// AppSettings holds persisted user configuration for the core daemon.
type AppSettings struct {
	Alias           string        `json:"alias"`
	Port            uint16        `json:"port"`
	HTTPS           bool          `json:"https"`
	SaveDir         string        `json:"saveDir"`
	QuickSave       QuickSaveMode `json:"quickSave"`
	PIN             string        `json:"pin"`
	MulticastGroup  string        `json:"multicastGroup"`
	MulticastPort   uint16        `json:"multicastPort"`
	VerifyChecksums bool          `json:"verifyChecksums"`
	DeviceModel     string        `json:"deviceModel"`
}

// SettingsPatch carries optional partial updates to AppSettings.
type SettingsPatch struct {
	Alias           *string        `json:"alias,omitempty"`
	Port            *uint16        `json:"port,omitempty"`
	HTTPS           *bool          `json:"https,omitempty"`
	SaveDir         *string        `json:"saveDir,omitempty"`
	QuickSave       *QuickSaveMode `json:"quickSave,omitempty"`
	PIN             *string        `json:"pin,omitempty"`
	MulticastGroup  *string        `json:"multicastGroup,omitempty"`
	MulticastPort   *uint16        `json:"multicastPort,omitempty"`
	VerifyChecksums *bool          `json:"verifyChecksums,omitempty"`
}

// SettingsStore is the single writer for persisted settings in <dataDir>/settings.json.
type SettingsStore struct {
	mu       sync.RWMutex
	filePath string
	current  AppSettings
}

func defaultAppSettings(dataDir string) AppSettings {
	alias := "NshSend Desktop"
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		alias = strings.TrimSpace(host)
	}

	saveDir := filepath.Join(dataDir, "downloads")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		saveDir = filepath.Join(home, "Downloads")
	}

	return AppSettings{
		Alias:           alias,
		Port:            shared.DefaultPort,
		HTTPS:           true,
		SaveDir:         saveDir,
		QuickSave:       QuickSaveOff,
		PIN:             "",
		MulticastGroup:  shared.DefaultMulticastGroupV4,
		MulticastPort:   shared.DefaultPort,
		VerifyChecksums: true,
		DeviceModel:     detectDeviceModel(),
	}
}

func detectDeviceModel() string {
	switch runtime.GOOS {
	case "linux":
		return "Linux"
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	default:
		return runtime.GOOS
	}
}

// LoadSettingsStore loads settings from <dataDir>/settings.json or writes defaults.
func LoadSettingsStore(dataDir string) (*SettingsStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "settings.json")
	cfg := defaultAppSettings(dataDir)

	raw, err := os.ReadFile(path)
	if err == nil {
		if jsonErr := json.Unmarshal(raw, &cfg); jsonErr != nil {
			return nil, fmt.Errorf("parse settings.json: %w", jsonErr)
		}
		if cfg.Port == 0 {
			cfg.Port = shared.DefaultPort
		}
		if cfg.MulticastPort == 0 {
			cfg.MulticastPort = shared.DefaultPort
		}
		if strings.TrimSpace(cfg.MulticastGroup) == "" {
			cfg.MulticastGroup = shared.DefaultMulticastGroupV4
		}
		if strings.TrimSpace(cfg.Alias) == "" {
			cfg.Alias = "NshSend Desktop"
		}
		if strings.TrimSpace(cfg.SaveDir) == "" {
			cfg.SaveDir = defaultAppSettings(dataDir).SaveDir
		}
		if cfg.QuickSave != QuickSaveOn {
			cfg.QuickSave = QuickSaveOff
		}
		if cfg.DeviceModel == "" {
			cfg.DeviceModel = detectDeviceModel()
		}
	} else if errors.Is(err, os.ErrNotExist) {
		payload, marshalErr := json.MarshalIndent(cfg, "", "  ")
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal default settings: %w", marshalErr)
		}
		if writeErr := writeAtomicFile(path, payload, 0o600); writeErr != nil {
			return nil, fmt.Errorf("write default settings: %w", writeErr)
		}
	} else {
		return nil, fmt.Errorf("read settings.json: %w", err)
	}

	return &SettingsStore{
		filePath: path,
		current:  cfg,
	}, nil
}

// CurrentSettings returns a snapshot of the current AppSettings.
func (s *SettingsStore) CurrentSettings() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// UpdateSettings validates and persists partial settings changes, reporting whether
// the HTTP/mTLS server or multicast listeners must restart.
func (s *SettingsStore) UpdateSettings(patch SettingsPatch) (AppSettings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.current
	if patch.Alias != nil {
		trimmed := strings.TrimSpace(*patch.Alias)
		if trimmed == "" {
			return s.current, false, errors.New("alias cannot be empty")
		}
		next.Alias = trimmed
	}
	if patch.Port != nil {
		if *patch.Port == 0 {
			return s.current, false, errors.New("port must be between 1 and 65535")
		}
		next.Port = *patch.Port
	}
	if patch.HTTPS != nil {
		next.HTTPS = *patch.HTTPS
	}
	if patch.SaveDir != nil {
		trimmed := strings.TrimSpace(*patch.SaveDir)
		if trimmed == "" {
			return s.current, false, errors.New("saveDir cannot be empty")
		}
		next.SaveDir = trimmed
	}
	if patch.QuickSave != nil {
		if *patch.QuickSave != QuickSaveOff && *patch.QuickSave != QuickSaveOn {
			return s.current, false, fmt.Errorf("invalid quickSave %q", *patch.QuickSave)
		}
		next.QuickSave = *patch.QuickSave
	}
	if patch.PIN != nil {
		next.PIN = strings.TrimSpace(*patch.PIN)
	}
	if patch.MulticastGroup != nil {
		trimmed := strings.TrimSpace(*patch.MulticastGroup)
		if trimmed == "" {
			return s.current, false, errors.New("multicastGroup cannot be empty")
		}
		next.MulticastGroup = trimmed
	}
	if patch.MulticastPort != nil {
		if *patch.MulticastPort == 0 {
			return s.current, false, errors.New("multicastPort must be between 1 and 65535")
		}
		next.MulticastPort = *patch.MulticastPort
	}
	if patch.VerifyChecksums != nil {
		next.VerifyChecksums = *patch.VerifyChecksums
	}

	needsRestart := next.Alias != s.current.Alias ||
		next.Port != s.current.Port ||
		next.HTTPS != s.current.HTTPS ||
		next.PIN != s.current.PIN ||
		next.MulticastGroup != s.current.MulticastGroup ||
		next.MulticastPort != s.current.MulticastPort ||
		next.VerifyChecksums != s.current.VerifyChecksums

	payload, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return s.current, false, fmt.Errorf("marshal settings: %w", err)
	}
	if err := writeAtomicFile(s.filePath, payload, 0o600); err != nil {
		return s.current, false, fmt.Errorf("persist settings: %w", err)
	}

	s.current = next
	return next, needsRestart, nil
}
