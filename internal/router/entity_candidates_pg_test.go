package router

import (
	"context"
	"encoding/json"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type candidateFactWire struct {
	types.StoryFact
	SubjectEntityID *string `json:"subject_entity_id"`
	ObjectEntityID  *string `json:"object_entity_id"`
}

type candidateFixture struct {
	*mcpFixture
	model *understandingChat
}

func newCandidateFixture(t *testing.T) *candidateFixture {
	t.Helper()
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	return &candidateFixture{mcpFixture: f, model: model}
}

func (f *candidateFixture) request(t *testing.T, method, path string, input any, status int) []byte {
	t.Helper()
	body := ""
	if input != nil {
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		body = string(raw)
	}
	r := personalRequest(t, f.router, method, path, body, f.key, true)
	require.Equal(t, status, r.Code, r.Body.String())
	return r.Body.Bytes()
}

func (f *candidateFixture) entity(t *testing.T, name string, aliases ...string) types.Entity {
	t.Helper()
	var out struct{ Data types.Entity }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/entities", map[string]any{
		"canonical_name": name, "entity_type": "organization", "aliases": aliases,
	}, 201), &out))
	return out.Data
}

func (f *candidateFixture) process(t *testing.T, ref string, fact map[string]any) candidateFactWire {
	t.Helper()
	f.model.respond = func(_ context.Context, _ []chat.Message) string {
		raw, err := json.Marshal(
			map[string]any{"title": "实体挂接资料", "summary": map[string]string{"overview": "报道一次合作。"}, "fact": fact},
		)
		require.NoError(t, err)
		return string(raw)
	}
	result := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "official", "source_ref": ref, "title": ref,
		"content": "A collaboration was announced.", "published_at": "2026-10-24",
	})[0]
	require.Equal(t, "accepted", result["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	var page struct{ Data types.StoryPage }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories", nil, 200), &page))
	for _, story := range page.Data.Items {
		var detail struct {
			Data struct{ Facts []candidateFactWire }
		}
		require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+story.ID, nil, 200), &detail))
		for _, stored := range detail.Data.Facts {
			for _, source := range stored.Sources {
				if source.EntryID == result["entry_id"] {
					return stored
				}
			}
		}
	}
	t.Fatal("processed material has no fact")
	return candidateFactWire{}
}

func candidateFact(subject, object string) map[string]any {
	return map[string]any{
		"title":        "合作",
		"subject":      subject,
		"action":       "合作",
		"object":       object,
		"quote":        "A collaboration was announced.",
		"subject_type": "organization",
		"object_type":  "organization",
	}
}

func TestFactEntityLinkingHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	aurora := f.entity(t, "Aurora", "极光")
	partner := f.entity(t, "Partner", "Shared")
	f.entity(t, "Another", "Shared")
	for _, tc := range []struct{ name, subject, object, subjectID, objectID, wantSubject, wantObject string }{
		{"model IDs", "full sentence about Aurora", "unrelated text", aurora.ID, partner.ID, aurora.ID, partner.ID},
		{"normalization", " ＡＵＲＯＲＡ\u3000", "极 光", "", "", aurora.ID, aurora.ID},
		{"invalid IDs", "Unknown institution", "Unknown person", "missing", "missing", "", ""},
		{"exact matching only", "Aurora announced a release", "Shared", "", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact := candidateFact(tc.subject, tc.object)
			fact["subject_entity_id"], fact["object_entity_id"] = tc.subjectID, tc.objectID
			stored := f.process(t, tc.name, fact)
			if tc.wantSubject == "" {
				require.Nil(t, stored.SubjectEntityID)
			} else {
				require.NotNil(t, stored.SubjectEntityID)
				require.Equal(t, tc.wantSubject, *stored.SubjectEntityID)
			}
			if tc.wantObject == "" {
				require.Nil(t, stored.ObjectEntityID)
			} else {
				require.NotNil(t, stored.ObjectEntityID)
				require.Equal(t, tc.wantObject, *stored.ObjectEntityID)
			}
		})
	}
}

type candidateWire struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	EntityID        *string `json:"entity_id"`
	OccurrenceCount int64   `json:"occurrence_count"`
}

