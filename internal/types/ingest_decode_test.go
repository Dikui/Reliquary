package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeIngestBatchSeparatesEnvelopeAndItemErrors(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"items":[]}`, `{"items":{}}`, `{"items":[{}],"unknown":true}`, `{"items":[{}]} {}`} {
		_, err := DecodeIngestBatch(strings.NewReader(raw))
		require.Error(t, err, raw)
	}
	request, err := DecodeIngestBatch(strings.NewReader(`{"items":[{"url":"https://example.org/valid","acquisition":"verbatim","content":"原文"},{"content":42},{"unknown":true},null,[]]}`))
	require.NoError(t, err)
	require.Len(t, request.Items, 5)
	require.Equal(t, "原文", request.Items[0].Content)
	require.Empty(t, request.Items[0].InputError)
	for _, item := range request.Items[1:] {
		require.NotEmpty(t, item.InputError)
	}
	for _, count := range []int{50, 51} {
		raw := `{"items":[` + strings.TrimSuffix(strings.Repeat(`{},`, count), ",") + `]}`
		request, err := DecodeIngestBatch(strings.NewReader(raw))
		if count == 50 {
			require.NoError(t, err)
			require.Len(t, request.Items, 50)
		} else {
			require.Error(t, err)
		}
	}
}

func TestDecodeIngestFileAcceptsAssetIDInlineAndObject(t *testing.T) {
	request, err := DecodeIngestBatch(strings.NewReader(`{"items":[{"url":"","file":"11111111-1111-4111-8111-111111111111"},{"url":"","file":"JVBERi0xLjQK"},{"url":"","file":{"data":"JVBERi0xLjQK","name":"original.pdf"}},{"url":"","file":{"data":"JVBERi0xLjQK","name":"original.pdf","unknown":true}}]}`))
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-4111-8111-111111111111", request.Items[0].File.AssetID)
	require.Equal(t, "JVBERi0xLjQK", request.Items[1].File.Data)
	require.Equal(t, "original.pdf", request.Items[2].File.Name)
	require.NotEmpty(t, request.Items[3].InputError)
}

func TestDecodeIngestRequiresURLFieldAndAllowsExplicitEmpty(t *testing.T) {
	request, err := DecodeIngestBatch(strings.NewReader(`{"items":[{"acquisition":"extracted","content":"原文"},{"url":"","acquisition":"extracted","content":"原文"},{"url":null,"acquisition":"extracted","content":"原文"}]}`))
	require.NoError(t, err)
	require.Len(t, request.Items, 3)
	require.Contains(t, request.Items[0].InputError, "url 必填")
	require.Empty(t, request.Items[1].InputError)
	require.Empty(t, request.Items[1].URL)
	require.Contains(t, request.Items[2].InputError, "url 必须是字符串")
}
