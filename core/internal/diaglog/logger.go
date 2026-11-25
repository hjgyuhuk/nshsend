package diaglog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	maxLogBytes    int64 = 2 * 1024 * 1024
	maxRingLines   int   = 200
	defaultLogName       = "core.log"
)

// DiagnosticLog writes Go core logs to stderr, a rotating file (<dataDir>/core.log),
// and an in-memory ring buffer for UI inspection. It never writes to stdout.
type DiagnosticLog struct {
	mu       sync.Mutex
	filePath string
	file     *os.File
	written  int64
	lines    []string
}

// OpenDiagnosticLog opens or creates <dataDir>/core.log, rotating if it exceeds 2 MiB.
func OpenDiagnosticLog(dataDir string) (*DiagnosticLog, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	path := filepath.Join(dataDir, defaultLogName)
	if info, err := os.Stat(path); err == nil && info.Size() >= maxLogBytes {
		_ = os.Rename(path, path+".1")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	var currentSize int64
	if info, statErr := f.Stat(); statErr == nil {
		currentSize = info.Size()
	}

	return &DiagnosticLog{
		filePath: path,
		file:     f,
		written:  currentSize,
		lines:    make([]string, 0, maxRingLines),
	}, nil
}

// Write implements io.Writer, mirroring logs to stderr, file, and ring buffer.
func (d *DiagnosticLog) Write(p []byte) (int, error) {
	_, _ = os.Stderr.Write(p)

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.file != nil {
		if d.written+int64(len(p)) >= maxLogBytes {
			_ = d.file.Close()
			_ = os.Rename(d.filePath, d.filePath+".1")
			if nextFile, err := os.OpenFile(d.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
				d.file = nextFile
				d.written = 0
			} else {
				d.file = nil
			}
		}
		if d.file != nil {
			n, _ := d.file.Write(p)
			d.written += int64(n)
		}
	}

	raw := strings.ReplaceAll(string(p), "\r\n", "\n")
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(d.lines) >= maxRingLines {
			copy(d.lines, d.lines[1:])
			d.lines[len(d.lines)-1] = trimmed
		} else {
			d.lines = append(d.lines, trimmed)
		}
	}

	return len(p), nil
}

// FilePath returns the absolute path to core.log.
func (d *DiagnosticLog) FilePath() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.filePath
}

// RecentLines returns up to limit most recent log lines (oldest to newest).
func (d *DiagnosticLog) RecentLines(limit int) []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if limit <= 0 || limit > len(d.lines) {
		limit = len(d.lines)
	}
	start := len(d.lines) - limit
	out := make([]string, limit)
	copy(out, d.lines[start:])
	return out
}

// Close closes the underlying log file.
func (d *DiagnosticLog) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}
