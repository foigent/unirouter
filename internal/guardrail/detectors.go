package guardrail

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/filter"
)

type sensitiveWordDetector struct {
	filter       *filter.DFAFilter
	version      string
	maxWordBytes int
}

func (d *sensitiveWordDetector) StreamCapability() StreamCapability {
	lookbehind := d.maxWordBytes
	if lookbehind > 0 {
		lookbehind--
	}
	return StreamCapability{Synchronous: lookbehind <= MaxStreamLookbehindBytes, LookbehindBytes: lookbehind}
}

func (d *sensitiveWordDetector) Version() string {
	if d == nil || d.version == "" {
		return "sensitive-words.unversioned"
	}
	return d.version
}

func (d *sensitiveWordDetector) Detect(_ Context, text Text, _ json.RawMessage) ([]Finding, error) {
	if d == nil || d.filter == nil {
		return nil, ErrDetectorMissing
	}
	matched, _ := d.filter.IsSensitive(text.Content)
	if !matched {
		return nil, nil
	}
	return []Finding{{Category: "sensitive_word", Start: -1, End: -1, ConfidenceBucket: "deterministic"}}, nil
}

type piiSecretsDetector struct{}

func (piiSecretsDetector) Version() string { return "unirouter.local-pii-secrets.v1" }

func (piiSecretsDetector) StreamCapability() StreamCapability {
	// Local patterns become detectable well inside this retained suffix. Longer
	// ambiguous email-like text is explicitly bounded rather than advertised as
	// zero-disclosure detection.
	return StreamCapability{Synchronous: true, LookbehindBytes: 512}
}

var localPatterns = map[string]*regexp.Regexp{
	"email":       regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`),
	"phone":       regexp.MustCompile(`(?:\+?86[- ]?)?(?:1[3-9]\d{9})|(?:\+?[1-9]\d{0,2}[- .]?)?(?:\(?\d{2,4}\)?[- .]?)\d{3,4}[- .]?\d{4}`),
	"credit_card": regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`),
	"secret":      regexp.MustCompile(`(?i)\b(?:sk|api|key|token|secret)[-_]?[a-z0-9]{12,}\b`),
}

var (
	detectorIdentifierPattern = regexp.MustCompile(`^external:[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	detectorVersionPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	findingCategoryPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	confidenceBucketPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,23}$`)
)

func validDetectorIdentifier(value string) bool {
	return detectorIdentifierPattern.MatchString(strings.TrimSpace(value))
}

func validDetectorVersion(value string) bool {
	return detectorVersionPattern.MatchString(strings.TrimSpace(value))
}

type piiConfig struct {
	Entities []string `json:"entities"`
}

func (piiSecretsDetector) Detect(_ Context, text Text, raw json.RawMessage) ([]Finding, error) {
	config := piiConfig{Entities: []string{"email", "phone", "credit_card", "secret"}}
	if len(raw) > 0 {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return nil, fmt.Errorf("invalid PII detector config object")
		}
		for key := range object {
			if key != "entities" {
				return nil, fmt.Errorf("unsupported PII detector config field %q", key)
			}
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("invalid PII detector config: %w", err)
		}
	}
	if len(config.Entities) == 0 || len(config.Entities) > len(localPatterns) {
		return nil, fmt.Errorf("invalid PII entity selection")
	}
	seen := make(map[string]bool)
	findings := make([]Finding, 0)
	for _, entity := range config.Entities {
		entity = strings.TrimSpace(entity)
		pattern, ok := localPatterns[entity]
		if !ok || seen[entity] {
			return nil, fmt.Errorf("unsupported or duplicate PII entity %q", entity)
		}
		seen[entity] = true
		remaining := 256 - len(findings)
		if remaining <= 0 {
			break
		}
		for _, span := range pattern.FindAllStringIndex(text.Content, remaining) {
			if entity == "credit_card" && !validLuhn(text.Content[span[0]:span[1]]) {
				continue
			}
			findings = append(findings, Finding{Category: entity, Start: span[0], End: span[1], ConfidenceBucket: "deterministic"})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Start == findings[j].Start {
			if findings[i].End == findings[j].End {
				return findings[i].Category < findings[j].Category
			}
			return findings[i].End > findings[j].End
		}
		return findings[i].Start < findings[j].Start
	})
	return dedupeOverlappingFindings(findings), nil
}

func validLuhn(value string) bool {
	digits := make([]int, 0, len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	parity := len(digits) % 2
	for i, digit := range digits {
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}

func dedupeOverlappingFindings(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Start < 0 || finding.End <= finding.Start {
			out = append(out, finding)
			continue
		}
		if len(out) > 0 && out[len(out)-1].Start >= 0 && finding.Start < out[len(out)-1].End {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func normalizeFindings(content string, findings []Finding, detectorErr error) ([]Finding, error) {
	if detectorErr != nil {
		return nil, detectorErr
	}
	if len(findings) > 256 {
		return nil, fmt.Errorf("detector returned too many findings")
	}
	findings = append([]Finding(nil), findings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Start == findings[j].Start {
			if findings[i].End == findings[j].End {
				if findings[i].Category == findings[j].Category {
					return findings[i].ConfidenceBucket < findings[j].ConfidenceBucket
				}
				return findings[i].Category < findings[j].Category
			}
			return findings[i].End > findings[j].End
		}
		return findings[i].Start < findings[j].Start
	})
	for _, finding := range findings {
		locationless := finding.Start == -1 && finding.End == -1
		if !findingCategoryPattern.MatchString(finding.Category) ||
			!confidenceBucketPattern.MatchString(finding.ConfidenceBucket) ||
			(!locationless && (finding.Start < 0 || finding.End <= finding.Start || finding.End > len(content) ||
				(finding.Start < len(content) && !utf8.RuneStart(content[finding.Start])) ||
				(finding.End < len(content) && !utf8.RuneStart(content[finding.End])))) {
			return nil, fmt.Errorf("detector returned an invalid bounded finding")
		}
	}
	return dedupeOverlappingFindings(findings), nil
}