func (f *candidateFixture) candidates(t *testing.T, status string) []candidateWire {
	t.Helper()
	var out struct {
		Data struct {
			Items []candidateWire
			Total int64
		}
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entity-candidates?status="+status, nil, 200), &out),
	)
	return out.Data.Items
}

func TestEntityCandidatesApproveIgnoreAndAliasHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	first := f.process(t, "first", candidateFact("New Institute", "New Person"))
	second := f.process(t, "second", candidateFact(" ＮＥＷ Institute ", "Model A"))
	items := f.candidates(t, "pending")
	require.Len(t, items, 3)
	require.Equal(t, "New Institute", items[0].Name)
	require.EqualValues(t, 2, items[0].OccurrenceCount)
	institution := items[0]
	var detail struct {
		Data struct {
			Candidate   candidateWire
			Occurrences []struct {
				Fact     candidateFactWire
				Endpoint string
			}
			Total int64
		}
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entity-candidates/"+institution.ID, nil, 200), &detail),
	)
	require.EqualValues(t, 2, detail.Data.Total)
	require.Len(t, detail.Data.Occurrences, 2)
	require.Equal(t, "subject", detail.Data.Occurrences[0].Endpoint)
	require.ElementsMatch(
		t,
		[]string{first.ID, second.ID},
		[]string{detail.Data.Occurrences[0].Fact.ID, detail.Data.Occurrences[1].Fact.ID},
	)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institution.ID+"/ignore", map[string]any{}, 200)
	require.Len(t, f.candidates(t, "pending"), 2)
	third := f.process(t, "third", candidateFact("New Institute", "New Institute"))
	require.NoError(t, f.articles.(interfaces.EntryService).
		ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	ignored := f.candidates(t, "ignored")
	require.Len(t, ignored, 1)
	require.EqualValues(t, 4, ignored[0].OccurrenceCount)
	var approved struct{ Data candidateWire }
	require.NoError(
		t,
		json.Unmarshal(
			f.request(
				t,
				http.MethodPost,
				"/api/v1/entity-candidates/"+institution.ID+"/approve",
				map[string]any{},
				200,
			),
			&approved,
		),
	)
	require.Equal(t, "approved", approved.Data.Status)
	require.NotNil(t, approved.Data.EntityID)
	for _, fact := range []candidateFactWire{first, second, third} {
		var story struct {
			Data struct{ Facts []candidateFactWire }
		}
		require.NoError(
			t,
			json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+fact.StoryID, nil, 200), &story),
		)
		require.Equal(t, approved.Data.EntityID, story.Data.Facts[0].SubjectEntityID)
		if fact.ID == third.ID {
			require.Equal(t, approved.Data.EntityID, story.Data.Facts[0].ObjectEntityID)
		}
	}
	var entity struct{ Data types.EntityDetail }
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entities/"+*approved.Data.EntityID, nil, 200), &entity),
	)
	require.Equal(t, "New Institute", entity.Data.Entity.CanonicalName)
	// Approval retries do not create a second entity.
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institution.ID+"/approve", map[string]any{}, 200)
	target := f.entity(t, "Established")
	pending := f.candidates(t, "pending")
	var person candidateWire
	for _, item := range pending {
		if item.Name == "New Person" {
			person = item
		}
	}
	require.NotEmpty(t, person.ID)
	f.request(
		t,
		http.MethodPost,
		"/api/v1/entity-candidates/"+person.ID+"/alias",
		map[string]string{"entity_id": "missing"},
		404,
	)
	f.request(
		t,
		http.MethodPost,
		"/api/v1/entity-candidates/"+person.ID+"/alias",
		map[string]string{"entity_id": target.ID},
		200,
	)
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entities/"+target.ID, nil, 200), &entity))
	require.Contains(t, entity.Data.Entity.Aliases, "New Person")
	var story struct {
		Data struct{ Facts []candidateFactWire }
	}
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+first.StoryID, nil, 200), &story))
	require.Equal(t, &target.ID, story.Data.Facts[0].ObjectEntityID)
	// New occurrences of a resolved name use the entity directory.
	fourth := f.process(t, "fourth", candidateFact("new institute", "New Person"))
	require.Equal(t, approved.Data.EntityID, fourth.SubjectEntityID)
	require.Equal(t, &target.ID, fourth.ObjectEntityID)
}

