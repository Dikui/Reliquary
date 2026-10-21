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
	request, err := DecodeIngestBatch(strings.NewReader(`{"items":[{"url":"https://example.org/valid","library":"official","acquisition":"verbatim","content":"原文"},{"content":42},{"unknown":true},null,[]]}`))
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

func TestDecodeIngestAllowsOptionalURL(t *testing.T) {
	request, err := DecodeIngestBatch(strings.NewReader(`{"items":[{"library":"official","acquisition":"extracted","content":"原文"},{"url":"","acquisition":"extracted","content":"原文"},{"url":null,"acquisition":"extracted","content":"原文"}]}`))
	require.NoError(t, err)
	require.Len(t, request.Items, 3)
	require.Empty(t, request.Items[0].InputError)
	require.Empty(t, request.Items[1].InputError)
	require.Empty(t, request.Items[1].URL)
	require.Empty(t, request.Items[2].InputError)
}

func TestDecodeIngestMineAllowsOmittedURLAndRejectsImageCompletion(t *testing.T) {
	request, err := DecodeIngestBatch(strings.NewReader(`{"items":[{"library":"personal","acquisition":"verbatim","content":"本人原文"},{"library":"personal","entry_id":"11111111-1111-4111-8111-111111111111","url":"","acquisition":"verbatim"},{"library":"personal","url":null,"acquisition":"verbatim"}]}`))
	require.NoError(t, err)
	require.Empty(t, request.Items[0].InputError)
	require.Equal(t, "personal", request.Items[0].Library)
	require.Empty(t, request.Items[0].URL)
	require.Contains(t, request.Items[1].InputError, "entry_id")
	require.Empty(t, request.Items[2].InputError)
}
