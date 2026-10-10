package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type nameRebuildQueue struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	err   error
}

func (q *nameRebuildQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return nil, q.err
	}
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: uuid.NewString()}, nil
}
func (q *nameRebuildQueue) last(t *testing.T) *asynq.Task {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	require.NotEmpty(t, q.tasks)
	return q.tasks[len(q.tasks)-1]
}
func (q *nameRebuildQueue) count() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.tasks) }

type nameRebuildModels struct {
	interfaces.ModelService
	models map[string]embedding.Embedder
	err    error
	onGet  func(context.Context, string)
}

func (m *nameRebuildModels) GetEmbeddingModel(ctx context.Context, id string) (embedding.Embedder, error) {
	if m.onGet != nil {
		m.onGet(ctx, id)
	}
	if m.err != nil {
		return nil, m.err
	}
	value := m.models[id]
	if value == nil {
		return nil, errors.New("test embedding model unavailable")
	}
	return value, nil
}

type nameRebuildEmbedder struct {
	entityEmbeddingInterface
	dimension         int
	id                string
	mu                sync.Mutex
	texts             []string
	batches           int
	returnedDimension int
	onBatch           func(context.Context, []string) error
}

func (e *nameRebuildEmbedder) GetDimensions() int   { return e.dimension }
func (e *nameRebuildEmbedder) GetModelID() string   { return e.id }
func (e *nameRebuildEmbedder) GetModelName() string { return e.id }
func (e *nameRebuildEmbedder) vector() []float32 {
	dim := e.dimension
	if e.returnedDimension > 0 {
		dim = e.returnedDimension
	}
	vector := make([]float32, dim)
	vector[0] = 1
	return vector
}
func (e *nameRebuildEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	e.texts = append(e.texts, text)
	e.mu.Unlock()
	return e.vector(), nil
}
func (e *nameRebuildEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if !types.IsBackgroundTask(ctx) || types.ModelTierFromContext(ctx) != types.ModelTierEmbedding {
		return nil, errors.New("rebuild must use background embedding tier")
	}
	e.mu.Lock()
	e.texts = append(e.texts, texts...)
	e.batches++
	e.mu.Unlock()
	if e.onBatch != nil {
		if err := e.onBatch(ctx, texts); err != nil {
			return nil, err
		}
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = e.vector()
	}
	return vectors, nil
}
func configureNameEmbedding(t *testing.T, db *gorm.DB, id string, dimension int) {
	t.Helper()
	record := types.Model{ID: id, TenantID: 1, Name: id, Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Source: types.ModelSourceRemote, Parameters: types.ModelParameters{BaseURL: "https://example.com/v1", EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension}}}
	require.NoError(t, db.Save(&record).Error)
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("tenant_id=1").Update("model_tiers", types.ModelTierConfig{Embedding: id}).Error)
}
func nameRebuildFixture(t *testing.T) (*gorm.DB, context.Context, *EntityService, *EntityNameEmbeddingRebuilder, *nameRebuildModels, *nameRebuildQueue) {
	t.Helper()
	db, ctx, svc := entityFixture(t)
	models := &nameRebuildModels{models: map[string]embedding.Embedder{}}
	queue := &nameRebuildQueue{}
	rebuilder := NewEntityNameEmbeddingRebuilder(db, models, queue)
	svc.models = models
	svc.nameEmbeddingRebuilder = rebuilder
	return db, ctx, svc, rebuilder, models, queue
}
func readNameEntity(t *testing.T, db *gorm.DB, id string) types.Entity {
	t.Helper()
	var entity types.Entity
	require.NoError(t, db.First(&entity, "id=?", id).Error)
	return entity
}
func pendingNameEntity(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	entity := entityRecord(id, name, "company")
	entity.EmbeddingNeedsRebuild = true
	require.NoError(t, db.Create(&entity).Error)
}

