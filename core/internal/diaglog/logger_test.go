package diaglog

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestDiagnosticLogPersistenceAndRingBuffer(t *testing.T) {
	dir := t.TempDir()
	logger, err := OpenDiagnosticLog(dir)
	if err != nil {
		t.Fatalf("OpenDiagnosticLog: %v", err)
	}
	defer logger.Close()

	for i := 1; i <= 210; i++ {
		if _, err := fmt.Fprintf(logger, "line-%d\n", i); err != nil {
			t.Fatalf("write line %d: %v", i, err)
		}
	}

	recent := logger.RecentLines(5)
	if len(recent) != 5 {
		t.Fatalf("expected 5 recent lines, got %d", len(recent))
	}
	if recent[0] != "line-206" || recent[4] != "line-210" {
		t.Fatalf("unexpected ring buffer tail: %v", recent)
	}

	all := logger.RecentLines(0)
	if len(all) != maxRingLines {
		t.Fatalf("expected %d lines in ring buffer, got %d", maxRingLines, len(all))
	}

	content, err := os.ReadFile(logger.FilePath())
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(content), "line-210") {
		t.Fatalf("expected line-210 in %s", logger.FilePath())
	}
}