func TestMaterialEntityCandidatesReviewAndSourceLifecycleHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	f.model.respond = func(_ context.Context, _ []chat.Message) string {
		return `{"title":"研究资料","summary":{"overview":"New Institute 与 Researcher 发布研究。"},"entity_mentions":[{"name":"New Institute","type":"organization","quote":"New Institute and Researcher publish research."},{"name":"Researcher","type":"person","quote":"New Institute and Researcher publish research."}],"fact":null}`
	}
	process := func(ref string) string {
		t.Helper()
		result := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
			"library": "references", "source_ref": ref, "title": ref,
			"content": "New Institute and Researcher publish research.",
		})[0]
		require.Equal(t, "accepted", result["status"])
		require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
		return result["entry_id"].(string)
	}
	normal, manual, full := process("normal"), process("manual"), process("full")
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	f.request(t, http.MethodPut, "/api/v1/entries/"+manual+"/entities", map[string]any{"entity_ids": []string{}}, 200)
	target := f.entity(t, "Known Researcher")
	for _, entity := range []types.Entity{target, f.entity(t, "First"), f.entity(t, "Second")} {
		require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: full, EntityID: entity.ID}).Error)
	}
	items := f.candidates(t, "pending")
	require.Len(t, items, 2)
	for _, item := range items {
		require.EqualValues(t, 3, item.OccurrenceCount)
		var detail struct{ Data types.EntityCandidateDetail }
		require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entity-candidates/"+item.ID+"?limit=2&offset=1", nil, 200), &detail))
		require.EqualValues(t, 3, detail.Data.Total)
		require.Len(t, detail.Data.Occurrences, 2)
		for _, source := range detail.Data.Occurrences {
			require.Nil(t, source.Fact)
			require.NotNil(t, source.Entry)
			require.NotEmpty(t, source.Entry.Title)
			require.Equal(t, "New Institute and Researcher publish research.", source.Quote)
		}
		if item.Name == "New Institute" {
			f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+item.ID+"/approve", nil, 200)
		} else {
			f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+item.ID+"/alias", map[string]string{"entity_id": target.ID}, 200)
		}
	}
	for entryID, want := range map[string]int64{normal: 2, manual: 0, full: 3} {
		var count int64
		require.NoError(t, f.db.Model(&types.EntryEntity{}).Where("entry_id=?", entryID).Count(&count).Error)
		require.Equal(t, want, count)
	}
	// A revised source loses its old candidate evidence before background processing.
	revised := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "references", "source_ref": "normal", "title": "Revised", "content": "Revised material without named participants.",
	})[0]
	require.Equal(t, normal, revised["entry_id"])
	for _, item := range f.candidates(t, "all") {
		require.EqualValues(t, 2, item.OccurrenceCount)
	}
	deleteEvidenceEntry(t, f.mcpFixture, full)
	for _, item := range f.candidates(t, "all") {
		require.EqualValues(t, 1, item.OccurrenceCount)
	}
}

func TestManualEntityAliasBackfillsOnlyExactUnlinkedEndpointsHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	target := f.entity(t, "Known Institute")
	other := f.entity(t, "Other Institute")
	fact := candidateFact(" ＮＥＷ Name ", "New Name is mentioned in a sentence")
	fact["subject_type"], fact["object_type"] = "other", "other"
	exact := f.process(t, "alias-history", fact)
	occupied := candidateFact("New Name", "Unrelated")
	occupied["subject_entity_id"] = other.ID
	preserved := f.process(t, "preserve-explicit-id", occupied)
	f.request(t, http.MethodPatch, "/api/v1/entities/"+target.ID, map[string]any{
		"canonical_name": target.CanonicalName, "entity_type": target.EntityType, "aliases": []string{"New Name"},
	}, 200)
	var detail struct {
		Data struct{ Facts []candidateFactWire }
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+exact.StoryID, nil, 200), &detail),
	)
	require.Equal(t, &target.ID, detail.Data.Facts[0].SubjectEntityID)
	require.Nil(t, detail.Data.Facts[0].ObjectEntityID)
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+preserved.StoryID, nil, 200), &detail),
	)
	require.Equal(t, &other.ID, detail.Data.Facts[0].SubjectEntityID)
	// A newly added alias shared with another entity does not resolve ambiguity.
	ambiguous := f.process(t, "ambiguous-history", candidateFact("Shared Alias", "Model"))
	f.entity(t, "Ambiguous Institute", "Shared Alias")
	f.request(t, http.MethodPatch, "/api/v1/entities/"+target.ID, map[string]any{
		"canonical_name": target.CanonicalName, "entity_type": target.EntityType,
		"aliases": []string{"New Name", "Shared Alias"},
	}, 200)
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+ambiguous.StoryID, nil, 200), &detail),
	)
	require.Nil(t, detail.Data.Facts[0].SubjectEntityID)
}

