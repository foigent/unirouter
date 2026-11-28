package admin

import (
	"testing"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

// Each case uses the same eight-digit page content and the fullwidth layout
// marks documented by the supplier. Coordinate digits are not page text.
func TestProjectOCRProbeWithLayout(t *testing.T) {
	const answer = "58263742"
	const box = "<｜box｜>[[58, 26, 37, 42]]<｜/box｜>"
	cases := []struct {
		name string
		page string
		want bool
	}{
		{"plain exact answer", answer, true},
		{"documented layout and exact visible answer", "<｜ref｜>标题<｜/ref｜>" + box + "\n" + answer, true},
		{"wrong recognized text with matching coordinate digits", "<｜ref｜>标题<｜/ref｜>" + box + "\n00000000", false},
		{"coordinates only", box, false},
		{"different plain answer", "58263741", false},
		{"extra visible content", "<｜ref｜>标题<｜/ref｜>" + box + "\nother text " + answer, false},
		{"unterminated box", "<｜box｜>[[58,26,37,42]]" + answer, false},
		{"unknown reference content", "<｜ref｜>00000000<｜/ref｜>" + box + answer, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &endpointprotocol.MediaArtifact{Bytes: []byte(tc.page), MediaType: "text/plain; charset=utf-8"}
			got := verifiesOCRProbe(out, answer)
			if got != tc.want {
				t.Errorf("verified=%t, wanted %t", got, tc.want)
			}
		})
	}
}
