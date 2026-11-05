package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

// Regression (medium), task mel-01: first processing must match products against
// the saved body, including image destinations rewritten during publication.
func TestProductLinksOnFirstArticleProcessingUseSavedBody(t *testing.T) {
	for _, vault := range []bool{false, true} {
		for _, managed := range []bool{false, true} {
			t.Run(fmt.Sprintf("vault=%t/managed=%t", vault, managed), func(t *testing.T) {
				var h *articleProcessingHarness
				if vault {
					h, _ = newVaultArticleHarness(t)
				} else {
					h = newArticleProcessingHarness(t, true)
				}
				products := h.library
				if managed {
					h.library.EntityMode = types.EntityModeManaged
				} else {
					products = &types.Library{ID: "products", SpaceID: h.library.SpaceID, Name: "Products", Directory: "products", EntityMode: types.EntityModeManaged}
					require.NoError(t, h.db.Create(products).Error)
					h.library.ProductLibraryIDs = []string{products.ID}
				}
				require.NoError(t, h.db.Save(h.library).Error)
				product := types.Entity{ID: "widget-pro", SpaceID: products.SpaceID, LibraryID: products.ID, CanonicalName: "Widget Pro", NormalizedName: "widget pro", EntityType: "product"}
				require.NoError(t, h.db.Create(&product).Error)
				h.processor.output = articleProcessingOutput(t, "event", nil, []string{})
				body := "# 新品\n\n![统计](" + articleImageDataURI(t, 30) + ")\n\nWidget Pro reports new measurements."
				entry, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID,
					newMultipartFileHeader(t, "products.md", body),
					map[string]string{"publisher": "Acme", "original_url": "https://example.org/products", "published_at": "2026-10-01"},
					nil, "", types.ChannelWeb, nil)
				require.NoError(t, err)
				require.Empty(t, entry.VaultBody, "the upload has not saved its parsed body yet")
				// Approved imports skip intake's early body save, as in the
				// original undated-article regression.
				intake, err := entry.IntakeMetadata()
				require.NoError(t, err)
				intake.Decision = "approved"
				require.NoError(t, entry.SetIntakeMetadata(intake))
				require.NoError(t, h.db.Save(entry).Error)
				h.drain(t)
				entry = h.article(t, entry.ID)
				require.Equal(t, types.ParseStatusCompleted, entry.ParseStatus, entry.ErrorMessage)
				require.Equal(t, types.SummaryStatusCompleted, entry.SummaryStatus, entry.ErrorMessage)
				reader, _, err := h.service.GetEntryBody(h.ctx, entry.ID)
				require.NoError(t, err)
				savedBody, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, reader.Close())
				require.NotContains(t, string(savedBody), "data:image", "image materialization changes the original body's offsets")
				links, err := NewEntityService(h.db, h.service.modelService).EntryEntityLinks(h.ctx, entry.ID)
				require.NoError(t, err)
				require.Len(t, links, 1)
				require.Equal(t, product.ID, links[0].Entity.ID)
				require.Equal(t, "extracted", links[0].Origin)
				require.Equal(t, "references", links[0].RelationType)
				hash := fmt.Sprintf("%x", sha256.Sum256(savedBody))
				require.Equal(t, hash, links[0].BodyHash)
				require.Len(t, links[0].Evidence, 1)
				location := links[0].Evidence[0]
				require.Equal(t, hash, location.BodyHash)
				require.Equal(t, "Widget Pro", string([]rune(string(savedBody))[location.Start:location.End]))
			})
		}
	}
}

