package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func TestResolveContextEventsRelationClasses(t *testing.T) {
	tests := []struct {
		name             string
		response         string
		wantRelation     string
		wantExistingID   string
		wantRelatedEvent string
	}{
		{
			name:         "same occurrence merges",
			response:     `{"relation":"SAME_OCCURRENCE","event_id":"old-event","confidence":0.99}`,
			wantRelation: types.KnowledgeEventRelationSameOccurrence, wantExistingID: "old-event", wantRelatedEvent: "old-event",
		},
		{
			name:         "same story stays separate and points back",
			response:     `{"relation":"SAME_STORY","event_id":"old-event","confidence":0.99}`,
			wantRelation: types.KnowledgeEventRelationSameStory, wantRelatedEvent: "old-event",
		},
		{
			name:         "unrelated stays independent",
			response:     `{"relation":"UNRELATED","event_id":"","confidence":0.99}`,
			wantRelation: types.KnowledgeEventRelationUnrelated,
		},
		{
			name:         "missing relation cannot merge",
			response:     `{"event_id":"old-event","confidence":0.99}`,
			wantRelation: types.KnowledgeEventRelationUnrelated,
		},
		{
			name:         "low confidence cannot merge",
			response:     `{"relation":"SAME_OCCURRENCE","event_id":"old-event","confidence":0.89}`,
			wantRelation: types.KnowledgeEventRelationUnrelated,
		},
		{
			name:         "unlisted target is ignored",
			response:     `{"relation":"SAME_STORY","event_id":"not-listed","confidence":0.99}`,
			wantRelation: types.KnowledgeEventRelationUnrelated,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := knowledgeContextTestDB(t)
			oldTime := time.Now().UTC().Add(-220 * 24 * time.Hour)
			require.NoError(t, db.Create(&[]types.KnowledgeEntity{
				{ID: "acme", TenantID: 9, CanonicalName: "Acme", NormalizedName: "acme"},
				{ID: "other", TenantID: 9, CanonicalName: "Other", NormalizedName: "other"},
			}).Error)
			oldEvents := []types.KnowledgeEvent{
				{ID: "old-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Launch announced", EntityIDs: types.StringArray{"acme"}, OccurredAt: &oldTime, CreatedAt: oldTime, UpdatedAt: oldTime},
				{ID: "undated-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Earlier change without a date", EntityIDs: types.StringArray{"acme"}, CreatedAt: oldTime, UpdatedAt: oldTime},
				{ID: "other-entity-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Different object", EntityIDs: types.StringArray{"other"}, CreatedAt: oldTime, UpdatedAt: oldTime},
			}
			require.NoError(t, db.Create(&oldEvents).Error)
			seedContextEventEntities(t, db, oldEvents...)

			var observedCandidateIDs []string
			model := &knowledgeContextTestChat{reply: func(messages []chat.Message) string {
				var payload struct {
					Candidates []struct {
						ID string `json:"id"`
					} `json:"candidates"`
				}
				_ = json.Unmarshal([]byte(messages[1].Content), &payload)
				for _, candidate := range payload.Candidates {
					observedCandidateIDs = append(observedCandidateIDs, candidate.ID)
				}
				return test.response
			}}
			service := NewKnowledgeContextService(db, nil)
			source := contextExtractedEvent{
				Title: "Availability update", EventType: "availability", Description: "The product is now available",
				EntityMentions: []string{"Acme"}, Quote: "Acme made it available.",
			}
			resolved, err := service.resolveContextEvents(context.Background(), model, 9, "kb", []contextExtractedEvent{source}, map[string]types.KnowledgeEntity{"acme": {ID: "acme"}})
			require.NoError(t, err)
			require.Len(t, resolved, 1)
			require.Equal(t, test.wantRelation, resolved[0].Relation)
			if test.wantExistingID != "" {
				require.Equal(t, test.wantExistingID, resolved[0].Event.ID)
			} else {
				require.NotEqual(t, "old-event", resolved[0].Event.ID)
			}
			require.Equal(t, test.wantRelatedEvent, resolved[0].RelatedEventID)
			require.Equal(t, 1, model.calls)
			require.ElementsMatch(t, []string{"old-event", "undated-event"}, observedCandidateIDs,
				"entity overlap narrows candidates while old and undated events remain eligible")

			if test.wantExistingID == "" {
				require.NoError(t, db.Create(&resolved[0].Event).Error)
			}
			evidence := types.KnowledgeEvidence{ID: "source-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "source-message",
				Version: 1, BodyHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Body: source.Quote,
				Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: time.Now()}
			require.NoError(t, db.Create(&evidence).Error)
			require.NoError(t, attachContextEventEvidence(db, 9, "kb", &resolved[0].Event, &evidence, source,
				resolved[0].Relation, resolved[0].RelatedEventID))
			var stored types.KnowledgeEventEvidence
			require.NoError(t, db.Where("event_id = ? AND evidence_id = ?", resolved[0].Event.ID, evidence.ID).First(&stored).Error)
			require.Equal(t, test.wantRelation, stored.Relation)
			require.Equal(t, test.wantRelatedEvent, stored.RelatedEventID)
		})
	}
}

