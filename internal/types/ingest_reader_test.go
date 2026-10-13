package types

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"io"
	"strings"
	"testing"
)

type countingIngestReader struct {
	io.Reader
	read int64
}

func (r *countingIngestReader) Read(p []byte) (int, error) {
	n, e := r.Reader.Read(p)
	r.read += int64(n)
	return n, e
}

type repeatedByteReader byte

func (b repeatedByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}

func TestIngestReaderCodePointsAndEarlyLimits(t *testing.T) {
	for _, unit := range []string{"界", "😀", `\u754c`, `\ud83d\ude00`} {
		// Each encoding must count as exactly one code point.
		raw := `{"items":[{"library":"mine","content":"` + strings.Repeat(unit, MaxIngestContentRunes) + `","acquisition":"verbatim"}]}`
		got, err := DecodeIngestBatch(strings.NewReader(raw))
		require.NoError(t, err)
		require.Len(t, []rune(got.Items[0].Content), MaxIngestContentRunes)
	}
	reader := &countingIngestReader{Reader: io.MultiReader(strings.NewReader(`{"items":[{"content":"`), io.LimitReader(repeatedByteReader('a'), 100_000_000))}
	_, err := DecodeIngestBatch(reader)
	require.ErrorContains(t, err, "5000000")
	require.Less(t, reader.read, int64(MaxIngestContentRunes+65536))
	bytes := &countingIngestReader{Reader: io.MultiReader(strings.NewReader(`{"other":"`), io.LimitReader(repeatedByteReader('a'), 100_000_000))}
	decoder := json.NewDecoder(NewIngestJSONReader(bytes, 1<<20))
	var value any
	require.ErrorContains(t, decoder.Decode(&value), "byte limit")
	require.LessOrEqual(t, bytes.read, int64((1<<20)+1))
}

func TestIngestByteLimitsStopStreamingWithoutBuffering(t *testing.T) {
	for _, test := range []struct {
		prefix string
		limit  int64
		want   int64
	}{
		{`{"extra":"`, MaxIngestBatchBytes, MaxIngestBatchBytes},
		{`{"items":[{"agent_summary":"`, MaxIngestBatchBytes, MaxIngestItemBytes + 32},
		{`{"extra":"`, MaxIngestMCPBytes, MaxIngestMCPBytes},
	} {
		reader := &countingIngestReader{Reader: io.MultiReader(strings.NewReader(test.prefix), io.LimitReader(repeatedByteReader('a'), 200*1024*1024))}
		_, err := io.Copy(io.Discard, NewIngestJSONReader(reader, test.limit))
		require.Error(t, err)
		require.LessOrEqual(t, reader.read, test.want+32768)
	}
}
