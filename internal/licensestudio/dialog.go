package licensestudio

import (
	"context"
	"errors"
)

var ErrDialogCancelled = errors.New("file selection cancelled")

type FileDialogs interface {
	ChooseDirectory(ctx context.Context, prompt string) (string, error)
	ChooseOpenFile(ctx context.Context, prompt string) (string, error)
	ChooseSaveFile(ctx context.Context, prompt, defaultName string) (string, error)
}

func NewNativeFileDialogs() FileDialogs {
	return nativeFileDialogs{}
}
