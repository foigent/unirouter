package tokenprofile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKimiK26MatchesFrozenOfficialTokenizerVectors(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []int
	}{
		{name: "ascii", text: "Hello, Kimi K2.6!", want: []int{19180, 11, 24980, 72, 1040, 17, 13, 21, 0}},
		{name: "chinese", text: "请只回复 OK，不要输出其他内容。", want: []int{2066, 989, 14708, 17522, 378, 2556, 13791, 3055, 3311, 292}},
		{name: "unicode", text: "Cafe\u0301 and café — 東京🙂", want: []int{34, 6748, 37484, 316, 70591, 4275, 220, 138442, 5885, 46813}},
		{name: "pattern edges", text: "ABCDef xyz's 123456\n\n  tabs\t\t汉字🙂!!!\r\n", want: []int{55335, 3522, 69215, 1048, 220, 6694, 12972, 382, 220, 34530, 197, 197, 38325, 5885, 46813, 20118, 462}},
		{name: "special", text: "[BOS]<|im_user|>hello<|im_end|>[EOS]", want: []int{163584, 163587, 22931, 163586, 163585}},
		{name: "json", text: `{"model":"kimi-k2.6","messages":[{"role":"user","content":"hello"}]}`, want: []int{8264, 7940, 7471, 74, 30223, 12467, 17, 13, 21, 3923, 36603, 76350, 13481, 7471, 2482, 3923, 4204, 7471, 22931, 16934, 24216}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := encodeKimiK26([]byte(test.text))
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestKimiK26LongRunMatchesOfficialSafetySplitting(t *testing.T) {
	count, err := Count(
		KimiK26ID,
		KimiK26Version,
		[]byte(strings.Repeat("a", 100_000)),
	)
	require.NoError(t, err)
	require.Equal(t, 12_500, count)
}

func TestMiniMaxM27MatchesFrozenOfficialTokenizerVectors(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []int
	}{
		{name: "ascii", text: "Hello, MiniMax M2.7!", want: []int{19739, 44, 35353, 12973, 373, 50, 46, 55, 33}},
		{name: "chinese", text: "请只回复 OK，不要输出其他内容。", want: []int{4088, 1938, 28485, 20900, 22317, 21537, 5576, 4802, 350}},
		{name: "NFC", text: "Cafe\u0301 and café", want: []int{67, 154797, 306, 22778}},
		{name: "added tokens", text: `<think>reasoning</think><minimax:tool_call>{"name":"x"}</minimax:tool_call>`, want: []int{200050, 64639, 289, 200051, 200052, 16673, 1906, 15368, 120, 22089, 200053}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := encodeMiniMaxM27([]byte(test.text))
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestCountRequiresExactTokenizerProfileAndValidUTF8(t *testing.T) {
	kimiCount, err := Count(KimiK26ID, KimiK26Version, []byte("hello"))
	require.NoError(t, err)
	require.Positive(t, kimiCount)

	count, err := Count(MiniMaxM27ID, MiniMaxM27Version, []byte("hello"))
	require.NoError(t, err)
	require.Positive(t, count)

	_, err = Count(MiniMaxM27ID, "2.0.0", []byte("hello"))
	require.Error(t, err)
	_, err = Count(MiniMaxM27ID, MiniMaxM27Version, []byte{0xff})
	require.Error(t, err)
	_, err = Count(KimiK26ID, KimiK26Version, []byte{0xff})
	require.Error(t, err)
}
