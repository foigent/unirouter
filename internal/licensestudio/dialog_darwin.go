//go:build darwin

package licensestudio

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type nativeFileDialogs struct{}

func (nativeFileDialogs) ChooseDirectory(ctx context.Context, prompt string) (string, error) {
	const script = `on run argv
set promptText to item 1 of argv
set selectedFolder to choose folder with prompt promptText
return POSIX path of selectedFolder
end run`
	return runAppleScript(ctx, script, prompt)
}

func (nativeFileDialogs) ChooseOpenFile(ctx context.Context, prompt string) (string, error) {
	const script = `on run argv
set promptText to item 1 of argv
set selectedFile to choose file with prompt promptText
return POSIX path of selectedFile
end run`
	return runAppleScript(ctx, script, prompt)
}

func (nativeFileDialogs) ChooseSaveFile(ctx context.Context, prompt, defaultName string) (string, error) {
	const script = `on run argv
set promptText to item 1 of argv
set suggestedName to item 2 of argv
set selectedFile to choose file name with prompt promptText default name suggestedName
return POSIX path of selectedFile
end run`
	return runAppleScript(ctx, script, prompt, defaultName)
}

func runAppleScript(ctx context.Context, script string, args ...string) (string, error) {
	commandArgs := []string{"-e", script, "--"}
	commandArgs = append(commandArgs, args...)
	output, err := exec.CommandContext(ctx, "osascript", commandArgs...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 1 || exitErr.ExitCode() == -128) &&
			(strings.Contains(strings.ToLower(text), "canceled") || strings.Contains(text, "-128")) {
			return "", ErrDialogCancelled
		}
		if text == "" {
			text = err.Error()
		}
		return "", fmt.Errorf("open macOS file dialog: %s", text)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", fmt.Errorf("macOS file dialog returned an empty path")
	}
	return path, nil
}
