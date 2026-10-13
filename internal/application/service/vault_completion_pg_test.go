package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVaultCompletedTutorialPreservesFullBody(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	k := h.upload(t, "Tutorial stays complete", "# Tutorial\n\nOriginal source remains complete.")
	require.NoError(t, v.WriteArticle(h.ctx, k, "# Tutorial\n\nOriginal source remains complete.", nil))
	_, _, body := readVaultArticle(t, v, k)
	require.Contains(t, string(body), "Original source remains complete.")
	require.Contains(t, k.VaultBody, "Original source remains complete.")
}
