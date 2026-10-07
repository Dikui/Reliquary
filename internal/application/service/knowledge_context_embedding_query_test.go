package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type contextEmbeddingQueryTrace struct {
	gormlogger.Interface
	mu      sync.Mutex
	queries []string
}

func (l *contextEmbeddingQueryTrace) Trace(ctx context.Context, begin time.Time, statement func() (string, int64), err error) {
	sql, rows := statement()
	l.mu.Lock()
	l.queries = append(l.queries, sql)
	l.mu.Unlock()
	l.Interface.Trace(ctx, begin, func() (string, int64) { return sql, rows }, err)
}

func (l *contextEmbeddingQueryTrace) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.queries...)
}

type contextEmbeddingQueryModels struct {
	interfaces.ModelService
	chat     chat.Chat
	embedder embedding.Embedder
}

func (m contextEmbeddingQueryModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return m.chat, nil
}
func (m contextEmbeddingQueryModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.embedder, nil
}

func TestP3MentionParsingDoesNotMarkHistoricalEmbeddingRegistry(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Create(&types.Model{ID: "p3-embedding", TenantID: 1, Name: "Name vector", Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3}}}).Error)
	require.NoError(t, db.Model(&types.Tenant{}).Where("id = ?", 1).Update("model_tiers", types.ModelTierConfig{Embedding: "p3-embedding"}).Error)
	stale := make([]types.KnowledgeEntity, 50)
	for i := range stale {
		stale[i] = p3Entity(fmt.Sprintf("historical-vector-%02d", i), fmt.Sprintf("Historical registry object %02d", i), "model")
		stale[i].EmbeddingModelID = "p3-embedding"
		stale[i].EmbeddingDimension = 3
		stale[i].NameEmbedding = types.ContextNameEmbedding{0, 1, 0}
		switch i % 4 {
		case 0:
			stale[i].EmbeddingModelID = "previous-vector-model"
		case 1:
			stale[i].EmbeddingDimension = 2
			stale[i].NameEmbedding = types.ContextNameEmbedding{0, 1}
		case 2:
			stale[i].NameEmbedding = nil
		case 3:
			stale[i].NameEmbedding = types.ContextNameEmbedding{0, 1}
		}
	}
	require.NoError(t, db.Create(&stale).Error)
	var before []types.KnowledgeEntity
	require.NoError(t, db.Where("id LIKE ?", "historical-vector-%").Order("id").Find(&before).Error)
	for _, entity := range before {
		require.False(t, entity.EmbeddingNeedsRebuild)
	}
	current := p3Entity("query-current", "Current Target", "model")
	current.EmbeddingModelID, current.EmbeddingDimension = "p3-embedding", 3
	current.NameEmbedding = types.ContextNameEmbedding{1, 0, 0}
	require.NoError(t, db.Create(&current).Error)
	const newMention = "不同的中文称呼"
	body := "Current Target also appears as " + newMention + "."
	k := p3Message(t, db, "query-budget-message", body)
	extraction, err := json.Marshal(contextExtraction{Entities: []contextExtractedEntity{
		{Mention: current.CanonicalName, Canonical: current.CanonicalName, EntityType: "model", Quote: current.CanonicalName},
		{Mention: newMention, Canonical: newMention, EntityType: "model", Quote: newMention},
	}})
	require.NoError(t, err)
	judgment := &p3CountingChat{reply: func(_ context.Context, messages []chat.Message) string {
		if !strings.Contains(messages[0].Content, "same real-world object") {
			return string(extraction)
		}
		var payload struct {
			Mention    string                  `json:"mention"`
			Candidates []types.KnowledgeEntity `json:"candidates"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
		require.Equal(t, newMention, payload.Mention)
		require.Len(t, payload.Candidates, 1, "old model, dimensions and missing vectors do not enter vector recall")
		require.Equal(t, current.ID, payload.Candidates[0].ID)
		return `{"decision":"match","entity_id":"query-current","confidence":0.99}`
	}}
	embedder := &p3Embedder{dimension: 3}
	svc.models = contextEmbeddingQueryModels{chat: judgment, embedder: embedder}
	trace := &contextEmbeddingQueryTrace{Interface: db.Logger}
	svc.db = db.Session(&gorm.Session{Logger: trace})
	require.NoError(t, svc.ProcessMessage(ctx, k, body))
	require.Equal(t, 1, judgment.count("extraction"))
	require.Equal(t, 1, judgment.count("entity"), "only the one non-exact input mention needs model judgment")
	require.Equal(t, 1, embedder.calls)
	var after []types.KnowledgeEntity
	require.NoError(t, db.Where("id LIKE ?", "historical-vector-%").Order("id").Find(&after).Error)
	require.Equal(t, before, after, "parsing leaves historical names, vector profiles, flags and timestamps untouched")
	idPredicate := regexp.MustCompile(`\bid\s*(?:=|\bin\s*\()`)
	flagUpdates := 0
	for _, statement := range trace.snapshot() {
		normalized := strings.ToLower(strings.ReplaceAll(statement, `"`, ""))
		if !strings.HasPrefix(strings.TrimSpace(normalized), "update knowledge_context_entities ") || !strings.Contains(normalized, "embedding_needs_rebuild") {
			continue
		}
		flagUpdates++
		_, where, hasWhere := strings.Cut(normalized, " where ")
		require.True(t, hasWhere, "entity flag updates require explicit object IDs: %s", statement)
		require.True(t, idPredicate.MatchString(where), "parsing must not perform a registry-wide embedding flag update: %s", statement)
		require.Contains(t, where, current.ID, "only an object referenced by this input may be updated: %s", statement)
	}
	t.Logf("historical_objects=%d input_mentions=2 exact=1 entity_judgments=%d mention_embeddings=%d object_scoped_flag_updates=%d", len(after), judgment.count("entity"), embedder.calls, flagUpdates)
}
