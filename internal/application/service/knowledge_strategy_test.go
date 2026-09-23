package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type strategyKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s strategyKB) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type strategyOriginal struct{ interfaces.KnowledgeService }

func (strategyOriginal) GetKnowledgeFile(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("A sourced report with several observations and evidence.")), "report.md", nil
}

type strategyChat struct{}

func (strategyChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	system := messages[0].Content
	switch {
	case strings.HasPrefix(system, "Draft only"):
		return &types.ChatResponse{Content: `{"filter":"Prefer detailed evidence","tags":"Prefer specific object names","collection":"Prefer primary reports","research":"Search primary reports","reason":"User requested more precise screening and labels"}`}, nil
	case strings.HasPrefix(system, "Review this sourced"):
		if strings.Contains(system, "Prefer detailed evidence") || strings.Contains(system, "Prefer primary reports") {
			return &types.ChatResponse{Content: `{"approved":false,"reason":"Insufficient detail"}`}, nil
		}
		return &types.ChatResponse{Content: `{"approved":true,"reason":"Has source evidence"}`}, nil
	case strings.HasPrefix(system, "Rewrite one web search"):
		if strings.Contains(messages[1].Content, "Search primary reports") {
			return &types.ChatResponse{Content: `{"query":"Report primary source"}`}, nil
		}
		return &types.ChatResponse{Content: `{"query":"Report"}`}, nil
	case strings.HasPrefix(system, "Classify one sourced"):
		name := "Broad topic"
		if strings.Contains(system, "Prefer specific object names") {
			name = "Specific object"
		}
		value, _ := json.Marshal(map[string]any{"natures": []string{"描述"}, "referents": []map[string]string{{"kind": "object", "name": name}}})
		return &types.ChatResponse{Content: string(value)}, nil
	}
	return nil, errors.New("unexpected model call")
}

func (strategyChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not used")
}
func (strategyChat) GetModelName() string { return "strategy" }
func (strategyChat) GetModelID() string   { return "strategy" }

