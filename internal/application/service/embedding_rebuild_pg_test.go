package service

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reliquary/internal/config"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

type allRebuildEmbedder struct {
	nameRebuildEmbedder
	supportImages bool
	calls         int
	imageCalls    int
	onEmbed       func(context.Context, string) error
	onImage       func(context.Context) error
}

func (e *allRebuildEmbedder) SupportsImages() bool { return e.supportImages }
func (e *allRebuildEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if !types.IsBackgroundTask(ctx) || types.ModelTierFromContext(ctx) != types.ModelTierEmbedding {
		return nil, errors.New("all rebuild calls must use background embedding tier")
	}
	e.calls++
	e.texts = append(e.texts, text)
	if e.onEmbed != nil {
		if err := e.onEmbed(ctx, text); err != nil {
			return nil, err
		}
	}
	return e.vector(), nil
}
func (e *allRebuildEmbedder) EmbedImage(ctx context.Context, dataURL string) ([]float32, error) {
	e.imageCalls++
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, errors.New("expected authenticated vault image bytes")
	}
	if e.onImage != nil {
		if err := e.onImage(ctx); err != nil {
			return nil, err
		}
	}
	return e.vector(), nil
}

type allRebuildFixture struct {
	*entrySearchFixture
	rebuilder *EmbeddingRebuilder
	embedder  *allRebuildEmbedder
	queue     *nameRebuildQueue
	models    *nameRebuildModels
	full      *types.Entry
	imagePath string
}

func newAllRebuildFixture(t *testing.T, images bool) *allRebuildFixture {
	t.Helper()
	h := newEntrySearchFixture(t)
	full := h.add(t, "full", "article", "2026-10-01", "event", "semanticlaunch", 2)
	h.add(t, "summary", "article", "2026-10-02", "tutorial", "semanticlaunch", 0)
	h.add(t, "mine", "mine", "", "", "personal", 1)
	h.add(t, "report", "report", "", "", "analysis", 1)
	require.NoError(t, h.db.Create(&types.Entity{ID: "entity", SpaceID: 1, CanonicalName: "Rebuild Entity", NormalizedName: "rebuild entity", EntityType: "product", EmbeddingNeedsRebuild: true,
		EmbeddingModelID: "fixture-model", EmbeddingDimension: 3, NameEmbedding: types.EntityNameEmbedding{0, 1, 0}}).Error)
	root := t.TempDir()
	vault, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	full.VaultPath = "articles/2026-10/full.md"
	imagePath := "../../attachments/full/photo.png"
	full.VaultBody = "Plain body\n\n![image](" + imagePath + ")"
	require.NoError(t, h.db.Save(full).Error)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "attachments", full.ID), 0755))
	dataURL := articleImageDataURI(t, 42)
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, "data:image/png;base64,"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "attachments", full.ID, "photo.png"), data, 0644))
	require.NoError(t, h.db.Exec(`INSERT INTO image_embeddings(space_id,entry_id,library_id,attachment_path,model_id,dimension,embedding,content_hash)
		VALUES(1,?, ?, ?, 'fixture-model',3,?,'old')`, full.ID, h.library.ID, imagePath, pgvector.NewHalfVector([]float32{0, 1, 0})).Error)
	configureNameEmbedding(t, h.db, "replacement-model", 3)
	h.library.EmbeddingModelID, h.library.ModelTiers.Embedding = "replacement-model", "replacement-model"
	require.NoError(t, h.db.Save(h.library).Error)
	embedder := &allRebuildEmbedder{nameRebuildEmbedder: nameRebuildEmbedder{id: "replacement-model", dimension: 3}, supportImages: images}
	models := &nameRebuildModels{models: map[string]embedding.Embedder{"replacement-model": embedder}}
	queue := &nameRebuildQueue{}
	return &allRebuildFixture{entrySearchFixture: h, rebuilder: NewEmbeddingRebuilder(h.db, models, queue, vault), embedder: embedder, queue: queue, models: models, full: full, imagePath: imagePath}
}