func TestEntityCandidatesExcludeOtherEndpointsAuthorsAndLibrariesHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	fact := candidateFact("Named Researcher", "Model Product")
	fact["subject_type"], fact["object_type"] = "person", "other"
	f.process(t, "person-and-product", fact)
	items := f.candidates(t, "pending")
	require.Len(t, items, 1)
	require.Equal(t, "Named Researcher", items[0].Name)
	// Merely supplying a byline and fact-shaped model output cannot create candidates in other libraries.
	for _, library := range []string{"references", "personal", "ai-output"} {
		result := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
			"library": library, "source_ref": "exclude-" + library, "title": "Other material",
			"content": "A collaboration was announced.", "author": "Byline Author", "published_at": "2026-10-24",
		})[0]
		require.Equal(t, "accepted", result["status"])
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
	}
	require.Equal(t, items, f.candidates(t, "pending"))
	// An author alone on a synthesis has no endpoint occurrence.
	f.model.respond = func(_ context.Context, _ []chat.Message) string {
		return `{"title":"综合资料","summary":{"overview":"评论已有报道。"},"fact":null}`
	}
	result := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "official", "source_ref": "byline-only", "title": "Synthesis",
		"content": "A collaboration was announced.", "author": "Byline Author", "published_at": "2026-10-24",
	})[0]
	require.Equal(t, "accepted", result["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	require.Equal(t, items, f.candidates(t, "pending"))
}

func TestEntityCandidateReviewUsesOnlyRecordedOccurrencesAndLogsHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	// Same text without an institution/person classification has no occurrence.
	unrecordedFact := candidateFact("New Institute", "Product")
	unrecordedFact["subject_type"], unrecordedFact["object_type"] = "other", "other"
	unrecorded := f.process(t, "unrecorded", unrecordedFact)
	fact := candidateFact("New Institute", "Other Person")
	fact["subject_entity_id"] = 42 // A bad optional field cannot discard valid evidence.
	f.process(t, "recorded", fact)
	items := f.candidates(t, "pending")
	var institute candidateWire
	for _, item := range items {
		if item.Name == "New Institute" {
			institute = item
		}
	}
	require.NotEmpty(t, institute.ID)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institute.ID+"/ignore", nil, 200)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institute.ID+"/approve", nil, 200)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institute.ID+"/approve", nil, 200)
	var story struct {
		Data struct{ Facts []candidateFactWire }
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+unrecorded.StoryID, nil, 200), &story),
	)
	require.Nil(t, story.Data.Facts[0].SubjectEntityID, "approval must not scan unrelated historical text")
	target := f.entity(t, "Known")
	for _, item := range items {
		if item.ID != institute.ID {
			f.request(
				t,
				http.MethodPost,
				"/api/v1/entity-candidates/"+item.ID+"/alias",
				map[string]string{"entity_id": target.ID},
				200,
			)
		}
	}
	f.request(t, http.MethodPatch, "/api/v1/entities/"+target.ID, map[string]any{
		"canonical_name": target.CanonicalName, "entity_type": target.EntityType,
		"aliases": []string{"Other Person", "Historical Alias"},
	}, 200)
	logs := handler.NewOperationLogHandler(service.NewOperationLogService(f.db))
	f.router.GET("/api/v1/operation-logs", logs.List)
	for _, kind := range []string{
		"ignore_entity_candidate", "approve_entity_candidate", "alias_entity_candidate", "add_entity_alias",
	} {
		var out struct{ Data types.OperationLogPage }
		require.NoError(
			t,
			json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/operation-logs?operation_type="+kind, nil, 200), &out),
		)
		require.EqualValues(t, 1, out.Data.Total, kind)
		require.NotEmpty(t, out.Data.Items[0].Actor)
		require.NotEmpty(t, out.Data.Items[0].After)
	}
	for _, path := range []string{"?limit=101", "?offset=-1", "?status=unknown"} {
		f.request(t, http.MethodGet, "/api/v1/entity-candidates"+path, nil, 400)
	}
	f.request(t, http.MethodGet, "/api/v1/entity-candidates/missing", nil, 404)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/missing/approve", nil, 404)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institute.ID+"/ignore", nil, 400)
	f.request(t, http.MethodPost, "/api/v1/entity-candidates/"+institute.ID+"/alias", map[string]any{}, 400)
}

