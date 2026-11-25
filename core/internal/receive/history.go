package receive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const maxHistoryEntries = 200

// HistoryEntry records one completed received file or text message.
type HistoryEntry struct {
	ID          string `json:"id"`
	FileName    string `json:"fileName"`
	FileType    string `json:"fileType"`
	Size        uint64 `json:"size"`
	SavedPath   string `json:"savedPath,omitempty"`
	TextPreview string `json:"textPreview,omitempty"`
	SenderAlias string `json:"senderAlias"`
	TimestampMs int64  `json:"timestampMs"`
	IsText      bool   `json:"isText"`
}

// HistoryStore is the single writer for persisted receive history in <dataDir>/history.json.
type HistoryStore struct {
	mu       sync.Mutex
	filePath string
	entries  []HistoryEntry
}

// LoadHistoryStore loads history from <dataDir>/history.json.
func LoadHistoryStore(dataDir string) (*HistoryStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "history.json")
	entries := []HistoryEntry{}

	raw, err := os.ReadFile(path)
	if err == nil {
		if jsonErr := json.Unmarshal(raw, &entries); jsonErr != nil {
			entries = []HistoryEntry{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read history.json: %w", err)
	}

	return &HistoryStore{
		filePath: path,
		entries:  entries,
	}, nil
}

// AppendHistoryEntry prepends an entry (newest first) and persists atomically.
func (h *HistoryStore) AppendHistoryEntry(entry HistoryEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = append([]HistoryEntry{entry}, h.entries...)
	if len(h.entries) > maxHistoryEntries {
		h.entries = h.entries[:maxHistoryEntries]
	}
	return h.persistLocked()
}

// ListHistoryEntries returns a copy of all stored history entries.
func (h *HistoryStore) ListHistoryEntries() []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]HistoryEntry, len(h.entries))
	copy(out, h.entries)
	return out
}

// RemoveHistoryEntry deletes one entry by ID.
func (h *HistoryStore) RemoveHistoryEntry(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	filtered := make([]HistoryEntry, 0, len(h.entries))
	for _, e := range h.entries {
		if e.ID != id {
			filtered = append(filtered, e)
		}
	}
	h.entries = filtered
	return h.persistLocked()
}

// ClearHistoryEntries removes all history entries.
func (h *HistoryStore) ClearHistoryEntries() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = []HistoryEntry{}
	return h.persistLocked()
}

func (h *HistoryStore) persistLocked() error {
	payload, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal history: %w", err)
	}
	dir := filepath.Dir(h.filePath)
	tmp, err := os.CreateTemp(dir, ".tmp-history-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, h.filePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}
