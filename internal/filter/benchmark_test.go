package filter

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkDFAFilter_ShortText(b *testing.B) {
	words := make([]string, 1000)
	for i := range words {
		words[i] = fmt.Sprintf("word%d", i)
	}
	f := NewDFAFilter(words)
	text := "this is a normal message with word500 in it"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.IsSensitive(text)
	}
}

func BenchmarkDFAFilter_LongText(b *testing.B) {
	words := make([]string, 1000)
	for i := range words {
		words[i] = fmt.Sprintf("敏感词%d", i)
	}
	f := NewDFAFilter(words)
	text := strings.Repeat("这是一段很长的正常文本内容，", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.IsSensitive(text)
	}
}

func BenchmarkDFAFilter_NoMatch(b *testing.B) {
	words := make([]string, 5000)
	for i := range words {
		words[i] = fmt.Sprintf("word%d", i)
	}
	f := NewDFAFilter(words)
	text := "completely clean text with no matches at all"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.IsSensitive(text)
	}
}

func BenchmarkDFAFilter_WorstCase(b *testing.B) {
	// Many overlapping words that all match
	words := make([]string, 100)
	for i := range words {
		words[i] = fmt.Sprintf("ab%d", i)
	}
	f := NewDFAFilter(words)
	text := strings.Repeat("ab50 ab51 ab52 ab53 ab54 ab55 ", 50)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.IsSensitive(text)
	}
}

func BenchmarkDFAFilter_UpdateWords(b *testing.B) {
	words := make([]string, 1000)
	for i := range words {
		words[i] = fmt.Sprintf("word%d", i)
	}
	f := NewDFAFilter(words)

	newWords := make([]string, 1000)
	for i := range newWords {
		newWords[i] = fmt.Sprintf("new%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.UpdateWords(newWords)
	}
}
