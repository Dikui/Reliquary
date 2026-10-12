package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestVaultSupplementOnlyChangesMissingImageTargetsAndFileLinks(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "Immutable supplement", "# Evidence\n\nKeep every original character.\n")
	original := k.VaultBody
	// Seed an incomplete source image without downloading it.
	before := original + "\n![figure](https://example.test/missing.png)\n<img src='https://example.test/missing.png' alt='same figure'>"
	abs := filepath.Join(v.root, filepath.FromSlash(k.VaultPath))
	data, _, _ := readVaultArticle(t, v, k)
	header, _, err := SplitVaultArticle(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(abs, append(header, []byte(before)...), 0644))
	k.VaultBody = before
	k.Incomplete = true
	require.NoError(t, k.SetIngestMissing([]types.IngestMissing{{Type: "image", SourceURL: "https://example.test/missing.png", Reason: "未提供图片文件"}, {Type: "file", SourceURL: "https://example.test/data.txt", Reason: "原件尚未提供"}}))
	ref, err := v.StoreArticleAttachment(k, "figure.png", []byte("supplied original image bytes"))
	require.NoError(t, err)
	fileRef, err := v.StoreArticleAttachment(k, "data.txt", []byte("supplied file bytes"))
	require.NoError(t, err)
	retryRef, err := v.StoreArticleAttachment(k, "data.txt", []byte("supplied file bytes"))
	require.NoError(t, err)
	require.NotEqual(t, fileRef, retryRef)
	require.NoError(t, v.DeleteStoredArticleAttachment(k, retryRef))
	olderFile, err := v.ReadImage(k, fileRef)
	require.NoError(t, err)
	require.NoError(t, olderFile.Close())
	mappings := map[string]string{"https://example.test/missing.png": ref}
	links := []string{"[data](<" + fileRef + ">)"}
	allowed := AppendIngestAttachmentLinks(replaceVaultImageTargets(before, mappings), links)
	err = v.SupplementArticle(h.ctx, k, "Changed original words", mappings, links, false)
	require.ErrorContains(t, err, "changed article text")
	require.NotEmpty(t, v.ArticleError())
	change, err := v.BeginArticleSupplement(h.ctx, k, allowed, mappings, links, false)
	require.NoError(t, err)
	_, _, body := readVaultArticle(t, v, k)
	require.Equal(t, allowed, string(body))
	require.Contains(t, string(body), original)
	require.NotContains(t, string(body), "https://example.test/missing.png")
	require.NoError(t, change.rollback())
	_, _, body = readVaultArticle(t, v, k)
	require.Equal(t, before, string(body))
	k.VaultBody = before
	require.NoError(t, v.SupplementArticle(h.ctx, k, allowed, mappings, links, false))
	k.Incomplete = false
	err = v.SupplementArticle(h.ctx, k, allowed+"x", nil, nil, false)
	require.ErrorContains(t, err, "finalized")
	_, _, body = readVaultArticle(t, v, k)
	require.Equal(t, allowed, string(body))
}

func TestVaultSupplementFirstBodyRequiresRecordedContentMissing(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	k := h.upload(t, "Header only", "# Tutorial\n\nOriginal tutorial contains all clear instructions.")
	require.Empty(t, k.VaultBody)
	k.Incomplete = true
	err := v.SupplementArticle(h.ctx, k, "supplied body", nil, nil, true)
	require.ErrorContains(t, err, "recorded missing content")
	require.NoError(t, k.SetIngestMissing([]types.IngestMissing{{Type: "content", SourceURL: k.Source, Reason: "修正为全文保存，需要正文"}}))
	require.NoError(t, v.SupplementArticle(h.ctx, k, "supplied body", nil, nil, true))
	_, _, body := readVaultArticle(t, v, k)
	require.Equal(t, "supplied body", string(body))
}

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
