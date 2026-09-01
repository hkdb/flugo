//go:build linux && !android

package filechooser

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// isUserCancel reports whether err is a non-zero exec.ExitError with code 1,
// which is the convention used by both zenity and kdialog to signal that the
// user closed/canceled the dialog. Any other error is a real failure.
func isUserCancel(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
}

// dialogTool identifies which dialog tool is available on the system.
type dialogTool int

const (
	toolNone dialogTool = iota
	toolZenity
	toolKdialog
	toolMatedialog
)

// findDialogTool detects which dialog tool is available.
func findDialogTool() dialogTool {
	if _, err := exec.LookPath("zenity"); err == nil {
		return toolZenity
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		return toolKdialog
	}
	if _, err := exec.LookPath("matedialog"); err == nil {
		return toolMatedialog
	}
	return toolNone
}

// fallbackPickFile uses zenity/kdialog to pick a single file.
func fallbackPickFile(title string) (string, error) {
	tool := findDialogTool()
	switch tool {
	case toolZenity, toolMatedialog:
		bin := "zenity"
		if tool == toolMatedialog {
			bin = "matedialog"
		}
		out, err := exec.Command(bin, "--file-selection", "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return "", nil // user canceled
			}
			return "", fmt.Errorf("%s file selection failed: %w", bin, err)
		}
		return strings.TrimSpace(string(out)), nil
	case toolKdialog:
		out, err := exec.Command("kdialog", "--getopenfilename", ".", "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return "", nil
			}
			return "", fmt.Errorf("kdialog file selection failed: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	default:
		return "", fmt.Errorf("no file chooser available: install zenity, kdialog, or ensure XDG Desktop Portal is running")
	}
}

// fallbackPickFiles uses zenity/kdialog to pick multiple files.
func fallbackPickFiles(title string) ([]string, error) {
	tool := findDialogTool()
	switch tool {
	case toolZenity, toolMatedialog:
		bin := "zenity"
		if tool == toolMatedialog {
			bin = "matedialog"
		}
		out, err := exec.Command(bin, "--file-selection", "--multiple", "--separator", "\n", "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("%s file selection failed: %w", bin, err)
		}
		result := strings.TrimSpace(string(out))
		if result == "" {
			return nil, nil
		}
		return strings.Split(result, "\n"), nil
	case toolKdialog:
		out, err := exec.Command("kdialog", "--getopenfilename", ".", "--multiple", "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("kdialog file selection failed: %w", err)
		}
		result := strings.TrimSpace(string(out))
		if result == "" {
			return nil, nil
		}
		return strings.Split(result, "\n"), nil
	default:
		return nil, fmt.Errorf("no file chooser available: install zenity, kdialog, or ensure XDG Desktop Portal is running")
	}
}

// fallbackPickDirectory uses zenity/kdialog to pick a directory.
func fallbackPickDirectory(title string) (string, error) {
	tool := findDialogTool()
	switch tool {
	case toolZenity, toolMatedialog:
		bin := "zenity"
		if tool == toolMatedialog {
			bin = "matedialog"
		}
		out, err := exec.Command(bin, "--file-selection", "--directory", "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return "", nil
			}
			return "", fmt.Errorf("%s directory selection failed: %w", bin, err)
		}
		return strings.TrimSpace(string(out)), nil
	case toolKdialog:
		out, err := exec.Command("kdialog", "--getexistingdirectory", ".", "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return "", nil
			}
			return "", fmt.Errorf("kdialog directory selection failed: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	default:
		return "", fmt.Errorf("no file chooser available: install zenity, kdialog, or ensure XDG Desktop Portal is running")
	}
}

// fallbackSaveFile uses zenity/kdialog to show a save file dialog.
func fallbackSaveFile(title, suggestedName, directory string) (string, error) {
	tool := findDialogTool()
	switch tool {
	case toolZenity, toolMatedialog:
		bin := "zenity"
		if tool == toolMatedialog {
			bin = "matedialog"
		}
		args := []string{"--file-selection", "--save", "--confirm-overwrite", "--title", title}
		if suggestedName != "" {
			filename := suggestedName
			if directory != "" {
				filename = directory + "/" + suggestedName
			}
			args = append(args, "--filename", filename)
		}
		out, err := exec.Command(bin, args...).Output()
		if err != nil {
			if isUserCancel(err) {
				return "", nil
			}
			return "", fmt.Errorf("%s save dialog failed: %w", bin, err)
		}
		return strings.TrimSpace(string(out)), nil
	case toolKdialog:
		startDir := "."
		if directory != "" {
			startDir = directory
		}
		if suggestedName != "" {
			startDir = startDir + "/" + suggestedName
		}
		out, err := exec.Command("kdialog", "--getsavefilename", startDir, "--title", title).Output()
		if err != nil {
			if isUserCancel(err) {
				return "", nil
			}
			return "", fmt.Errorf("kdialog save dialog failed: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	default:
		return "", fmt.Errorf("no file chooser available: install zenity, kdialog, or ensure XDG Desktop Portal is running")
	}
}

// fallbackOpen opens a file, directory, or URL with xdg-open.
func fallbackOpen(path string) error {
	if _, err := exec.LookPath("xdg-open"); err != nil {
		return fmt.Errorf("xdg-open not available: %w", err)
	}
	cmd := exec.Command("xdg-open", path)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the child so it doesn't linger as a zombie (xdg-open forks and returns
	// quickly); we don't block on the launched application.
	go func() { _ = cmd.Wait() }()
	return nil
}
