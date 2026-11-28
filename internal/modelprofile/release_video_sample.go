package modelprofile

import (
	"encoding/hex"
	"strings"
)

const (
	releaseVideoSampleOrigin = "https://raw.githubusercontent.com/foigent/unirouter/"
	releaseVideoSamplePath   = "/internal/modelcapabilityqualification/testdata/inline-video-colors.mp4"
)

func ReleaseVideoSampleURL(commit string) (string, bool) {
	if len(commit) != 40 || strings.ToLower(commit) != commit {
		return "", false
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return "", false
	}
	return releaseVideoSampleOrigin + commit + releaseVideoSamplePath, true
}