func (h *allRebuildFixture) state(t *testing.T) *types.EmbeddingRebuildRun {
	t.Helper()
	status, err := h.rebuilder.Status(h.ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, status.LatestRun)
	return status.LatestRun
}

func TestAllEmbeddingRebuildProgressAndEveryVectorIdentity(t *testing.T) {
	h := newAllRebuildFixture(t, true)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 8, run.TotalCount)
	require.NoError(t, h.rebuilder.Enqueue(h.ctx, run))
	require.Equal(t, types.TypeEmbeddingRebuild, h.queue.last(t).Type())
	var progress []int64
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, func(value *types.EmbeddingRebuildRun) { progress = append(progress, value.CompletedCount) }))
	require.Equal(t, []int64{1, 2, 3, 4, 5, 6, 7, 8}, progress)
	status := h.state(t)
	require.Equal(t, "completed", status.Status)
	require.EqualValues(t, 8, status.CompletedCount)
	require.Zero(t, status.FailedCount)
	require.Equal(t, 7, h.embedder.calls)
	require.Equal(t, 1, h.embedder.imageCalls)
	for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
		var wrong int64
		require.NoError(t, h.db.Table(table).Where("model_id<>? OR dimension<>3", "replacement-model").Count(&wrong).Error)
		require.Zero(t, wrong, table)
	}
	entity := readNameEntity(t, h.db, "entity")
	require.Equal(t, "replacement-model", entity.EmbeddingModelID)
	require.Equal(t, 3, entity.EmbeddingDimension)
	require.False(t, entity.EmbeddingNeedsRebuild)
	var documents []types.Entry
	require.NoError(t, h.db.Find(&documents).Error)
	for _, document := range documents {
		require.Equal(t, "replacement-model", document.EmbeddingModelID)
	}
	require.Contains(t, strings.Join(h.embedder.texts, "\n"), "本人")
	require.Contains(t, strings.Join(h.embedder.texts, "\n"), "整理结果")
	// Consuming the previously queued task cannot rerun completed work.
	require.NoError(t, h.rebuilder.Handle(h.ctx, h.queue.last(t)))
	require.Equal(t, 7, h.embedder.calls)
}

func TestMaintenanceEmbeddingRebuildPreservesAllEntityProfileColumns(t *testing.T) {
	for _, preserve := range []bool{true, false} {
		name := "normal"
		if preserve {
			name = "maintenance"
		}
		t.Run(name, func(t *testing.T) {
			h := newAllRebuildFixture(t, false)
			originalAt := time.Date(2025, 1, 2, 3, 4, 5, 123456000, time.UTC)
			require.NoError(t, h.db.Model(&types.Entity{}).Where("id='entity'").UpdateColumn("updated_at", originalAt).Error)
			var before, after string
			profileSQL := `SELECT (to_jsonb(e)-'name_embedding'-'embedding_model_id'-'embedding_dimension'-'embedding_needs_rebuild')::text FROM entities e WHERE id='entity'`
			require.NoError(t, h.db.Raw(profileSQL).Scan(&before).Error)
			h.rebuilder.PreserveEntityProfileTimestamps(preserve)
			run, err := h.rebuilder.Start(h.ctx, 1)
			require.NoError(t, err)
			require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
			require.Equal(t, "completed", h.state(t).Status)
			require.NoError(t, h.db.Raw(profileSQL).Scan(&after).Error)
			entity := readNameEntity(t, h.db, "entity")
			require.Equal(t, "replacement-model", entity.EmbeddingModelID)
			require.Equal(t, 3, entity.EmbeddingDimension)
			require.False(t, entity.EmbeddingNeedsRebuild)
			require.Equal(t, types.EntityNameEmbedding(h.embedder.vector()), entity.NameEmbedding)
			if preserve {
				require.JSONEq(t, before, after, "all nonregenerated profile columns must survive maintenance")
				require.True(t, entity.UpdatedAt.Equal(originalAt))
			} else {
				require.True(t, entity.UpdatedAt.After(originalAt), "ordinary task 12 rebuild keeps its existing timestamp behavior")
			}
		})
	}
}

