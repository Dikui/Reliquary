package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestVaultCompletedHeaderOnlyRequiresMarkerAndConsumesIt(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	k := h.upload(t, "Summary completion", "# Original tutorial\n\nAll source instructions are retained until completion.")
	require.Empty(t, k.VaultBody)
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", k.ID).Updates(map[string]any{"acquisition": "extracted", "processing_mode": "full", "content_type": "research"}).Error)
	image, err := readVaultImage(h.ctx, articleImageDataURI(t, 45), nil)
	require.NoError(t, err)
	body := "# First full body\n\nOriginal words remain exact.\n\n![chart](local://fixture/chart.png)\n\n[file](<local://fixture/source.txt>)"
	read := func(_ context.Context, ref string) (io.ReadCloser, error) {
		switch ref {
		case "local://fixture/chart.png":
			return io.NopCloser(bytes.NewReader(image)), nil
		case "local://fixture/source.txt":
			return io.NopCloser(bytes.NewReader([]byte("Original source file"))), nil
		default:
			t.Fatalf("unexpected material reader: %s", ref)
			return nil, nil
		}
	}
	require.ErrorContains(t, v.WriteArticle(h.ctx, k, body, read), "replacement refused")
	require.NoError(t, h.db.First(k, "id=?", k.ID).Error)
	metadata := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	metadata["completion_pending"] = json.RawMessage(`true`)
	k.Metadata, err = json.Marshal(metadata)
	require.NoError(t, err)
	k.FilePath, k.FileName = "local://fixture/source.txt", "source.txt"
	require.NoError(t, k.SetIngestAttachments([]types.IngestAttachment{{Name: "source.txt", Type: "txt", Path: k.FilePath, Size: 20}}))
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", k.ID).Updates(map[string]any{"metadata": k.Metadata, "file_path": k.FilePath, "file_name": k.FileName, "ingest_attachments": k.IngestAttachments}).Error)
	require.NoError(t, v.WriteArticle(h.ctx, k, body, read))
	_, header, saved := readVaultArticle(t, v, k)
	require.Contains(t, string(header), "content_type: \"research\"")
	require.Contains(t, string(saved), "Original words remain exact.")
	require.NotContains(t, string(saved), "local://")
	require.Contains(t, string(saved), "../../attachments/"+k.ID+"/")
	require.Equal(t, 1, bytes.Count(saved, []byte("[file]")))
	require.False(t, ingestCompletionPending(k))
	var persisted types.Entry
	require.NoError(t, h.db.First(&persisted, "id=?", k.ID).Error)
	require.Equal(t, string(saved), persisted.VaultBody)
	require.False(t, ingestCompletionPending(&persisted))
	original, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(k.VaultOriginalPath)))
	require.NoError(t, err)
	require.Equal(t, "Original source file", string(original))
	metadata["completion_pending"] = json.RawMessage(`true`)
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", k.ID).Update("metadata", types.JSON(raw)).Error)
	require.ErrorContains(t, v.WriteArticle(h.ctx, k, "changed final body", read), "header-only")
}

func TestVaultCompletedSummaryKeepsPublicBodyEmpty(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	k := h.upload(t, "Summary stays summary", "# Tutorial\n\nOriginal source is processed as a summary.")
	metadata := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	metadata["completion_pending"] = json.RawMessage(`true`)
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", k.ID).Updates(map[string]any{"metadata": types.JSON(raw), "acquisition": "verbatim"}).Error)
	require.NoError(t, v.WriteArticle(h.ctx, k, "Complete raw input has no public body in summary mode", nil))
	_, _, body := readVaultArticle(t, v, k)
	require.Empty(t, body)
	require.Empty(t, k.VaultBody)
	require.False(t, ingestCompletionPending(k))
}
