package filter

import (
	"strings"
	"sync"
)

// DFAFilter implements a deterministic finite automaton for sensitive word filtering.
type DFAFilter struct {
	root     *dfaNode
	mu       sync.RWMutex
	wordList []string
}

type dfaNode struct {
	children map[rune]*dfaNode
	isEnd    bool
}

// NewDFAFilter creates a new DFA filter with the given word list.
func NewDFAFilter(words []string) *DFAFilter {
	f := &DFAFilter{
		root:     &dfaNode{children: make(map[rune]*dfaNode)},
		wordList: words,
	}
	for _, w := range words {
		f.addWord(w)
	}
	return f
}

func (f *DFAFilter) addWord(word string) {
	node := f.root
	for _, ch := range strings.ToLower(word) {
		if _, ok := node.children[ch]; !ok {
			node.children[ch] = &dfaNode{children: make(map[rune]*dfaNode)}
		}
		node = node.children[ch]
	}
	node.isEnd = true
}

// IsSensitive checks if the text contains any sensitive words.
func (f *DFAFilter) IsSensitive(text string) (bool, []string) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	lower := strings.ToLower(text)
	runes := []rune(lower)
	found := make(map[string]bool)
	var matches []string

	for i := 0; i < len(runes); i++ {
		node := f.root
		j := i
		lastMatch := -1
		for j < len(runes) {
			child, ok := node.children[runes[j]]
			if !ok {
				break
			}
			node = child
			if node.isEnd {
				lastMatch = j
			}
			j++
		}
		if lastMatch >= 0 {
			word := string(runes[i : lastMatch+1])
			if !found[word] {
				found[word] = true
				matches = append(matches, word)
			}
		}
	}

	return len(matches) > 0, matches
}

// UpdateWords replaces the word list with a new one.
func (f *DFAFilter) UpdateWords(words []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.root = &dfaNode{children: make(map[rune]*dfaNode)}
	f.wordList = words
	for _, w := range words {
		f.addWord(w)
	}
}

// WordCount returns the number of words in the filter.
func (f *DFAFilter) WordCount() int {
	return len(f.wordList)
}
