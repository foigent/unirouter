//go:build !darwin

package licensestudio

import (
	"context"
	"fmt"
	"runtime"
)

type nativeFileDialogs struct{}

func (nativeFileDialogs) ChooseDirectory(context.Context, string) (string, error) {
	return "", fmt.Errorf("native License Studio dialogs are not available on %s", runtime.GOOS)
}

func (nativeFileDialogs) ChooseOpenFile(context.Context, string) (string, error) {
	return "", fmt.Errorf("native License Studio dialogs are not available on %s", runtime.GOOS)
}

func (nativeFileDialogs) ChooseSaveFile(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("native License Studio dialogs are not available on %s", runtime.GOOS)
}
