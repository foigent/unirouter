package admin

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/big"
	"strings"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

// The code is encoded only in the PNG pixels. It is held in memory until
// checking the OCR result and serves no purpose in the retained receipt.
var ocrProbeDigits = [10][7]uint8{
	{14, 17, 19, 21, 25, 17, 14}, {4, 12, 4, 4, 4, 4, 14},
	{14, 17, 1, 2, 4, 8, 31}, {30, 1, 1, 14, 1, 1, 30},
	{2, 6, 10, 18, 31, 2, 2}, {31, 16, 16, 30, 1, 1, 30},
	{14, 16, 16, 30, 17, 17, 14}, {31, 1, 2, 4, 8, 8, 8},
	{14, 17, 17, 14, 17, 17, 14}, {14, 17, 17, 15, 1, 1, 14},
}

const ocrProbeGlyphScale = 10
const ocrProbeInset = 18

func makeOCRProbe(model string) (body []byte, answer string, err error) {
	const digits = 8
	code := make([]byte, digits)
	canvas := image.NewGray(image.Rect(0, 0, 2*ocrProbeInset+6*ocrProbeGlyphScale*digits, 2*ocrProbeInset+7*ocrProbeGlyphScale))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for index := range code {
		digit, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return nil, "", err
		}
		value := digit.Int64()
		code[index] = '0' + byte(value)
		for y, row := range ocrProbeDigits[value] {
			for x := range 5 {
				if row&(1<<(4-x)) == 0 {
					continue
				}
				x0, y0 := ocrProbeInset+ocrProbeGlyphScale*(index*6+x), ocrProbeInset+ocrProbeGlyphScale*y
				draw.Draw(canvas, image.Rect(x0, y0, x0+ocrProbeGlyphScale, y0+ocrProbeGlyphScale), image.NewUniform(color.Black), image.Point{}, draw.Src)
			}
		}
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, canvas); err != nil {
		return nil, "", err
	}
	body, err = endpointprotocol.DeepSeekOCRProbeBody(model, pngBytes.Bytes())
	return body, string(code), err
}

func verifiesOCRProbe(result *endpointprotocol.MediaArtifact, code string) bool {
	if result == nil || result.MediaType != "text/plain; charset=utf-8" || code == "" {
		return false
	}
	page := strings.TrimSpace(string(result.Bytes))
	// Only recognize a leading layout annotation. Coordinate digits and
	// arbitrary content inside or after the recognized text are not answers.
	if ref, ok := strings.CutPrefix(page, "<｜ref｜>"); ok {
		label, rest, found := strings.Cut(ref, "<｜/ref｜>")
		if !found || !validOCRProbeLayoutLabel(label) {
			return false
		}
		page = strings.TrimSpace(rest)
		if !strings.HasPrefix(page, "<｜box｜>") {
			return false
		}
	}
	if box, ok := strings.CutPrefix(page, "<｜box｜>"); ok {
		raw, rest, found := strings.Cut(box, "<｜/box｜>")
		if !found || !validOCRProbeBoxes(raw) {
			return false
		}
		page = strings.TrimSpace(rest)
	}
	return page == code
}

func validOCRProbeLayoutLabel(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "标题", "文本", "正文", "title", "text":
		return true
	default:
		return false
	}
}

func validOCRProbeBoxes(raw string) bool {
	var boxes [][]int
	if len(raw) > 512 || json.Unmarshal([]byte(raw), &boxes) != nil || len(boxes) == 0 || len(boxes) > 16 {
		return false
	}
	for _, box := range boxes {
		if len(box) != 4 {
			return false
		}
		for _, coordinate := range box {
			if coordinate < 0 || coordinate > 1000 {
				return false
			}
		}
	}
	return true
}
