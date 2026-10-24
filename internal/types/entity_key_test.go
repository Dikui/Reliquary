package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeEntityKey(t *testing.T) {
	for _, tc := range [][2]string{
		{
			" ＯｐｅｎＡＩ ",
			"openai",
		},
		{
			"Hugging\tFace\n",
			"huggingface",
		},
		{
			"深 度\u3000求索",
			"深度求索",
		},
		{
			"Straße",
			"straße",
		},
		{
			"E\u0301",
			"é",
		},
		{
			"\u00a0\u2003",
			"",
		},
	} {
		require.Equal(t, tc[1], NormalizeEntityKey(tc[0]))
	}
}
