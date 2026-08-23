package tokenprofile

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

const (
	KimiK26SourceCommit = "7eb5002f6aadc958aed6a9177b7ed26bb94011bb"
	KimiK26SHA256       = "b6c497a7469b33ced9c38afb1ad6e47f03f5e5dc05f15930799210ec050c5103"
	// The tokenizer config supplies the special-token names/IDs frozen below.
	KimiK26TokenizerConfigSHA256 = "12fcab43d2b6068f46769f5ff373960bf7c17a94d7abbc50e2491306b2f6cf58"
	// The tokenizer implementation supplies the regex semantics translated below.
	KimiK26ImplementationSHA256     = "2ab1ffb6f5c4380758bd8d9752ff1041c09024182676a4311528fbdf92fb9599"
	kimiK26ArtifactSize             = 2_795_286
	kimiK26BaseTokens               = 163_584
	kimiK26SpecialCount             = 256
	kimiK26MaxEncodeRunes           = 400_000
	kimiK26MaxConsecutiveClassRunes = 25_000
	// The official Python artifact uses regex character-class intersection
	// (A&&[^Han]). regexp2 follows .NET syntax, so the same sets are expressed
	// as character-class subtraction (A-[Han]). Frozen vectors below guard the
	// semantic translation.
	kimiK26Pattern = `[\p{Han}]+|[^\r\n\p{L}\p{N}]?[\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}-[\p{Han}]]*[\p{Ll}\p{Lm}\p{Lo}\p{M}-[\p{Han}]]+(?i:'s|'t|'re|'ve|'m|'ll|'d)?|[^\r\n\p{L}\p{N}]?[\p{Lu}\p{Lt}\p{Lm}\p{Lo}\p{M}-[\p{Han}]]+[\p{Ll}\p{Lm}\p{Lo}\p{M}-[\p{Han}]]*(?i:'s|'t|'re|'ve|'m|'ll|'d)?|\p{N}{1,3}| ?[^\s\p{L}\p{N}]+[\r\n]*|\s*[\r\n]+|\s+(?!\S)|\s+`
)

//go:embed assets/kimi-k2.6-tiktoken.model.gz
var kimiK26TokenizerGZIP []byte

var kimiK26State struct {
	once    sync.Once
	encoder *tiktoken.Tiktoken
	err     error
}

func encodeKimiK26(value []byte) (ids []int, err error) {
	if !utf8.Valid(value) {
		return nil, fmt.Errorf("tokenizer input must be valid UTF-8")
	}
	kimiK26State.once.Do(func() {
		kimiK26State.encoder, kimiK26State.err = loadKimiK26()
	})
	if kimiK26State.err != nil {
		return nil, kimiK26State.err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			ids = nil
			err = fmt.Errorf("tokenizer execution failed: %v", recovered)
		}
	}()
	for _, chunk := range splitKimiK26Input(string(value)) {
		ids = append(ids, kimiK26State.encoder.Encode(chunk, []string{"all"}, nil)...)
	}
	return ids, nil
}

// splitKimiK26Input mirrors the safety splitting in the frozen official
// tokenizer implementation. Splitting can only remove cross-boundary merges,
// so it also preserves admission's upper-bound requirement.
func splitKimiK26Input(value string) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return []string{""}
	}
	chunks := make([]string, 0, 1+len(runes)/kimiK26MaxConsecutiveClassRunes)
	for outerStart := 0; outerStart < len(runes); outerStart += kimiK26MaxEncodeRunes {
		outerEnd := min(outerStart+kimiK26MaxEncodeRunes, len(runes))
		part := runes[outerStart:outerEnd]
		sliceStart := 0
		currentLength := 0
		currentIsSpace := unicode.IsSpace(part[0])
		for index, character := range part {
			isSpace := unicode.IsSpace(character)
			if currentIsSpace != isSpace {
				currentLength = 1
				currentIsSpace = isSpace
				continue
			}
			currentLength++
			if currentLength > kimiK26MaxConsecutiveClassRunes {
				chunks = append(chunks, string(part[sliceStart:index]))
				sliceStart = index
				currentLength = 1
			}
		}
		chunks = append(chunks, string(part[sliceStart:]))
	}
	return chunks
}

