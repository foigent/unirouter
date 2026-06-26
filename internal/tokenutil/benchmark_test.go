package tokenutil

import (
	"strings"
	"testing"
)

func BenchmarkEstimateTokens_English(b *testing.B) {
	text := strings.Repeat("Hello world, this is a test message. ", 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkEstimateTokens_Chinese(b *testing.B) {
	text := strings.Repeat("你好世界，这是一条测试消息。", 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkEstimateTokens_Mixed(b *testing.B) {
	text := strings.Repeat("Hello 你好 world 世界 test 测试. ", 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkEstimateTokens_Short(b *testing.B) {
	text := "Hello"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(text)
	}
}

func BenchmarkEstimateRequestTokens(b *testing.B) {
	msgs := []struct{ Role, Content string }{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: strings.Repeat("What is the meaning of life? ", 10)},
		{Role: "assistant", Content: strings.Repeat("The meaning of life is ", 20)},
		{Role: "user", Content: "Can you elaborate?"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateRequestTokens(msgs)
	}
}