func TestEntityCandidateReferencesSurviveEntityMergeAndDeleteHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	stored := f.process(t, "reviewed-entity", candidateFact("New Institute", "Product"))
	var selected candidateWire
	for _, item := range f.candidates(t, "pending") {
		if item.Name == "New Institute" {
			selected = item
		}
	}
	var reviewed struct{ Data candidateWire }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost,
		"/api/v1/entity-candidates/"+selected.ID+"/approve", nil, 200), &reviewed))
	require.NotNil(t, reviewed.Data.EntityID)
	keep := f.entity(t, "Retained Institute")
	f.request(t, http.MethodPost, "/api/v1/entities/merge", map[string]string{
		"keep_entity_id": keep.ID, "merge_entity_id": *reviewed.Data.EntityID,
	}, 200)
	var story struct {
		Data struct{ Facts []candidateFactWire }
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+stored.StoryID, nil, 200), &story),
	)
	require.Equal(t, &keep.ID, story.Data.Facts[0].SubjectEntityID)
	var detail struct {
		Data struct{ Candidate candidateWire }
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entity-candidates/"+selected.ID, nil, 200), &detail),
	)
	require.Equal(t, &keep.ID, detail.Data.Candidate.EntityID)
	f.request(t, http.MethodDelete, "/api/v1/entities/"+keep.ID, nil, 200)
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+stored.StoryID, nil, 200), &story),
	)
	require.Nil(t, story.Data.Facts[0].SubjectEntityID)
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entity-candidates/"+selected.ID, nil, 200), &detail),
	)
	require.Equal(t, "pending", detail.Data.Candidate.Status)
	require.EqualValues(t, 1, detail.Data.Candidate.OccurrenceCount)
}

func TestFactEntityLinkingValidatesLongMaterialMergeOptionsHTTP(t *testing.T) {
	model := &understandingChat{}
	base := newMCPFixture(t, mcpProcessingFixture{chat: model, maxBodyChars: 900})
	base.seedProcessingModels(t)
	f := &candidateFixture{mcpFixture: base, model: model}
	existing := f.entity(t, "Established")
	lateID := ""
	model.respond = func(ctx context.Context, messages []chat.Message) string {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		fact := candidateFact("Unrecognized subject text", "Unrecognized object text")
		fact["subject_entity_id"] = existing.ID
		fact["subject_type"], fact["object_type"] = "organization", "other"
		if stage == "article_summary_merge" {
			require.Contains(t, messages[0].Content, "subject_entity_id")
			require.Contains(t, messages[1].Content, "subject_type")
			lateID = f.entity(t, "Created while the merge model was running").ID
			// The new ID exists at save time but was never offered to this call.
			require.NotContains(t, messages[1].Content, lateID)
			fact["object_entity_id"] = lateID
		}
		raw, err := json.Marshal(
			map[string]any{"title": "分段资料", "summary": map[string]string{"overview": "资料报道一次合作。"}, "fact": fact},
		)
		require.NoError(t, err)
		return string(raw)
	}
	result := ingestRevisionHTTP(t, base, map[string]any{
		"library": "official", "source_ref": "long-endpoint-validation", "title": "Long material",
		"content":      strings.Repeat("Background and measurements.\n\n", 70) + "A collaboration was announced.",
		"published_at": "2026-10-24",
	})[0]
	require.Equal(t, "accepted", result["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	require.NotEmpty(t, lateID)
	var page struct{ Data types.StoryPage }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories", nil, 200), &page))
	require.Len(t, page.Data.Items, 1)
	var detail struct {
		Data struct{ Facts []candidateFactWire }
	}
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories/"+page.Data.Items[0].ID, nil, 200), &detail),
	)
	require.Equal(t, &existing.ID, detail.Data.Facts[0].SubjectEntityID)
	require.Nil(t, detail.Data.Facts[0].ObjectEntityID)
}
