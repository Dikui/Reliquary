package service

import (
	"testing"

	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func TestVaultPublishedIngestWriteRejectsAnyOtherBody(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "Final ingest body", "# Original\n\nAll original words and whitespace remain exact.\n")
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", k.ID).Update("acquisition", "verbatim").Error)
	require.NoError(t, v.RefreshHeader(h.ctx, k.ID))
	before, _, body := readVaultArticle(t, v, k)
	require.NoError(t, v.WriteArticle(h.ctx, k, string(body), nil))
	for _, incomplete := range []bool{false, true} {
		require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", k.ID).Update("incomplete", incomplete).Error)
		err := v.WriteArticle(h.ctx, k, string(body)+"changed", nil)
		require.ErrorContains(t, err, "replacement refused")
		require.Contains(t, v.ArticleError(), "body differs")
		current, _, saved := readVaultArticle(t, v, k)
		require.Equal(t, before, current)
		require.Equal(t, body, saved)
	}
}
