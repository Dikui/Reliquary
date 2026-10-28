package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func filteredStories(t *testing.T, f *candidateFixture, query string) types.StoryPage {
	t.Helper()
	var out struct{ Data types.StoryPage }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/stories"+query, nil, 200), &out))
	return out.Data
}

func TestStoryFiltersIntersectEndpointEntitiesAndSourceDomainsAtStoryLevelHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a, b, other := f.entity(t, "Aurora"), f.entity(t, "Partner"), f.entity(t, "Other")
	domains := storyFilterDomains(t, f)
	agent, llm := domains[0].ID, domains[1].ID
	first := f.process(t, "subject-endpoint", candidateFact("Aurora", "Unlisted"))
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"same_story","confidence":0.8}`
	}
	second := f.process(t, "object-endpoint", candidateFact("Unlisted", "Partner"))
	require.Equal(t, first.StoryID, second.StoryID)
	setStorySourceDomains(t, f, second.Sources[0].EntryID, agent)
	f.model.groupRespond = nil
	third := f.process(t, "different-story", candidateFact("Other", "Unlisted"))
	setStorySourceDomains(t, f, third.Sources[0].EntryID, llm)
	// Entry entity tags do not supply story endpoints.
	f.request(t, http.MethodPut, "/api/v1/entries/"+third.Sources[0].EntryID+"/entities",
		map[string]any{"entity_ids": []string{a.ID}}, 200)
	for _, query := range []string{
		"?entity_ids=" + a.ID,
		"?entity_ids=" + b.ID,
		"?entity_ids=" + a.ID + "&domain_ids=" + agent,
		"?entity_ids=" + a.ID + "," + b.ID + "&domain_ids=" + llm + "," + agent,
	} {
		page := filteredStories(t, f, query)
		require.EqualValues(t, 1, page.Total, query)
		require.Len(t, page.Items, 1, query)
		require.Equal(t, first.StoryID, page.Items[0].ID, query)
	}
	require.Empty(t, filteredStories(t, f, "?entity_ids="+a.ID+"&domain_ids="+llm).Items)
	require.Empty(t, filteredStories(t, f, "?entity_ids="+other.ID+"&domain_ids="+agent).Items)
	page := filteredStories(t, f, "?entity_ids="+a.ID+","+other.ID+"&domain_ids="+agent+","+llm+"&limit=1")
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	firstPageID := page.Items[0].ID
	page = filteredStories(t, f, "?entity_ids="+a.ID+","+other.ID+"&domain_ids="+agent+","+llm+"&limit=1&offset=1")
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.NotEqual(t, firstPageID, page.Items[0].ID)
}

func storyFilterDomains(t *testing.T, f *candidateFixture) []domainResponse {
	t.Helper()
	seedTestDomains(t, f.db)
	var out struct{ Data []domainResponse }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/domains", nil, 200), &out))
	return out.Data
}

func setStorySourceDomains(t *testing.T, f *candidateFixture, entryID string, ids ...string) {
	t.Helper()
	if ids == nil {
		ids = []string{}
	}
	f.request(t, http.MethodPut, "/api/v1/entries/"+entryID+"/domains", map[string]any{"domain_ids": ids}, 200)
}

func TestStoryFiltersDeriveDomainsFromSourcesAndReflectEditsHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	domains := storyFilterDomains(t, f)
	agent, llm := domains[0].ID, domains[1].ID
	first := f.process(t, "first", candidateFact("Aurora", "Partner"))
	second := f.process(t, "second", candidateFact("Another", "Other"))
	setStorySourceDomains(t, f, first.Sources[0].EntryID, agent)
	setStorySourceDomains(t, f, second.Sources[0].EntryID, llm)
	page := filteredStories(t, f, "?domain_ids="+agent)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, first.StoryID, page.Items[0].ID)
	setStorySourceDomains(t, f, first.Sources[0].EntryID, llm)
	require.Empty(t, filteredStories(t, f, "?domain_ids="+agent).Items)
	require.EqualValues(t, 2, filteredStories(t, f, "?domain_ids="+llm).Total)
	setStorySourceDomains(t, f, first.Sources[0].EntryID)
	require.EqualValues(t, 1, filteredStories(t, f, "?domain_ids="+llm).Total)
}

func TestStoryFiltersMCPMatchesHTTPAndValidatesSelections(t *testing.T) {
	f := newCandidateFixture(t)
	a, b := f.entity(t, "Aurora"), f.entity(t, "Partner")
	domains := storyFilterDomains(t, f)
	first := f.process(t, "first", candidateFact("Unlisted", "Aurora"))
	second := f.process(t, "second", candidateFact("Partner", "Unlisted"))
	setStorySourceDomains(t, f, first.Sources[0].EntryID, domains[0].ID)
	setStorySourceDomains(t, f, second.Sources[0].EntryID, domains[1].ID)
	client := f.connect(t, "2025-11-25")
	for _, query := range []types.StoryQuery{
		{EntityIDs: []string{a.ID}, DomainIDs: []string{domains[0].ID}},
		{EntityIDs: []string{b.ID}, DomainIDs: []string{domains[0].ID}},
		{EntityIDs: []string{a.ID, b.ID}, DomainIDs: []string{domains[0].ID, domains[1].ID}, Limit: 1},
		{EntityIDs: []string{a.ID, b.ID}, Limit: 1, Offset: 1},
		{DomainIDs: []string{"missing"}},
		{EntityIDs: []string{"missing"}},
	} {
		var got types.StoryPage
		mcpCall(t, client, "list_stories", query, &got)
		params := url.Values{
			"entity_ids": query.EntityIDs, "domain_ids": query.DomainIDs,
			"limit": {strconv.Itoa(query.Limit)}, "offset": {strconv.Itoa(query.Offset)},
		}
		require.Equal(t, filteredStories(t, f, "?"+params.Encode()), got)
	}
	for _, key := range []string{"entity_ids", "domain_ids"} {
		for _, ids := range [][]string{make([]string, 101), {" "}} {
			result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "list_stories", Arguments: map[string]any{key: ids},
			})
			require.NoError(t, err)
			require.True(t, result.IsError)
		}
		var ids []string
		for i := 0; i < 101; i++ {
			ids = append(ids, strings.Repeat("x", i+1))
		}
		f.request(t, http.MethodGet, "/api/v1/stories?"+key+"="+strings.Join(ids, ","), nil, 400)
	}
	page := filteredStories(t, f, "?entity_ids=%20"+a.ID+"%20,"+a.ID+"&entity_ids="+a.ID)
	require.EqualValues(t, 1, page.Total, "HTTP selections trim and deduplicate like material filters")
}

func TestStoryFiltersUnionAllSourcesAndIgnoreMentionsHTTPAndMCP(t *testing.T) {
	f := newCandidateFixture(t)
	domains := storyFilterDomains(t, f)
	agent, llm := domains[0].ID, domains[1].ID
	first := f.process(t, "first", candidateFact("Aurora", "Partner"))
	setStorySourceDomains(t, f, first.Sources[0].EntryID, agent)
	mention := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "ai-output", "content": "An analysis.", "mentioned_fact_ids": []string{first.ID},
	})[0]
	require.Equal(t, "accepted", mention["status"])
	setStorySourceDomains(t, f, mention["entry_id"].(string), llm)
	require.Len(t, groupingDetail(t, f.mcpFixture, first.StoryID).Mentions, 1)
	f.model.respond = func(context.Context, []chat.Message) string {
		return `{"title":"综合稿","summary":{"overview":"讨论已有合作。"},"fact":null}`
	}
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"mention","confidence":0.8}`
	}
	synthesis := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "official", "source_ref": "synthesis", "title": "Synthesis",
		"content": "An overview of the collaboration.", "published_at": "2026-10-24",
	})[0]
	require.Equal(t, "accepted", synthesis["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	setStorySourceDomains(t, f, synthesis["entry_id"].(string), llm)
	require.Len(t, groupingDetail(t, f.mcpFixture, first.StoryID).Mentions, 2)
	client := f.connect(t, "2025-11-25")
	assertDomain := func(id string, total int64) {
		t.Helper()
		page := filteredStories(t, f, "?domain_ids="+id)
		require.Equal(t, total, page.Total)
		var got types.StoryPage
		mcpCall(t, client, "list_stories", map[string]any{"domain_ids": []string{id}}, &got)
		require.Equal(t, page, got)
	}
	assertDomain(agent, 1)
	assertDomain(llm, 0)
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"same_occurrence","confidence":0.8}`
	}
	second := f.process(t, "second-source", candidateFact("Aurora", "Partner"))
	require.Equal(t, first.ID, second.ID)
	require.Len(t, second.Sources, 2)
	for _, source := range second.Sources {
		if source.EntryID != first.Sources[0].EntryID {
			setStorySourceDomains(t, f, source.EntryID, llm)
		}
	}
	assertDomain(agent, 1)
	assertDomain(llm, 1)
	// Removing the only Agent source updates the union without rebuilding the story.
	deleteEvidenceEntry(t, f.mcpFixture, first.Sources[0].EntryID)
	assertDomain(agent, 0)
	assertDomain(llm, 1)
}

func TestStoryFiltersExcludeHiddenEndpointsAndKeepUnsourcedCorrectionsHTTPAndMCP(t *testing.T) {
	f := newCandidateFixture(t)
	a, b := f.entity(t, "Aurora"), f.entity(t, "Partner")
	domains := storyFilterDomains(t, f)
	agent, llm := domains[0].ID, domains[1].ID
	hidden := f.process(t, "hidden", candidateFact("Aurora", "Unlisted"))
	setStorySourceDomains(t, f, hidden.Sources[0].EntryID, agent)
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"same_story","confidence":0.8}`
	}
	live := f.process(t, "live-sibling", candidateFact("Partner", "Unlisted"))
	require.Equal(t, hidden.StoryID, live.StoryID)
	setStorySourceDomains(t, f, live.Sources[0].EntryID, llm)
	f.model.groupRespond = nil
	manual := f.process(t, "manual", candidateFact("Aurora", "Partner"))
	setStorySourceDomains(t, f, manual.Sources[0].EntryID, agent)
	var corrected struct{ Data types.StoryFact }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/facts/"+manual.ID+"/split",
		map[string]string{"title": "Manually retained"}, 200), &corrected))
	// Revision withdraws old evidence before its replacement is processed.
	revised := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "official", "source_ref": "hidden", "title": "Revised",
		"content": "The previous claim was retracted.", "published_at": "2026-10-24",
	})[0]
	require.Equal(t, "accepted", revised["status"])
	deleteEvidenceEntry(t, f.mcpFixture, manual.Sources[0].EntryID)
	page := filteredStories(t, f, "?entity_ids="+a.ID)
	require.EqualValues(t, 1, page.Total, "the hidden sibling must not contribute its endpoint")
	require.Len(t, page.Items, 1)
	require.Equal(t, corrected.Data.StoryID, page.Items[0].ID)
	require.Empty(t, filteredStories(t, f, "?domain_ids="+agent).Items)
	require.Empty(t, filteredStories(t, f, "?entity_ids="+a.ID+"&domain_ids="+llm).Items)
	page = filteredStories(t, f, "?entity_ids="+b.ID+"&domain_ids="+llm)
	require.Len(t, page.Items, 1)
	require.Equal(t, live.StoryID, page.Items[0].ID)
	detail := groupingDetail(t, f.mcpFixture, corrected.Data.StoryID)
	require.Len(t, detail.Facts, 1)
	require.True(t, detail.Facts[0].NoSource)
	client := f.connect(t, "2025-11-25")
	var got types.StoryPage
	mcpCall(t, client, "list_stories", map[string]any{"entity_ids": []string{a.ID}}, &got)
	require.Len(t, got.Items, 1)
	require.Equal(t, corrected.Data.StoryID, got.Items[0].ID)
	mcpCall(t, client, "list_stories", map[string]any{
		"entity_ids": []string{a.ID}, "domain_ids": []string{llm, agent},
	}, &got)
	require.Zero(t, got.Total)
	require.Empty(t, got.Items)
	deleteEvidenceEntry(t, f.mcpFixture, live.Sources[0].EntryID)
	require.EqualValues(t, 1, filteredStories(t, f, "").Total, "a story with only hidden facts disappears")
}
