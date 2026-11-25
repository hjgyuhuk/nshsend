package receive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nshsend/core/internal/shared"
)

func TestPinAttemptTrackerLimits(t *testing.T) {
	tracker := NewPinAttemptTracker()
	if code, _ := tracker.ValidatePin("1234", "", false, "10.0.0.1"); code != 401 {
		t.Fatalf("expected 401 for missing pin, got %d", code)
	}
	for i := 0; i < 3; i++ {
		if code, _ := tracker.ValidatePin("1234", "0000", true, "10.0.0.1"); code != 401 {
			t.Fatalf("expected 401 for wrong pin attempt %d, got %d", i+1, code)
		}
	}
	if code, _ := tracker.ValidatePin("1234", "1234", true, "10.0.0.1"); code != 429 {
		t.Fatalf("expected 429 after 3 failures, got %d", code)
	}
}

func TestSaveUploadStreamCollisionAndChecksum(t *testing.T) {
	saveDir := t.TempDir()
	data := []byte("hello localsend")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])

	offer := shared.FileOfferV2{
		ID:       "f1",
		FileName: "../../evil/note.txt",
		Size:     uint64(len(data)),
		FileType: "text/plain",
		SHA256:   hexSum,
	}

	p1, n1, err := SaveUploadStream(context.Background(), bytes.NewReader(data), saveDir, offer, true, nil)
	if err != nil || n1 != uint64(len(data)) {
		t.Fatalf("first save failed: %v", err)
	}
	if filepath.Base(p1) != "note.txt" {
		t.Fatalf("unexpected saved filename: %s", p1)
	}

	p2, _, err := SaveUploadStream(context.Background(), bytes.NewReader(data), saveDir, offer, true, nil)
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if filepath.Base(p2) != "note (1).txt" {
		t.Fatalf("expected collision dedupe note (1).txt, got %s", filepath.Base(p2))
	}

	badOffer := offer
	badOffer.SHA256 = "deadbeef"
	_, _, err = SaveUploadStream(context.Background(), bytes.NewReader(data), saveDir, badOffer, true, nil)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestHistoryStorePersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadHistoryStore(dir)
	if err != nil {
		t.Fatalf("LoadHistoryStore: %v", err)
	}
	if err := store.AppendHistoryEntry(HistoryEntry{
		ID:          "s1:f1",
		FileName:    "photo.png",
		Size:        128,
		SenderAlias: "Phone",
		TimestampMs: 1000,
	}); err != nil {
		t.Fatalf("AppendHistoryEntry: %v", err)
	}

	reloaded, err := LoadHistoryStore(dir)
	if err != nil {
		t.Fatalf("reload HistoryStore: %v", err)
	}
	list := reloaded.ListHistoryEntries()
	if len(list) != 1 || list[0].FileName != "photo.png" {
		t.Fatalf("unexpected reloaded history: %+v", list)
	}
	if err := reloaded.RemoveHistoryEntry("s1:f1"); err != nil {
		t.Fatalf("RemoveHistoryEntry: %v", err)
	}
	if len(reloaded.ListHistoryEntries()) != 0 {
		t.Fatalf("expected empty history after remove")
	}
	if _, err := os.Stat(filepath.Join(dir, "history.json")); err != nil {
		t.Fatalf("expected history.json to exist: %v", err)
	}
}