// Regression (medium), task mel-01: no product targets must not disable cleanup
// of old local extraction links or discard current/manual local associations.
func TestProductLinksWithoutTargetsCleanStaleLocalEntities(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.processor.output = articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Acme", EntityType: "company"}}, []string{})
	entry, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID,
		newMultipartFileHeader(t, "local-entities.md", "Acme reports new measurements."),
		map[string]string{"publisher": "Acme", "original_url": "https://example.org/local-entities", "published_at": "2026-10-01"},
		nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	stale := types.Entity{ID: "stale", SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, CanonicalName: "Previous", NormalizedName: "previous", EntityType: "company"}
	manual := types.Entity{ID: "manual", SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, CanonicalName: "Manual", NormalizedName: "manual", EntityType: "company"}
	require.NoError(t, h.db.Create(&[]types.Entity{stale, manual}).Error)
	require.NoError(t, h.db.Create(&[]types.EntryEntity{
		{SpaceID: entry.SpaceID, EntryID: entry.ID, EntityID: stale.ID, Origin: "extracted"},
		{SpaceID: entry.SpaceID, EntryID: entry.ID, EntityID: manual.ID, Origin: "manual"},
	}).Error)
	require.Empty(t, h.library.ProductLibraryIDs)
	h.drain(t)
	entities := NewEntityService(h.db, h.service.modelService)
	previous, err := entities.EntityDetail(h.ctx, stale.ID)
	require.NoError(t, err)
	require.Empty(t, previous.Entries)
	kept, err := entities.EntityDetail(h.ctx, manual.ID)
	require.NoError(t, err)
	require.Len(t, kept.Entries, 1)
	current, err := entities.ListEntities(h.ctx, types.EntityQuery{LibraryID: h.library.ID, Entity: "Acme"})
	require.NoError(t, err)
	require.Len(t, current, 1)
	detail, err := entities.EntityDetail(h.ctx, current[0].ID)
	require.NoError(t, err)
	require.Len(t, detail.Entries, 1)
}

// Replace the remote embedding service with a deterministic fixture that also
// simulates a user body save while processing is waiting on the model.
type productBodyEditEmbedder struct {
	chunkSearchEmbedder
	edit func()
}

func (e productBodyEditEmbedder) Embed(ctx context.Context, body string) ([]float32, error) {
	if stage, _ := types.LLMCallMetadataFromContext(ctx); stage == "article_embedding" {
		e.edit()
	}
	return e.chunkSearchEmbedder.Embed(ctx, body)
}

type productBodyEditModels struct {
	articleProcessingModels
	embedder embedding.Embedder
}

func (m productBodyEditModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

// Regression (medium), task mel-01: the locked saved-body check rejects a
// processing result whose body was replaced before product links were saved.
func TestProductLinksRejectBodyChangedDuringProcessing(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.library.EntityMode = types.EntityModeManaged
	require.NoError(t, h.db.Save(h.library).Error)
	product := types.Entity{ID: "widget-pro", SpaceID: h.library.SpaceID, LibraryID: h.library.ID, CanonicalName: "Widget Pro", NormalizedName: "widget pro", EntityType: "product"}
	require.NoError(t, h.db.Create(&product).Error)
	h.processor.output = articleProcessingOutput(t, "event", nil, []string{})
	entry, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID,
		newMultipartFileHeader(t, "changed-products.md", "Widget Pro reports new measurements."),
		map[string]string{"publisher": "Acme", "original_url": "https://example.org/changed-products", "published_at": "2026-10-01"},
		nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	edited := false
	h.service.modelService = productBodyEditModels{
		articleProcessingModels: h.service.modelService.(articleProcessingModels),
		embedder: productBodyEditEmbedder{edit: func() {
			require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", entry.ID).Update("vault_body", "Replacement without the product mention.").Error)
			edited = true
		}},
	}
	require.Len(t, h.queue.tasks, 1)
	err = h.service.ProcessDocument(h.ctx, h.queue.tasks[0])
	require.True(t, edited, "the body was changed while processing awaited embedding")
	require.ErrorContains(t, err, "body changed")
	links, err := NewEntityService(h.db, h.service.modelService).EntryEntityLinks(h.ctx, entry.ID)
	require.NoError(t, err)
	require.Empty(t, links, "old body mentions must not create associations after a body edit")
}