func TestMaintenanceEmbeddingRebuildRejectsConcurrentEntityTimestampEdit(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	h.rebuilder.PreserveEntityProfileTimestamps(true)
	var editedAt time.Time
	h.embedder.onEmbed = func(_ context.Context, text string) error {
		if strings.Contains(text, "Rebuild Entity") {
			editedAt = time.Date(2026, 10, 11, 10, 20, 30, 123456000, time.UTC)
			require.NoError(t, h.db.Model(&types.Entity{}).Where("id='entity'").UpdateColumn("updated_at", editedAt).Error)
		}
		return nil
	}
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.EqualValues(t, 1, h.state(t).FailedCount)
	entity := readNameEntity(t, h.db, "entity")
	require.True(t, entity.UpdatedAt.Equal(editedAt), "maintenance must not overwrite a concurrent profile edit")
	require.Equal(t, "fixture-model", entity.EmbeddingModelID)
	require.True(t, entity.EmbeddingNeedsRebuild)
}

func TestAllEmbeddingRebuildInterruptResumeAndFailedRetry(t *testing.T) {
	h := newAllRebuildFixture(t, true)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(h.ctx)
	err = h.rebuilder.Run(ctx, 1, run.ID, func(value *types.EmbeddingRebuildRun) {
		if value.CompletedCount == 2 {
			cancel()
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	paused := h.state(t)
	require.Equal(t, "paused", paused.Status)
	require.EqualValues(t, 2, paused.CompletedCount)
	h.embedder.onEmbed = func(_ context.Context, text string) error {
		if strings.Contains(text, "Rebuild Entity") {
			return errors.New("temporary model outage")
		}
		return nil
	}
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, false))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	failed := h.state(t)
	require.Equal(t, "completed_with_errors", failed.Status)
	require.EqualValues(t, 7, failed.CompletedCount)
	require.EqualValues(t, 1, failed.FailedCount)
	status, err := h.rebuilder.Status(h.ctx, 1)
	require.NoError(t, err)
	require.Len(t, status.FailedItems, 1)
	require.True(t, status.CurrentModelMatches)
	require.Equal(t, "entity", status.FailedItems[0].Kind)
	require.Contains(t, status.FailedItems[0].Error, "temporary model outage")
	calls := h.embedder.calls
	h.embedder.onEmbed = nil
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, true))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, calls+1, h.embedder.calls, "failed retry does not repeat completed vectors")
	require.Equal(t, "completed", h.state(t).Status)
	require.EqualValues(t, 8, h.state(t).CompletedCount)
	var item types.EmbeddingRebuildItem
	require.NoError(t, h.db.Where("run_id=? AND kind='entity'", run.ID).First(&item).Error)
	require.Equal(t, 2, item.Attempts)
}

func TestAllEmbeddingRebuildPauseFencesInFlightAndRetrievalRemainsUsable(t *testing.T) {
	h := newAllRebuildFixture(t, true)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	h.embedder.onEmbed = func(_ context.Context, _ string) error {
		// This runs while the embedding provider is working, before the write.
		response := h.search(t, types.EntrySearchRequest{Query: "searchfixture", CollapseSimilar: entrySearchNoCollapse()})
		require.NotEmpty(t, response.Results, "keyword search continues while old vector identities are excluded")
		require.NoError(t, h.rebuilder.Pause(h.ctx, 1, run.ID))
		return nil
	}
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "paused", h.state(t).Status)
	require.Zero(t, h.state(t).CompletedCount, "in-flight embedding cannot commit after pause")
	var oldCount int64
	require.NoError(t, h.db.Table("article_embeddings").Where("model_id='fixture-model'").Count(&oldCount).Error)
	require.EqualValues(t, 2, oldCount, "existing vectors remain until successful replacement")
	h.embedder.onEmbed = nil
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, false))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "completed", h.state(t).Status)
}

