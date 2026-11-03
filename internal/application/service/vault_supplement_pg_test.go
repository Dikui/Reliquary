package service

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"net/http"
	"testing"

	"reliquary/internal/types"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVaultStoresJPEGWithGeneratedExtension(t *testing.T) {
	require.NoError(t, mime.AddExtensionType(".jfif", "image/jpeg"))
	var data bytes.Buffer
	require.NoError(t, jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil))
	v := &VaultService{root: t.TempDir()}
	k := &types.Entry{ID: uuid.NewString(), VaultPath: "library/article.md"}
	name := "image" + vaultImageExtension("resource://image", http.DetectContentType(data.Bytes()))
	ref, err := v.StoreArticleAttachment(k, name, data.Bytes())
	require.NoError(t, err)
	stored, err := v.ReadImage(k, ref)
	require.NoError(t, err)
	defer stored.Close()
	actual, err := io.ReadAll(stored)
	require.NoError(t, err)
	require.Equal(t, data.Bytes(), actual)
}

func TestVaultArticlePreservesBrowserBlobImages(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	for _, ref := range []string{"blob:http://localhost/source-image", "missing-local.png"} {
		k := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, SourceKind: "article", Type: "manual", Title: "Source images", ParseStatus: types.ParseStatusCompleted}
		require.NoError(t, h.db.Create(k).Error)
		body := fmt.Sprintf("# Original\n\n![source image](%s)\n", ref)
		err := v.WriteArticle(h.ctx, k, body, nil)
		if ref == "missing-local.png" {
			require.ErrorContains(t, err, "image reader is unavailable")
			continue
		}
		require.NoError(t, err)
		_, _, saved := readVaultArticle(t, v, k)
		require.Equal(t, body, string(saved))
	}
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
