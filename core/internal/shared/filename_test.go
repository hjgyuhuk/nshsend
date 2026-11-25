package shared

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFileName(t *testing.T) {
	if got := SanitizeFileName("a<b>c:d\"e/f\\g|h?i*j", "windows"); got != "a_b_c_d_e_f_g_h_i_j" {
		t.Fatalf("windows illegal chars: got %q", got)
	}
	if got := SanitizeFileName("a/b:c", "darwin"); got != "a_b_c" {
		t.Fatalf("darwin illegal chars: got %q", got)
	}
	if got := SanitizeFileName("a/b:c", "linux"); got != "a_b:c" {
		t.Fatalf("linux illegal chars: got %q", got)
	}
	if got := SanitizeFileName("CON.txt", "windows"); got != "_.txt" {
		t.Fatalf("windows reserved name: got %q", got)
	}
	if got := SanitizeFileName("report...  ", "windows"); got != "report_" {
		t.Fatalf("windows trailing dots: got %q", got)
	}
	if got := SanitizeFileName("..", "linux"); got != "untitled" {
		t.Fatalf("dotdot fallback: got %q", got)
	}
	long := strings.Repeat("你", 100)
	gotLong := SanitizeFileName(long, "linux")
	if len(gotLong) > 255 {
		t.Fatalf("exceeded 255 bytes: %d", len(gotLong))
	}
}

func TestSanitizeRelativePath(t *testing.T) {
	got := SanitizeRelativePath("../../etc/passwd", "linux")
	want := filepath.Join("etc", "passwd")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if gotEmpty := SanitizeRelativePath("../..", "linux"); gotEmpty != "untitled" {
		t.Fatalf("got %q, want untitled", gotEmpty)
	}
}
