package receive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nshsend/core/internal/shared"
)

// ErrChecksumMismatch indicates the uploaded file's SHA-256 did not match the offer.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// SaveUploadStream streams an incoming file upload to a temp file, verifies SHA-256 if requested,
// resolves filename collisions, renames atomically, and applies file timestamps.
func SaveUploadStream(
	ctx context.Context,
	body io.Reader,
	saveDir string,
	offer shared.FileOfferV2,
	verifyChecksum bool,
	onProgress func(bytesTransferred uint64),
) (string, uint64, error) {
	relPath := shared.SanitizeRelativePath(offer.FileName, runtime.GOOS)
	destCandidate := filepath.Join(saveDir, relPath)
	cleanRoot := filepath.Clean(saveDir)
	cleanDest := filepath.Clean(destCandidate)
	if cleanDest != cleanRoot && !strings.HasPrefix(cleanDest, cleanRoot+string(filepath.Separator)) {
		return "", 0, fmt.Errorf("resolved path escapes save directory")
	}

	destDir := filepath.Dir(cleanDest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create destination dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(destDir, ".nshsend-*.part")
	if err != nil {
		return "", 0, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	buf := make([]byte, 256*1024)
	var written uint64
	lastTick := time.Now()

	for {
		if err := ctx.Err(); err != nil {
			return "", written, err
		}
		n, readErr := body.Read(buf)
		if n > 0 {
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				return "", written, fmt.Errorf("write file chunk: %w", writeErr)
			}
			_, _ = hasher.Write(buf[:n])
			written += uint64(n)
			if onProgress != nil && time.Since(lastTick) >= 100*time.Millisecond {
				onProgress(written)
				lastTick = time.Now()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", written, fmt.Errorf("read upload stream: %w", readErr)
		}
	}

	if onProgress != nil {
		onProgress(written)
	}

	if verifyChecksum && offer.SHA256 != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actual, offer.SHA256) {
			return "", written, ErrChecksumMismatch
		}
	}

	if err := tmpFile.Close(); err != nil {
		return "", written, fmt.Errorf("close temp file: %w", err)
	}

	finalPath, err := claimUniqueDestPath(cleanDest)
	if err != nil {
		return "", written, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", written, fmt.Errorf("move file to destination: %w", err)
	}
	cleanupTmp = false

	applyFileTimestamps(finalPath, offer.Metadata)
	return finalPath, written, nil
}

func claimUniqueDestPath(candidate string) (string, error) {
	if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
		return candidate, nil
	}
	dir := filepath.Dir(candidate)
	base := filepath.Base(candidate)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 1; i < 10000; i++ {
		next := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(next); errors.Is(err, os.ErrNotExist) {
			return next, nil
		}
	}
	return "", fmt.Errorf("too many filename collisions for %s", base)
}

func applyFileTimestamps(path string, meta *shared.FileMetadata) {
	if meta == nil {
		return
	}
	var mtime, atime time.Time
	if meta.Modified != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, meta.Modified); err == nil {
			mtime = parsed
		}
	}
	if meta.Accessed != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, meta.Accessed); err == nil {
			atime = parsed
		}
	}
	if mtime.IsZero() && atime.IsZero() {
		return
	}
	if mtime.IsZero() {
		mtime = atime
	}
	if atime.IsZero() {
		atime = mtime
	}
	_ = os.Chtimes(path, atime, mtime)
}
