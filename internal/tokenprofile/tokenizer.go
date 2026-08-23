// Package tokenprofile implements the frozen tokenizer components referenced
// by model execution profiles. It never guesses a tokenizer from a model name.
package tokenprofile

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	tiktoken "github.com/pkoukk/tiktoken-go"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"golang.org/x/text/unicode/norm"
)

const (
	MiniMaxM27SourceCommit = "d494266a4affc0d2995ba1fa35c8481cbd84294b"
	MiniMaxM27SHA256       = "757622126525aeeb131756849d93298070ff3f0319c455ec8c5bb0f6b1cebbe8"
	miniMaxM27ArtifactSize = 9_730_160
)

//go:embed assets/minimax-m2.7-tokenizer.json.gz
var miniMaxM27TokenizerGZIP []byte

type tokenizerConfig struct {
	AddedTokens []struct {
		ID      int    `json:"id"`
		Content string `json:"content"`
	} `json:"added_tokens"`
	Normalizer struct {
		Type string `json:"type"`
	} `json:"normalizer"`
	PreTokenizer struct {
		PreTokenizers []struct {
			Pattern map[string]string `json:"pattern"`
		} `json:"pretokenizers"`
	} `json:"pre_tokenizer"`
	Model struct {
		Type  string         `json:"type"`
		Vocab map[string]int `json:"vocab"`
	} `json:"model"`
}

var miniMaxM27State struct {
	once    sync.Once
	encoder *tiktoken.Tiktoken
	err     error
}

func Count(profileID, profileVersion string, value []byte) (int, error) {
	var (
		ids []int
		err error
	)
	switch profileID + "\x00" + profileVersion {
	case modelprofile.TokenizerKimiK26ID + "\x00" + modelprofile.TokenizerKimiK26Version:
		ids, err = encodeKimiK26(value)
	case modelprofile.TokenizerMiniMaxM27ID + "\x00" + modelprofile.TokenizerMiniMaxM27Version:
		ids, err = encodeMiniMaxM27(value)
	default:
		return 0, fmt.Errorf("unsupported tokenizer profile %s@%s", profileID, profileVersion)
	}
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

func encodeMiniMaxM27(value []byte) (ids []int, err error) {
	if !utf8.Valid(value) {
		return nil, fmt.Errorf("tokenizer input must be valid UTF-8")
	}
	miniMaxM27State.once.Do(func() {
		miniMaxM27State.encoder, miniMaxM27State.err = loadMiniMaxM27()
	})
	if miniMaxM27State.err != nil {
		return nil, miniMaxM27State.err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			ids = nil
			err = fmt.Errorf("tokenizer execution failed: %v", recovered)
		}
	}()
	return miniMaxM27State.encoder.Encode(norm.NFC.String(string(value)), []string{"all"}, nil), nil
}

func loadMiniMaxM27() (*tiktoken.Tiktoken, error) {
	reader, err := gzip.NewReader(bytes.NewReader(miniMaxM27TokenizerGZIP))
	if err != nil {
		return nil, fmt.Errorf("open MiniMax tokenizer artifact: %w", err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read MiniMax tokenizer artifact: %w", err)
	}
	if len(raw) != miniMaxM27ArtifactSize {
		return nil, fmt.Errorf("MiniMax tokenizer artifact size drift: %d", len(raw))
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != MiniMaxM27SHA256 {
		return nil, fmt.Errorf("MiniMax tokenizer artifact digest drift")
	}

	var config tokenizerConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("decode MiniMax tokenizer artifact: %w", err)
	}
	if config.Model.Type != "BPE" || config.Normalizer.Type != "NFC" || len(config.Model.Vocab) != 200_000 || len(config.AddedTokens) != 54 || len(config.PreTokenizer.PreTokenizers) == 0 {
		return nil, fmt.Errorf("MiniMax tokenizer contract drift")
	}
	pattern := config.PreTokenizer.PreTokenizers[0].Pattern["Regex"]
	if pattern == "" {
		return nil, fmt.Errorf("MiniMax tokenizer regex missing")
	}

	inverse := byteUnicodeInverse()
	ranks := make(map[string]int, len(config.Model.Vocab))
	for token, id := range config.Model.Vocab {
		decoded := make([]byte, 0, len(token))
		for _, character := range token {
			value, ok := inverse[character]
			if !ok {
				return nil, fmt.Errorf("MiniMax tokenizer byte-unicode rune %U is unmapped", character)
			}
			decoded = append(decoded, value)
		}
		ranks[string(decoded)] = id
	}
	if len(ranks) != len(config.Model.Vocab) {
		return nil, fmt.Errorf("MiniMax tokenizer merge ranks are not unique")
	}
	special := make(map[string]int, len(config.AddedTokens))
	specialSet := make(map[string]any, len(config.AddedTokens))
	for _, token := range config.AddedTokens {
		if token.Content == "" {
			return nil, fmt.Errorf("MiniMax tokenizer special token is empty")
		}
		for existing := range special {
			if strings.HasPrefix(existing, token.Content) || strings.HasPrefix(token.Content, existing) {
				return nil, fmt.Errorf("MiniMax tokenizer special tokens overlap")
			}
		}
		special[token.Content] = token.ID
		specialSet[token.Content] = true
	}
	core, err := tiktoken.NewCoreBPE(ranks, special, pattern)
	if err != nil {
		return nil, fmt.Errorf("compile MiniMax tokenizer: %w", err)
	}
	return tiktoken.NewTiktoken(core, nil, specialSet), nil
}

func byteUnicodeInverse() map[rune]byte {
	values := make([]int, 0, 256)
	for value := int('!'); value <= int('~'); value++ {
		values = append(values, value)
	}
	for value := int('¡'); value <= int('¬'); value++ {
		values = append(values, value)
	}
	for value := int('®'); value <= int('ÿ'); value++ {
		values = append(values, value)
	}
	characters := append([]int(nil), values...)
	present := make(map[int]bool, len(values))
	for _, value := range values {
		present[value] = true
	}
	for value, offset := 0, 0; value < 256; value++ {
		if present[value] {
			continue
		}
		values = append(values, value)
		characters = append(characters, 256+offset)
		offset++
	}
	inverse := make(map[rune]byte, 256)
	for index, character := range characters {
		inverse[rune(character)] = byte(values[index])
	}
	return inverse
}