func TestAllEmbeddingRebuildTextOnlyAndModelChange(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 7, run.TotalCount)
	require.NoError(t, h.rebuilder.Pause(h.ctx, 1, run.ID))
	configureNameEmbedding(t, h.db, "another-model", 2)
	h.models.models["another-model"] = &allRebuildEmbedder{nameRebuildEmbedder: nameRebuildEmbedder{id: "another-model", dimension: 2}}
	previousStatus, statusErr := h.rebuilder.Status(h.ctx, 1)
	require.NoError(t, statusErr)
	require.False(t, previousStatus.CurrentModelMatches)
	require.ErrorContains(t, h.rebuilder.Continue(h.ctx, 1, run.ID, false), "embedding model changed")
	newRun, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err, "paused old-model run must not prevent a new-model rebuild")
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, newRun.ID, nil))
	require.Equal(t, "completed", h.state(t).Status)
	require.Equal(t, "another-model", h.state(t).ModelID)
	var original types.EmbeddingRebuildRun
	require.NoError(t, h.db.First(&original, "id=?", run.ID).Error)
	require.Equal(t, "failed", original.Status)
	var images int64
	require.NoError(t, h.db.Model(&types.EmbeddingRebuildItem{}).Where("run_id=? AND kind='image'", newRun.ID).Count(&images).Error)
	require.Zero(t, images)
}

func TestAllEmbeddingRebuildImagePauseDoesNotWaitForProvider(t *testing.T) {
	h := newAllRebuildFixture(t, true)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	h.embedder.onImage = func(context.Context) error {
		pauseCtx, cancel := context.WithTimeout(h.ctx, time.Second)
		defer cancel()
		// The provider call must not hold the checkpoint row or model locks.
		require.NoError(t, h.rebuilder.Pause(pauseCtx, 1, run.ID))
		return nil
	}
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	paused := h.state(t)
	require.Equal(t, "paused", paused.Status)
	require.EqualValues(t, 5, paused.CompletedCount)
	var identity string
	require.NoError(t, h.db.Table("image_embeddings").Select("model_id").Where("entry_id=?", h.full.ID).Scan(&identity).Error)
	require.Equal(t, "fixture-model", identity)
	h.embedder.onImage = nil
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, false))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "completed", h.state(t).Status)
	require.Equal(t, 2, h.embedder.imageCalls)
}

func TestAllEmbeddingRebuildSupersedesPendingAndExpiredOldProfiles(t *testing.T) {
	for _, previous := range []string{"pending", "running"} {
		t.Run(previous, func(t *testing.T) {
			h := newAllRebuildFixture(t, false)
			run, err := h.rebuilder.Start(h.ctx, 1)
			require.NoError(t, err)
			if previous == "running" {
				require.NoError(t, h.db.Model(run).Updates(map[string]any{"status": "running", "lease_until": time.Now().Add(-time.Minute)}).Error)
			}
			configureNameEmbedding(t, h.db, "third-model", 2)
			h.models.models["third-model"] = &allRebuildEmbedder{nameRebuildEmbedder: nameRebuildEmbedder{id: "third-model", dimension: 2}}
			current, err := h.rebuilder.Start(h.ctx, 1)
			require.NoError(t, err)
			require.NotEqual(t, run.ID, current.ID)
			var original types.EmbeddingRebuildRun
			require.NoError(t, h.db.First(&original, "id=?", run.ID).Error)
			require.Equal(t, "failed", original.Status)
		})
	}
}

func TestAllEmbeddingRebuildLeaseRecoveryAndWriteFailureRetry(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	// A killed process leaves pending items, and an expired lease is resumable.
	require.NoError(t, h.db.Model(run).Updates(map[string]any{"status": "running", "attempt_id": "crashed", "lease_until": time.Now().Add(-time.Minute)}).Error)
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, false))
	changed := false
	h.embedder.onEmbed = func(_ context.Context, text string) error {
		if !changed && strings.Contains(text, "Rebuild Entity") {
			changed = true
			require.NoError(t, h.db.Model(&types.Entity{}).Where("id='entity'").Update("description", "Changed while embedding").Error)
		}
		return nil
	}
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.EqualValues(t, 1, h.state(t).FailedCount, "write failure must have a persistent retryable item")
	h.embedder.onEmbed = nil
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, true))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "completed", h.state(t).Status)
}

