package gateway

import "strings"

// sseFieldLine is the protocol-level interpretation of one non-comment SSE
// line. The optional space after the first colon is syntax, not part of the
// value; all downstream routing must use this parsed representation.
type sseFieldLine struct {
	name  string
	value string
}

func parseSSEFieldLine(line string) (sseFieldLine, bool) {
	if line == "" || strings.HasPrefix(line, ":") {
		return sseFieldLine{}, false
	}
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return sseFieldLine{name: line}, true
	}
	value := line[colon+1:]
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return sseFieldLine{name: line[:colon], value: value}, true
}
