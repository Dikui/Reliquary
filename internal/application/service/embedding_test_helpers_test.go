package service

import (
	"context"
	"errors"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"sync"
	"testing"
	"time"

	taskqueue "reliquary/internal/queue"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type nameRebuildQueue struct {
	mu    sync.Mutex
	tasks []*taskqueue.Task
	err   error
}

func (q *nameRebuildQueue) Enqueue(task *taskqueue.Task, _ ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return nil, q.err
	}
	q.tasks = append(q.tasks, task)
	return &taskqueue.TaskInfo{ID: uuid.NewString()}, nil
}

func (q *nameRebuildQueue) last(t *testing.T) *taskqueue.Task {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	require.NotEmpty(t, q.tasks)
	return q.tasks[len(q.tasks)-1]
}

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
	embedding.Embedder
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
	record := types.Model{
		ID:      id,
		SpaceID: 1,
		Name:    id,
		Type:    types.ModelTypeEmbedding,
		Status:  types.ModelStatusActive,
		Source:  types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			BaseURL:             "https://example.com/v1",
			EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension},
		},
	}
	require.NoError(t, db.Save(&record).Error)
	require.NoError(t, db.Model(&types.Space{}).Where("id=1").Update("embedding_model_id", id).Error)
}

type legacyEntityCandidate struct {
	Name, EntityType, Description string
	Aliases                       []string
}

func entityRecord(id, name, _ string) types.Entity {
	return types.Entity{
		ID:             id,
		SpaceID:        1,
		CanonicalName:  name,
		NormalizedName: types.NormalizeEntityKey(name),
		EntityType:     "organization",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

func entityFixture(t *testing.T) (*gorm.DB, context.Context, *EntityService) {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Entities"}).Error)
	library := types.Library{
		ID:                 "entity-library",
		SpaceID:            1,
		SystemRole:         "official",
		Template:           types.TemplateExternalTimely,
		Directory:          "official",
		SourceEvidenceRule: "link_required",
		Name:               "Entity articles",
		SummaryModelID:     "entity-chat",
		ChunkingConfig:     types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50},
	}
	require.NoError(t, db.Create(&library).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "entity-reviewer")
	return db, ctx, NewEntityService(db, nil)
}

func maintainTestEntities(
	ctx context.Context,
	t *testing.T,
	db *gorm.DB,
	entryID string,
	names ...string,
) []types.Entity {
	t.Helper()
	service := NewEntityService(db, nil)
	entities := []types.Entity{}
	ids := []string{}
	for _, name := range names {
		entity, err := service.SaveEntity(ctx, "", types.EntityInput{CanonicalName: name, EntityType: "organization"})
		require.NoError(t, err)
		entities = append(entities, *entity)
		ids = append(ids, entity.ID)
	}
	if entryID != "" {
		_, err := service.SetEntryEntities(ctx, entryID, ids)
		require.NoError(t, err)
	}
	return entities
}