func TestEntityNameEmbeddingRebuildAfterInitialConfiguration(t *testing.T) {
	db, ctx, svc, rebuilder, models, queue := nameRebuildFixture(t)
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	resolved, _, err := svc.resolveEntities(ctx, model, 1, "entity-kb", []entityResolutionInput{{Mention: "Ready Entity", Canonical: "Ready Entity", EntityType: " COMPANY ", Description: "  Identifying detail.  "}})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, resolved); return err }))
	require.True(t, readNameEntity(t, db, resolved[0].ID).EmbeddingNeedsRebuild)
	configureNameEmbedding(t, db, "embedding-two", 2)
	embedder := &nameRebuildEmbedder{dimension: 2, id: "embedding-two"}
	models.models["embedding-two"] = embedder
	require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "knowledge_base_embedding_model_changed"))
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	status, err := rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, status.PendingCount)
	require.Equal(t, "completed", status.LatestRun.Status)
	require.EqualValues(t, 1, status.LatestRun.ProcessedCount)
	stored := readNameEntity(t, db, resolved[0].ID)
	require.False(t, stored.EmbeddingNeedsRebuild)
	require.Len(t, stored.NameEmbedding, 2)
	require.Equal(t, "embedding-two", stored.EmbeddingModelID)
	require.Equal(t, "Ready Entity\ncompany\nIdentifying detail.", embedder.texts[0])
	judge := &entityCountingChat{reply: func(_ context.Context, messages []chat.Message) string {
		return fmt.Sprintf(`{"decision":"match","entity_id":%q,"confidence":0.99}`, stored.ID)
	}}
	_, mentions, err := svc.resolveEntities(ctx, judge, 1, "entity-kb", []entityResolutionInput{{Mention: "完全不同的中文叫法", Canonical: "完全不同的中文叫法", EntityType: "company"}})
	require.NoError(t, err)
	require.Equal(t, stored.ID, mentions["完全不同的中文叫法"].ID, "rebuilt vectors must participate in candidate recall")
}

func TestEntityNameEmbeddingRebuildSwitchesDimensionsAndUsesBatches(t *testing.T) {
	t.Setenv("RELIQUARY_ENTITY_EMBEDDING_BATCH_SIZE", "2")
	db, ctx, _, rebuilder, models, queue := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "old-two", 2)
	old := &nameRebuildEmbedder{dimension: 2, id: "old-two"}
	models.models["old-two"] = old
	for i := 0; i < 3; i++ {
		pendingNameEntity(t, db, fmt.Sprintf("entity-%d", i), fmt.Sprintf("Entity %d", i))
	}
	require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "configuration"))
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	require.Equal(t, 2, old.batches)
	configureNameEmbedding(t, db, "new-three", 3)
	newModel := &nameRebuildEmbedder{dimension: 3, id: "new-three"}
	models.models["new-three"] = newModel
	require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "knowledge_base_embedding_model_changed"))
	before := readNameEntity(t, db, "entity-0")
	require.True(t, before.EmbeddingNeedsRebuild)
	require.Len(t, before.NameEmbedding, 2, "marking preserves prior vector until successful replacement")
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	after := readNameEntity(t, db, "entity-0")
	require.False(t, after.EmbeddingNeedsRebuild)
	require.Equal(t, "new-three", after.EmbeddingModelID)
	require.Equal(t, 3, after.EmbeddingDimension)
	require.Len(t, after.NameEmbedding, 3)
	status, err := rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, status.PendingCount)
	require.EqualValues(t, 3, status.LatestRun.ProcessedCount)
	require.Equal(t, 2, status.LatestRun.ModelCalls)
}

func TestEntityNameEmbeddingRebuildFailuresRemainVisibleAndPending(t *testing.T) {
	for _, failure := range []string{"unconfigured", "unavailable", "wrong_vector_dimension", "inactive"} {
		t.Run(failure, func(t *testing.T) {
			db, ctx, _, rebuilder, models, queue := nameRebuildFixture(t)
			entity := entityRecord("pending", "Pending", "company")
			entity.NameEmbedding = types.EntityNameEmbedding{1, 0}
			entity.EmbeddingModelID = "old"
			entity.EmbeddingDimension = 2
			require.NoError(t, db.Create(&entity).Error)
			if failure != "unconfigured" {
				configureNameEmbedding(t, db, "configured", 2)
				embedder := &nameRebuildEmbedder{dimension: 2, id: "configured"}
				models.models["configured"] = embedder
				if failure == "unavailable" {
					models.err = errors.New("provider unavailable")
				}
				if failure == "wrong_vector_dimension" {
					embedder.returnedDimension = 3
				}
				if failure == "inactive" {
					require.NoError(t, db.Model(&types.Model{}).Where("id=?", "configured").Update("status", types.ModelStatusDownloading).Error)
				}
			}
			require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "configuration"))
			require.Equal(t, 1, queue.count())
			require.True(t, readNameEntity(t, db, entity.ID).EmbeddingNeedsRebuild)
			require.Error(t, rebuilder.Handle(ctx, queue.last(t)))
			status, err := rebuilder.Status(ctx, 1)
			require.NoError(t, err)
			require.EqualValues(t, 1, status.PendingCount)
			require.Equal(t, "failed", status.LatestRun.Status)
			require.NotEmpty(t, status.LatestRun.Error)
			stored := readNameEntity(t, db, entity.ID)
			require.True(t, stored.EmbeddingNeedsRebuild)
			require.Equal(t, "old", stored.EmbeddingModelID)
			require.Len(t, stored.NameEmbedding, 2)
		})
	}
}