func TestAllEmbeddingRebuildQueueFailurePreservesPendingCheckpoints(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	h.queue.err = errors.New("task queue unavailable")
	require.ErrorContains(t, h.rebuilder.Enqueue(h.ctx, run), "task queue unavailable")
	require.Equal(t, "failed", h.state(t).Status)
	require.Zero(t, h.state(t).CompletedCount)
	require.EqualValues(t, 7, h.state(t).TotalCount)
	h.queue.err = nil
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, false))
	require.NoError(t, h.rebuilder.Enqueue(h.ctx, run))
	require.NoError(t, h.rebuilder.Handle(h.ctx, h.queue.last(t)))
	require.Equal(t, "completed", h.state(t).Status)
}

func TestAllEmbeddingRebuildModelChangedDuringProviderCallCannotCommit(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	h.embedder.onEmbed = func(context.Context, string) error {
		configureNameEmbedding(t, h.db, "inflight-model", 2)
		return nil
	}
	require.ErrorContains(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil), "embedding profile changed")
	failed := h.state(t)
	require.Equal(t, "failed", failed.Status)
	require.Zero(t, failed.CompletedCount)
	var wrong int64
	require.NoError(t, h.db.Table("article_embeddings").Where("model_id='replacement-model'").Count(&wrong).Error)
	require.Zero(t, wrong)
	h.models.models["inflight-model"] = &allRebuildEmbedder{nameRebuildEmbedder: nameRebuildEmbedder{id: "inflight-model", dimension: 2}}
	_, err = h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
}

func TestAllEmbeddingRebuildRejectsBadVectorAndRetriesOnlyFailures(t *testing.T) {
	h := newAllRebuildFixture(t, false)
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	h.embedder.returnedDimension = 2
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "completed_with_errors", h.state(t).Status)
	require.EqualValues(t, 7, h.state(t).FailedCount)
	require.Zero(t, h.state(t).CompletedCount)
	h.embedder.returnedDimension = 0
	require.NoError(t, h.rebuilder.Continue(h.ctx, 1, run.ID, true))
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "completed", h.state(t).Status)
	require.EqualValues(t, 7, h.state(t).CompletedCount)
}

func TestAllEmbeddingRebuildIncludesImportedPersonalSummaryImagesAndChunks(t *testing.T) {
	h := newAllRebuildFixture(t, true)
	var personal types.Entry
	require.NoError(t, h.db.Where("id='mine'").First(&personal).Error)
	personal.ContentType = "tutorial"
	personal.VaultPath = "mine/personal.md"
	personal.VaultBody = "# Personal procedure\n\n![Personal image](../attachments/mine/personal.png)"
	personal.StructuredSummary = &types.EntrySummary{Overview: "Personal operating procedure", Points: []string{"Retain the complete instructions"}, KeyTerms: []string{"Personal"}}
	personal.Description = personal.StructuredSummary.RenderText()
	require.NoError(t, h.db.Save(&personal).Error)
	dataURL := articleImageDataURI(t, 53)
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, "data:image/png;base64,"))
	require.NoError(t, err)
	path := filepath.Join(h.rebuilder.vault.root, "attachments", personal.ID, "personal.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, data, 0644))
	run, err := h.rebuilder.Start(h.ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 10, run.TotalCount)
	require.NoError(t, h.rebuilder.Run(h.ctx, 1, run.ID, nil))
	require.Equal(t, "completed", h.state(t).Status)
	for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
		var count int64
		require.NoError(t, h.db.Table(table).Where("entry_id=? AND model_id=? AND dimension=3", personal.ID, "replacement-model").Count(&count).Error)
		require.EqualValues(t, 1, count, table)
	}
	require.Equal(t, 2, h.embedder.imageCalls)
	require.Contains(t, strings.Join(h.embedder.texts, "\n"), "Personal operating procedure")
}