func TestKnowledgeStrategyCandidateTrialAndRestore(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id INTEGER, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id,tenant_id) VALUES ('kb',1)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE knowledges (id TEXT PRIMARY KEY, tenant_id INTEGER, knowledge_base_id TEXT, title TEXT, parse_status TEXT, metadata TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&types.KnowledgeStrategyVersion{}))
	message := &types.Knowledge{ID: "sample", TenantID: 1, KnowledgeBaseID: "kb", Title: "Report", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, message.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{Publisher: "Publisher", OriginalURL: "https://example.org/report", PublishedAt: "2026-09-01"}}))
	require.NoError(t, db.Exec(`INSERT INTO knowledges (id,tenant_id,knowledge_base_id,title,parse_status,metadata) VALUES (?,?,?,?,?,?)`, message.ID, message.TenantID, message.KnowledgeBaseID, message.Title, message.ParseStatus, string(message.Metadata)).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "owner", SummaryModelID: "model"}
	svc := NewKnowledgeStrategyService(db, strategyKB{kb: kb}, strategyOriginal{}, intakeModelService{chat: strategyChat{}}, nil)
	owner := anomalyContext("owner")
	version, err := svc.Propose(owner, "kb", "Tighten evidence screening and use specific names", []string{"sample"})
	require.NoError(t, err)
	require.Equal(t, "candidate", version.Status)
	require.Contains(t, string(version.SampleResults), "Insufficient detail")
	var unchanged string
	require.NoError(t, db.Raw(`SELECT metadata FROM knowledges WHERE id = 'sample'`).Scan(&unchanged).Error)
	require.JSONEq(t, string(message.Metadata), unchanged)
	active, err := activeKnowledgeStrategyRules(owner, db, 1, "kb")
	require.NoError(t, err)
	require.Nil(t, active)
	_, err = svc.Activate(anomalyContext("reviewer"), "kb", version.ID)
	require.Error(t, err)
	_, err = svc.Activate(owner, "kb", version.ID)
	require.NoError(t, err)
	active, err = activeKnowledgeStrategyRules(owner, db, 1, "kb")
	require.NoError(t, err)
	require.Equal(t, "Prefer detailed evidence", active.Filter)
	targets := types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb", TenantID: 1}}
	researchPolicy, err := activeResearchPolicyForTargets(owner, db, targets)
	require.NoError(t, err)
	require.Contains(t, researchPolicy, "Search primary reports")
	pending := &types.Knowledge{ID: "next", TenantID: 1, KnowledgeBaseID: "kb", Title: "New report"}
	require.NoError(t, pending.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{Publisher: "Publisher", OriginalURL: "https://example.org/next", PublishedAt: "2026-09-02"}}))
	repo := &intakeRepo{knowledge: pending}
	intakeService := &knowledgeService{db: db, repo: repo, modelService: intakeModelService{chat: strategyChat{}}}
	rejected, err := intakeService.reviewIntake(owner, kb, pending, "New original body with evidence")
	require.NoError(t, err)
	require.True(t, rejected, "the active filter must affect a subsequent real intake")
	tagRepo := &autoTagTagRepo{}
	messageRepo := &autoTagKnowledgeRepo{}
	tagger := &KnowledgeAutoTagService{db: db, knowledgeRepo: messageRepo, tagRepo: tagRepo,
		chunkService: &autoTagChunkService{chunks: []*types.Chunk{{ChunkType: types.ChunkTypeText, Content: "Evidence about a specific object"}}},
		modelService: intakeModelService{chat: strategyChat{}}}
	require.NoError(t, tagger.tagMessage(owner, kb, &types.Knowledge{ID: "future", FileName: "future.md"}))
	require.Contains(t, []string{tagRepo.tags[0].Name, tagRepo.tags[1].Name}, "Specific object",
		"the active tag rule must affect a subsequent real tagging task")
	collectionOnly := &types.KnowledgeStrategyVersion{ID: "collection", TenantID: 1, KnowledgeBaseID: "kb", Status: "archived",
		Rules:         types.JSON(`{"filter":"","tags":"","collection":"Prefer primary reports","research":""}`),
		PreviousRules: types.JSON(`{}`), SampleResults: types.JSON(`[]`), Reason: "Earlier reviewed collection rule", CreatedBy: "owner"}
	require.NoError(t, db.Create(collectionOnly).Error)
	_, err = svc.Activate(owner, "kb", collectionOnly.ID)
	require.NoError(t, err)
	for _, channel := range []string{types.ChannelRSS, types.ChannelWeb} {
		candidate := &types.Knowledge{ID: channel, TenantID: 1, KnowledgeBaseID: "kb", Channel: channel}
		require.NoError(t, candidate.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{Publisher: "Publisher", OriginalURL: "https://example.org/article", PublishedAt: "2026-09-02"}}))
		itemRepo := &intakeRepo{knowledge: candidate}
		processor := &knowledgeService{db: db, repo: itemRepo, modelService: intakeModelService{chat: strategyChat{}}}
		blocked, reviewErr := processor.reviewIntake(owner, kb, candidate, "A sourced original report")
		require.NoError(t, reviewErr)
		require.Equal(t, channel == types.ChannelRSS, blocked, "collection policy applies to daily source intake")
	}
	_, err = svc.Activate(owner, "kb", version.ID)
	require.NoError(t, err)

	legacy := &types.KnowledgeStrategyVersion{ID: "legacy", TenantID: 1, KnowledgeBaseID: "kb", Status: "archived", Rules: types.JSON(`{"filter":"Original policy","tags":"Original labels"}`), PreviousRules: types.JSON(`{}`), SampleResults: types.JSON(`[]`), Reason: "Earlier reviewed version", CreatedBy: "owner"}
	require.NoError(t, db.Create(legacy).Error)
	_, err = svc.Activate(owner, "kb", legacy.ID)
	require.NoError(t, err)
	active, err = activeKnowledgeStrategyRules(owner, db, 1, "kb")
	require.NoError(t, err)
	require.Equal(t, "Original policy", active.Filter)
	researchPolicy, err = activeResearchPolicyForTargets(owner, db, targets)
	require.NoError(t, err)
	require.Empty(t, researchPolicy)
	_, err = svc.Activate(owner, "kb", version.ID)
	require.NoError(t, err)
	active, err = activeKnowledgeStrategyRules(owner, db, 1, "kb")
	require.NoError(t, err)
	require.Equal(t, "Prefer detailed evidence", active.Filter)
}
