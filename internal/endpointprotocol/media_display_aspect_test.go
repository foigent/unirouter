package endpointprotocol

import (
	"os"
	"testing"
)

func TestMediaVideoDisplayRatio(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		spec  MP4Requirements
		match bool
	}{
		{"Wan requested landscape, ordinary landscape output", "testdata/dashscope-wan-synthetic-480p.mp4", dashScopeWanVideoProfile{}.VideoRequirements(), true},
		{"H3 requested landscape, ordinary landscape output", "testdata/minimax-h3-synthetic-768p.mp4", miniMaxH3VideoProfile{}.VideoRequirements(), true},
		{"Wan requested landscape, ordinary square output", "testdata/video-square-two.mp4", dashScopeWanVideoProfile{}.VideoRequirements(), false},
		{"H3 requested landscape, ordinary square output", "testdata/video-square-four.mp4", miniMaxH3VideoProfile{}.VideoRequirements(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			match := ValidateMP4(data, tc.spec)
			if match != tc.match {
				t.Errorf("qualifies=%t, wanted %t", match, tc.match)
			}
		})
	}
}