func TestMatchContextEventUsesOneBoundedCandidateCall(t *testing.T) {
	candidates := make([]types.KnowledgeEvent, 51)
	for i := range candidates {
		candidates[i] = types.KnowledgeEvent{ID: fmt.Sprintf("event-%02d", i), EntityIDs: types.StringArray{"acme"}}
	}
	model := &knowledgeContextTestChat{reply: func(messages []chat.Message) string {
		var input struct {
			Candidates []struct {
				ID string `json:"id"`
			} `json:"candidates"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		require.Len(t, input.Candidates, 10)
		return `{"relation":"SAME_STORY","event_id":"event-09","confidence":0.98}`
	}}
	service := NewKnowledgeContextService(nil, nil)
	resolved, err := service.matchContextEvent(context.Background(), model, contextExtractedEvent{Title: "Change"}, types.StringArray{"acme"}, candidates)
	require.NoError(t, err)
	require.Equal(t, types.KnowledgeEventRelationSameStory, resolved.Relation)
	require.Equal(t, "event-09", resolved.Event.ID)
	require.Equal(t, 1, model.calls)
}

func TestProcessRoundupKeepsFactsAndSkipsEventLinks(t *testing.T) {
	db := knowledgeContextTestDB(t)
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_bases ADD COLUMN summary_model_id TEXT NOT NULL DEFAULT ''`).Error)
	require.NoError(t, db.Exec(`UPDATE knowledge_bases SET summary_model_id = 'summary' WHERE id = 'kb'`).Error)
	body := "Acme launched Product X. Product Y will ship next month."
	k := types.Knowledge{ID: "roundup-message", TenantID: 9, KnowledgeBaseID: "kb", Title: "Weekly roundup",
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Metadata: types.JSON(`{"intake":{"decision":"approved"}}`)}
	require.NoError(t, db.Create(&k).Error)
	model := &knowledgeContextTestChat{response: `{"source_kind":"roundup","entities":[{"mention":"Acme","quote":"Acme launched Product X.","canonical_name":"Acme","entity_type":"organization","description":"Company","aliases":[]}],"facts":[{"kind":"attribute","subject":"Acme","predicate":"launch_status","value":"announced","value_type":"string","unit":"","object":"","effective_from":"","effective_to":"","quote":"Acme launched Product X."}],"events":[{"title":"Acme launched Product X","event_type":"launch","description":"Product X launched","occurred_at":"","entity_mentions":["Acme"],"quote":"Acme launched Product X."}],"attention":{"novelty":20,"impact":10,"credibility":50,"relevance":20,"actionability":10,"score":18,"reason":"A roundup includes product notes."}}`}
	service := NewKnowledgeContextService(db, knowledgeContextTestModelService{model: model})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	require.NoError(t, service.ProcessMessage(ctx, &k, body))
	result, err := service.Query(ctx, "kb", types.KnowledgeContextQuery{KnowledgeIDs: []string{k.ID}})
	require.NoError(t, err)
	require.Len(t, result.Facts, 1, "roundup sources retain their supported object facts")
	require.Empty(t, result.Events, "roundup source content must not attach or fan out events")
	require.Len(t, result.Evidence, 1)
	require.Equal(t, types.KnowledgeEvidenceSourceKindRoundup, result.Evidence[0].SourceKind)
	var linkCount int64
	require.NoError(t, db.Model(&types.KnowledgeEventEvidence{}).Where("evidence_id = ?", result.Evidence[0].ID).Count(&linkCount).Error)
	require.Zero(t, linkCount)
}

func TestProjectVisibleContextEventRelationsChecksBothEvidenceEnds(t *testing.T) {
	db := knowledgeContextTestDB(t)
	now := time.Now().UTC()
	messages := []types.Knowledge{
		{ID: "followup-source", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"intake":{"decision":"approved"}}`)},
		{ID: "visible-target-source", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"intake":{"decision":"approved"}}`)},
		{ID: "denied-target-source", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", Metadata: types.JSON(`{"intake":{"decision":"rejected"}}`)},
	}
	require.NoError(t, db.Create(&messages).Error)
	evidence := []types.KnowledgeEvidence{
		{ID: "followup-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "followup-source", Version: 1, BodyHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Body: "follow-up", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: now},
		{ID: "visible-target-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "visible-target-source", Version: 1, BodyHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Body: "old event", Title: "Old source", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: now},
		{ID: "denied-target-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: "denied-target-source", Version: 1, BodyHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Body: "private event", Title: "PRIVATE TARGET TITLE", Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, CreatedAt: now},
	}
	require.NoError(t, db.Create(&evidence).Error)
	events := []types.KnowledgeEvent{
		{ID: "followup-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Availability", EvidenceIDs: types.StringArray{"followup-evidence"}, CreatedAt: now, UpdatedAt: now},
		{ID: "denied-followup-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Private follow-up", EvidenceIDs: types.StringArray{"followup-evidence"}, CreatedAt: now, UpdatedAt: now},
		{ID: "unrelated-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Independent event", EvidenceIDs: types.StringArray{"followup-evidence"}, CreatedAt: now, UpdatedAt: now},
		{ID: "visible-target-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "Aggregate title must not be used", CreatedAt: now, UpdatedAt: now},
		{ID: "denied-target-event", TenantID: 9, KnowledgeBaseID: "kb", Title: "PRIVATE AGGREGATE TITLE", CreatedAt: now, UpdatedAt: now},
	}
	require.NoError(t, db.Create(&events).Error)
	links := []types.KnowledgeEventEvidence{
		{ID: "followup-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: "followup-event", EvidenceID: "followup-evidence", Relation: types.KnowledgeEventRelationSameStory, RelatedEventID: "visible-target-event", EntityIDs: types.StringArray{}, CreatedAt: now},
		{ID: "denied-followup-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: "denied-followup-event", EvidenceID: "followup-evidence", Relation: types.KnowledgeEventRelationSameStory, RelatedEventID: "denied-target-event", EntityIDs: types.StringArray{}, CreatedAt: now},
		{ID: "unrelated-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: "unrelated-event", EvidenceID: "followup-evidence", Relation: types.KnowledgeEventRelationUnrelated, EntityIDs: types.StringArray{}, CreatedAt: now},
		{ID: "visible-target-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: "visible-target-event", EvidenceID: "visible-target-evidence", SourceTitle: "Earlier launch report", CreatedAt: now},
		{ID: "denied-target-link", TenantID: 9, KnowledgeBaseID: "kb", EventID: "denied-target-event", EvidenceID: "denied-target-evidence", SourceTitle: "PRIVATE SOURCE SUMMARY", CreatedAt: now},
	}
	require.NoError(t, db.Create(&links).Error)

	service := NewKnowledgeContextService(db, nil)
	visibleSourceEvidence := map[string]types.KnowledgeEvidence{"followup-evidence": evidence[0]}
	query := types.KnowledgeContextQuery{KnowledgeIDs: []string{"followup-source", "visible-target-source", "denied-target-source"}}
	require.NoError(t, service.projectVisibleEventRelations(context.Background(), 9, []string{"kb"}, events[:3], visibleSourceEvidence, query))
	require.Len(t, events[0].Relations, 1)
	require.Equal(t, "visible-target-event", events[0].Relations[0].RelatedEventID)
	require.Equal(t, "Earlier launch report", events[0].Relations[0].RelatedTitle)
	require.Equal(t, "followup-evidence", events[0].Relations[0].EvidenceID)
	require.Empty(t, events[1].Relations, "relations to an unapproved source are hidden")
	require.Len(t, events[2].Relations, 1)
	require.Equal(t, types.KnowledgeEventRelationUnrelated, events[2].Relations[0].Relation)
	require.Empty(t, events[2].Relations[0].RelatedEventID, "unrelated classification has no graph target")

	query.KnowledgeIDs = []string{"followup-source"}
	require.NoError(t, service.projectVisibleEventRelations(context.Background(), 9, []string{"kb"}, events[:3], visibleSourceEvidence, query))
	require.Empty(t, events[0].Relations, "a visible source outside the requested message set is not a visible relation target")
}