func TestEntityNameResolutionDoesNotMarkWholeRegistry(t *testing.T) {
	db, ctx, svc, _, models, _ := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "configured", 2)
	models.models["configured"] = &nameRebuildEmbedder{dimension: 2, id: "configured"}
	entity := entityRecord("old", "Unrelated Old", "company")
	entity.NameEmbedding = types.EntityNameEmbedding{1, 0, 0}
	entity.EmbeddingModelID = "old-model"
	entity.EmbeddingDimension = 3
	require.NoError(t, db.Create(&entity).Error)
	updates := 0
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("count_registry_updates", func(tx *gorm.DB) {
		if tx.Statement.Table == "entities" {
			updates++
		}
	}))
	defer db.Callback().Update().Remove("count_registry_updates")
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	_, _, err := svc.resolveEntities(ctx, model, 1, "entity-kb", []entityResolutionInput{{Mention: "A New Name", Canonical: "A New Name", EntityType: "company"}})
	require.NoError(t, err)
	require.Zero(t, updates)
	require.False(t, readNameEntity(t, db, entity.ID).EmbeddingNeedsRebuild)
}

func TestEntityNameDescriptionChangesAndMergesEnqueueRebuilds(t *testing.T) {
	db, ctx, svc, rebuilder, models, queue := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "configured", 2)
	embedder := &nameRebuildEmbedder{dimension: 2, id: "configured"}
	models.models["configured"] = embedder
	pendingNameEntity(t, db, "keep", "Keep")
	pendingNameEntity(t, db, "merge", "Merge")
	require.NoError(t, rebuilder.Enqueue(ctx, 1, "initial"))
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	entity, err := svc.UpdateEntityDescription(ctx, "keep", "  Updated description.  ")
	require.NoError(t, err)
	require.True(t, entity.EmbeddingNeedsRebuild)
	require.Equal(t, "Updated description.", entity.Description)
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	require.False(t, readNameEntity(t, db, "keep").EmbeddingNeedsRebuild)
	require.Contains(t, embedder.texts, "Keep\ncompany\nUpdated description.")
	require.NoError(t, svc.MergeEntities(ctx, "keep", "merge"))
	require.True(t, readNameEntity(t, db, "keep").EmbeddingNeedsRebuild)
	require.Equal(t, "keep", readNameEntity(t, db, "merge").MergedIntoID)
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	status, err := rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, status.PendingCount)
	require.EqualValues(t, 1, status.LatestRun.ProcessedCount, "merged tombstone is excluded")
	entity, err = svc.UpdateEntityDescription(ctx, "keep", "")
	require.NoError(t, err)
	require.Empty(t, entity.Description)
	require.True(t, entity.EmbeddingNeedsRebuild)
}

func TestEntityNameRebuildDiscardsStaleDescriptionAndProfileResults(t *testing.T) {
	for _, change := range []string{"description", "profile", "merge", "delete"} {
		t.Run(change, func(t *testing.T) {
			db, ctx, svc, rebuilder, models, queue := nameRebuildFixture(t)
			configureNameEmbedding(t, db, "configured", 2)
			pendingNameEntity(t, db, "target", "Target")
			pendingNameEntity(t, db, "keep", "Keep")
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			embedder := &nameRebuildEmbedder{dimension: 2, id: "configured", onBatch: func(_ context.Context, _ []string) error { once.Do(func() { close(entered); <-release }); return nil }}
			models.models["configured"] = embedder
			require.NoError(t, rebuilder.Enqueue(ctx, 1, "concurrent"))
			task := queue.last(t)
			done := make(chan error, 1)
			go func() { done <- rebuilder.Handle(ctx, task) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("embedding did not begin")
			}
			switch change {
			case "description":
				_, err := svc.UpdateEntityDescription(ctx, "target", "Changed during embedding.")
				require.NoError(t, err)
			case "profile":
				var record types.Model
				require.NoError(t, db.First(&record, "id=?", "configured").Error)
				record.Parameters.BaseURL = "https://new.example.com/v1"
				require.NoError(t, db.Save(&record).Error)
				require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "embedding_model_updated"))
			case "merge":
				require.NoError(t, svc.MergeEntities(ctx, "keep", "target"))
			case "delete":
				require.NoError(t, db.Where("id=?", "target").Delete(&types.Entity{}).Error)
			}
			close(release)
			err := <-done
			if change == "profile" {
				require.ErrorContains(t, err, "configuration changed")
			} else {
				require.NoError(t, err)
			}
			if change == "delete" {
				var count int64
				require.NoError(t, db.Model(&types.Entity{}).Where("id=?", "target").Count(&count).Error)
				require.Zero(t, count)
				return
			}
			stored := readNameEntity(t, db, "target")
			require.Empty(t, stored.NameEmbedding, "old in-flight result must not overwrite changed identity")
			require.True(t, stored.EmbeddingNeedsRebuild)
			if change == "description" {
				require.Equal(t, "Changed during embedding.", stored.Description)
			}
			if change == "merge" {
				require.Equal(t, "keep", stored.MergedIntoID)
			}
		})
	}
}