func loadKimiK26() (*tiktoken.Tiktoken, error) {
	reader, err := gzip.NewReader(bytes.NewReader(kimiK26TokenizerGZIP))
	if err != nil {
		return nil, fmt.Errorf("open Kimi K2.6 tokenizer artifact: %w", err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read Kimi K2.6 tokenizer artifact: %w", err)
	}
	if len(raw) != kimiK26ArtifactSize {
		return nil, fmt.Errorf("Kimi K2.6 tokenizer artifact size drift: %d", len(raw))
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != KimiK26SHA256 {
		return nil, fmt.Errorf("Kimi K2.6 tokenizer artifact digest drift")
	}

	ranks := make(map[string]int, kimiK26BaseTokens)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		encoded, rawRank, found := strings.Cut(scanner.Text(), " ")
		if !found || encoded == "" || rawRank == "" || strings.Contains(rawRank, " ") {
			return nil, fmt.Errorf("Kimi K2.6 tokenizer artifact line is malformed")
		}
		token, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			return nil, fmt.Errorf("decode Kimi K2.6 tokenizer token: %w", decodeErr)
		}
		rank, parseErr := strconv.Atoi(rawRank)
		if parseErr != nil || rank < 0 || rank >= kimiK26BaseTokens {
			return nil, fmt.Errorf("Kimi K2.6 tokenizer rank is invalid")
		}
		if _, duplicate := ranks[string(token)]; duplicate {
			return nil, fmt.Errorf("Kimi K2.6 tokenizer token is duplicated")
		}
		ranks[string(token)] = rank
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan Kimi K2.6 tokenizer artifact: %w", err)
	}
	if len(ranks) != kimiK26BaseTokens {
		return nil, fmt.Errorf("Kimi K2.6 tokenizer rank count drift: %d", len(ranks))
	}

	special := kimiK26SpecialTokens()
	specialSet := make(map[string]any, len(special))
	for token := range special {
		specialSet[token] = true
	}
	core, err := tiktoken.NewCoreBPE(ranks, special, kimiK26Pattern)
	if err != nil {
		return nil, fmt.Errorf("compile Kimi K2.6 tokenizer: %w", err)
	}
	return tiktoken.NewTiktoken(core, nil, specialSet), nil
}

func kimiK26SpecialTokens() map[string]int {
	overrides := map[int]string{
		163_584: "[BOS]",
		163_585: "[EOS]",
		163_586: "<|im_end|>",
		163_587: "<|im_user|>",
		163_588: "<|im_assistant|>",
		163_590: "<|start_header_id|>",
		163_591: "<|end_header_id|>",
		163_593: "[EOT]",
		163_594: "<|im_system|>",
		163_595: "<|tool_calls_section_begin|>",
		163_596: "<|tool_calls_section_end|>",
		163_597: "<|tool_call_begin|>",
		163_598: "<|tool_call_argument_begin|>",
		163_599: "<|tool_call_end|>",
		163_601: "<|im_middle|>",
		163_602: "<|media_begin|>",
		163_603: "<|media_content|>",
		163_604: "<|media_end|>",
		163_605: "<|media_pad|>",
		163_606: "<think>",
		163_607: "</think>",
		163_838: "[UNK]",
		163_839: "[PAD]",
	}
	tokens := make(map[string]int, kimiK26SpecialCount)
	for id := kimiK26BaseTokens; id < kimiK26BaseTokens+kimiK26SpecialCount; id++ {
		content := overrides[id]
		if content == "" {
			content = fmt.Sprintf("<|reserved_token_%d|>", id)
		}
		tokens[content] = id
	}
	return tokens
}
