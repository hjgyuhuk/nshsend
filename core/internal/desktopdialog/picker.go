package desktopdialog

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// PickFilesNative opens the OS file picker dialog and returns selected absolute file paths.
// Returns an empty slice without error when the user cancels.
func PickFilesNative(ctx context.Context, multiple bool) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		return pickFilesLinux(ctx, multiple)
	case "darwin":
		return pickFilesDarwin(ctx, multiple)
	case "windows":
		return pickFilesWindows(ctx, multiple)
	default:
		return nil, fmt.Errorf("native file picker unsupported on %s", runtime.GOOS)
	}
}

// PickFolderNative opens the OS folder picker dialog and returns the selected directory path.
// Returns "" without error when the user cancels.
func PickFolderNative(ctx context.Context) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return pickFolderLinux(ctx)
	case "darwin":
		return pickFolderDarwin(ctx)
	case "windows":
		return pickFolderWindows(ctx)
	default:
		return "", fmt.Errorf("native folder picker unsupported on %s", runtime.GOOS)
	}
}

// OpenLocalPath opens a file or directory using the OS default application.
func OpenLocalPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		return fmt.Errorf("open path unsupported on %s", runtime.GOOS)
	}
	return cmd.Start()
}

// RevealLocalPath reveals a file in the OS file manager.
func RevealLocalPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,", path)
	default:
		return fmt.Errorf("reveal path unsupported on %s", runtime.GOOS)
	}
	return cmd.Start()
}

func pickFilesLinux(ctx context.Context, multiple bool) ([]string, error) {
	if portalFileChooserAvailable(ctx) {
		return pickFilesPortal(ctx, multiple)
	}
	if _, err := exec.LookPath("zenity"); err == nil {
		args := []string{"--file-selection", "--title=选择文件", "--separator=\n"}
		if multiple {
			args = append(args, "--multiple")
		}
		out, runErr := exec.CommandContext(ctx, "zenity", args...).Output()
		return parseDialogLines(out, runErr)
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		args := []string{"--getopenfilename"}
		if multiple {
			args = append(args, "--multiple", "--separate-output")
		}
		out, runErr := exec.CommandContext(ctx, "kdialog", args...).Output()
		return parseDialogLines(out, runErr)
	}
	return nil, errors.New("no file picker available on Linux: need a D-Bus session with xdg-desktop-portal, or install zenity/kdialog")
}

func pickFolderLinux(ctx context.Context) (string, error) {
	if portalFileChooserAvailable(ctx) {
		return pickFolderPortal(ctx)
	}
	if _, err := exec.LookPath("zenity"); err == nil {
		out, runErr := exec.CommandContext(ctx, "zenity", "--file-selection", "--directory", "--title=选择文件夹").Output()
		lines, parseErr := parseDialogLines(out, runErr)
		if parseErr != nil || len(lines) == 0 {
			return "", parseErr
		}
		return lines[0], nil
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		out, runErr := exec.CommandContext(ctx, "kdialog", "--getexistingdirectory").Output()
		lines, parseErr := parseDialogLines(out, runErr)
		if parseErr != nil || len(lines) == 0 {
			return "", parseErr
		}
		return lines[0], nil
	}
	return "", errors.New("no folder picker available on Linux: need a D-Bus session with xdg-desktop-portal, or install zenity/kdialog")
}

func pickFilesDarwin(ctx context.Context, multiple bool) ([]string, error) {
	script := `set f to choose file`
	if multiple {
		script = `set fs to choose file with multiple selections allowed
set out to ""
repeat with f in fs
  set out to out & POSIX path of f & linefeed
end repeat
return out`
	} else {
		script = `return POSIX path of (choose file)`
	}
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).Output()
	return parseDialogLines(out, err)
}

func pickFolderDarwin(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", `return POSIX path of (choose folder)`).Output()
	lines, parseErr := parseDialogLines(out, err)
	if parseErr != nil || len(lines) == 0 {
		return "", parseErr
	}
	return lines[0], nil
}

func pickFilesWindows(ctx context.Context, multiple bool) ([]string, error) {
	multiFlag := "$false"
	if multiple {
		multiFlag = "$true"
	}
	ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Multiselect = %s
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $d.FileNames -join [Environment]::NewLine }`, multiFlag)
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps).Output()
	return parseDialogLines(out, err)
}

func pickFolderWindows(ctx context.Context) (string, error) {
	ps := `Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $d.SelectedPath }`
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", ps).Output()
	lines, parseErr := parseDialogLines(out, err)
	if parseErr != nil || len(lines) == 0 {
		return "", parseErr
	}
	return lines[0], nil
}

func parseDialogLines(out []byte, runErr error) ([]string, error) {
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
			// User cancelled the dialog.
			return nil, nil
		}
		return nil, runErr
	}
	raw := strings.ReplaceAll(string(out), "\r\n", "\n")
	var paths []string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths, nil
}
