package tokenprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelprofile"
)

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
	count, err := Count(modelprofile.TokenizerMiniMaxM27ID, modelprofile.TokenizerMiniMaxM27Version, []byte("hello"))
	require.NoError(t, err)
	require.Positive(t, count)

	_, err = Count(modelprofile.TokenizerMiniMaxM27ID, "2.0.0", []byte("hello"))
	require.Error(t, err)
	_, err = Count(modelprofile.TokenizerMiniMaxM27ID, modelprofile.TokenizerMiniMaxM27Version, []byte{0xff})
	require.Error(t, err)
}