func TestEntityNameCreationDetectsProfileChangesBeforePersistence(t *testing.T) {
	db, ctx, svc, rebuilder, models, _ := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "configured", 2)
	models.models["configured"] = &nameRebuildEmbedder{dimension: 2, id: "configured"}
	model := &entityCountingChat{reply: func(context.Context, []chat.Message) string { return `{"decision":"new","confidence":1}` }}
	resolved, _, err := svc.resolveEntities(ctx, model, 1, "entity-kb", []entityResolutionInput{{Mention: "Future", Canonical: " Future ", EntityType: " COMPANY ", Description: " Detail "}})
	require.NoError(t, err)
	var metadata types.Model
	require.NoError(t, db.First(&metadata, "id=?", "configured").Error)
	metadata.Parameters.BaseURL = "https://new.example.com/v1"
	require.NoError(t, db.Save(&metadata).Error)
	require.NoError(t, rebuilder.MarkAndEnqueue(ctx, 1, "embedding_model_updated"))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistEntities(tx, resolved); return err }))
	stored := readNameEntity(t, db, resolved[0].ID)
	require.True(t, stored.EmbeddingNeedsRebuild)
	require.Len(t, stored.NameEmbedding, 2)
}

func TestEntityNameStartupDetectsSemanticProfileChangesWithoutCredentialsNoise(t *testing.T) {
	db, ctx, _, rebuilder, models, queue := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "configured", 2)
	models.models["configured"] = &nameRebuildEmbedder{dimension: 2, id: "configured"}
	entity := entityRecord("ready", "Ready", "company")
	entity.NameEmbedding = types.EntityNameEmbedding{1, 0}
	entity.EmbeddingModelID = "configured"
	entity.EmbeddingDimension = 2
	require.NoError(t, db.Create(&entity).Error)
	require.NoError(t, rebuilder.Startup(ctx))
	require.Equal(t, 1, queue.count(), "first startup establishes a rebuilt profile baseline")
	require.NoError(t, rebuilder.Handle(ctx, queue.last(t)))
	var record types.Model
	require.NoError(t, db.First(&record, "id=?", "configured").Error)
	record.Parameters.APIKey = "rotated key"
	require.NoError(t, db.Save(&record).Error)
	require.NoError(t, rebuilder.Startup(ctx))
	require.Equal(t, 1, queue.count(), "credentials do not change the embedding space")
	record.Parameters.BaseURL = "https://new.example.com/v1"
	require.NoError(t, db.Save(&record).Error)
	require.NoError(t, rebuilder.Startup(ctx))
	require.Equal(t, 2, queue.count())
	require.True(t, readNameEntity(t, db, entity.ID).EmbeddingNeedsRebuild)
}

func TestEntityNameProfileRejectsProviderConstructionRace(t *testing.T) {
	db, ctx, _, rebuilder, models, _ := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "configured", 2)
	models.models["configured"] = &nameRebuildEmbedder{dimension: 2, id: "configured"}
	models.onGet = func(_ context.Context, _ string) {
		var record types.Model
		require.NoError(t, db.First(&record, "id=?", "configured").Error)
		record.Parameters.BaseURL = "https://changed.example.com/v1"
		require.NoError(t, db.Save(&record).Error)
	}
	_, err := rebuilder.profile(ctx, 1)
	require.ErrorContains(t, err, "changed while constructing")
}

func TestEntityNameRebuildPartialFailuresPreserveProcessedProgress(t *testing.T) {
	t.Setenv("RELIQUARY_ENTITY_EMBEDDING_BATCH_SIZE", "1")
	db, ctx, _, rebuilder, models, queue := nameRebuildFixture(t)
	configureNameEmbedding(t, db, "configured", 2)
	pendingNameEntity(t, db, "a", "Alpha")
	pendingNameEntity(t, db, "b", "Beta")
	embedder := &nameRebuildEmbedder{dimension: 2, id: "configured", onBatch: func(_ context.Context, texts []string) error {
		if strings.HasPrefix(texts[0], "Beta") {
			return errors.New("batch unavailable")
		}
		return nil
	}}
	models.models["configured"] = embedder
	require.NoError(t, rebuilder.Enqueue(ctx, 1, "manual"))
	require.Error(t, rebuilder.Handle(ctx, queue.last(t)))
	status, err := rebuilder.Status(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "failed", status.LatestRun.Status)
	require.EqualValues(t, 1, status.LatestRun.ProcessedCount)
	require.EqualValues(t, 2, status.LatestRun.TotalCount)
	require.EqualValues(t, 1, status.PendingCount)
	require.False(t, readNameEntity(t, db, "a").EmbeddingNeedsRebuild)
	require.True(t, readNameEntity(t, db, "b").EmbeddingNeedsRebuild)
}
