package router

import (
	"encoding/json"
	"net/http"
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
)

type entityGraphWire struct {
	Center types.Entity   `json:"center"`
	Nodes  []types.Entity `json:"nodes"`
	Edges  []struct {
		SubjectID    string `json:"subject_entity_id"`
		ObjectID     string `json:"object_entity_id"`
		FactCount    int64  `json:"fact_count"`
		LatestAction string `json:"latest_action"`
	} `json:"edges"`
}

func readEntityGraph(t *testing.T, f *candidateFixture, id, query string) entityGraphWire {
	t.Helper()
	var out struct{ Data entityGraphWire }
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entities/"+id+"/graph"+query, nil, 200), &out),
	)
	return out.Data
}

func TestEntityGraphGroupsDirectedVisibleFactsOneHopHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a, b, c := f.entity(t, "Aurora"), f.entity(t, "Partner"), f.entity(t, "Third")
	makeFact := func(ref, subject, object, action, date string) candidateFactWire {
		fact := candidateFact(subject, object)
		fact["action"], fact["occurred_at"] = action, date
		return f.process(t, ref, fact)
	}
	makeFact("first", "Aurora", "Partner", "announces", "2026-10-20")
	makeFact("second", "Aurora", "Partner", "expands", "2026-10-22")
	makeFact("reverse", "Partner", "Aurora", "responds", "2026-10-21")
	makeFact("single", "Aurora", "Model", "releases", "2026-10-23")
	makeFact("second-hop", "Partner", "Third", "joins", "2026-10-24")
	graph := readEntityGraph(t, f, a.ID, "")
	require.Equal(t, a.ID, graph.Center.ID)
	ids := []string{}
	for _, node := range graph.Nodes {
		ids = append(ids, node.ID)
	}
	require.ElementsMatch(t, []string{a.ID, b.ID}, ids)
	require.NotContains(t, ids, c.ID)
	require.Len(t, graph.Edges, 2)
	for _, edge := range graph.Edges {
		if edge.SubjectID == a.ID {
			require.Equal(t, b.ID, edge.ObjectID)
			require.EqualValues(t, 2, edge.FactCount)
			require.Equal(t, "expands", edge.LatestAction)
		} else {
			require.Equal(t, b.ID, edge.SubjectID)
			require.Equal(t, a.ID, edge.ObjectID)
			require.EqualValues(t, 1, edge.FactCount)
			require.Equal(t, "responds", edge.LatestAction)
		}
	}
	graph = readEntityGraph(t, f, a.ID, "?from=2026-10-21&to=2026-10-22")
	require.Len(t, graph.Edges, 2)
	for _, edge := range graph.Edges {
		require.EqualValues(t, 1, edge.FactCount)
	}
	graph = readEntityGraph(t, f, a.ID, "?from=2026-10-24")
	require.Empty(t, graph.Edges)
	require.Len(t, graph.Nodes, 1)
	graph = readEntityGraph(t, f, b.ID, "")
	require.Len(t, graph.Nodes, 3)
	for _, query := range []string{"?from=bad", "?from=2026-10-23&to=2026-10-20", "?to=2026-02-30"} {
		f.request(t, http.MethodGet, "/api/v1/entities/"+a.ID+"/graph"+query, nil, 400)
	}
	f.request(t, http.MethodGet, "/api/v1/entities/missing/graph", nil, 404)
}

func readEntityFacts(t *testing.T, f *candidateFixture, id, query string) types.EntityFactPage {
	t.Helper()
	var out struct{ Data types.EntityFactPage }
	require.NoError(
		t,
		json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entities/"+id+"/facts"+query, nil, 200), &out),
	)
	return out.Data
}

func TestEntityFactsFilterCounterpartDirectionAndKeepUnsourcedCorrectionsHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a, b, c := f.entity(t, "Aurora"), f.entity(t, "Partner"), f.entity(t, "Third")
	makeFact := func(ref, subject, object, date string) candidateFactWire {
		fact := candidateFact(subject, object)
		fact["occurred_at"] = date
		return f.process(t, ref, fact)
	}
	first := makeFact("older", "Aurora", "Partner", "2026-10-20")
	reverse := makeFact("reverse", "Partner", "Aurora", "2026-10-21T23:59:59Z")
	single := makeFact("single", "Unlisted", "Aurora", "2026-10-22")
	hidden := makeFact("hidden", "Aurora", "Third", "2026-10-23")
	manual := makeFact("manual", "Aurora", "Partner", "2026-10-24")
	f.request(t, http.MethodPost, "/api/v1/facts/"+manual.ID+"/split", map[string]string{"title": "Retained"}, 200)
	deleteEvidenceEntry(t, f.mcpFixture, hidden.Sources[0].EntryID)
	deleteEvidenceEntry(t, f.mcpFixture, manual.Sources[0].EntryID)
	page := readEntityFacts(t, f, a.ID, "?limit=2")
	require.EqualValues(t, 4, page.Total)
	require.Len(t, page.Items, 2)
	require.Equal(t, first.ID, page.Items[0].ID)
	require.Equal(t, reverse.ID, page.Items[1].ID)
	require.Equal(t, first.Sources, page.Items[0].Sources)
	page = readEntityFacts(t, f, a.ID, "?limit=2&offset=2")
	require.Equal(t, single.ID, page.Items[0].ID, "single-ended facts belong in the node timeline")
	require.Equal(t, manual.ID, page.Items[1].ID)
	require.True(t, page.Items[1].NoSource)
	require.Empty(t, page.Items[1].Sources)
	page = readEntityFacts(t, f, a.ID, "?other_entity_id="+b.ID)
	require.EqualValues(t, 3, page.Total)
	page = readEntityFacts(t, f, a.ID, "?other_entity_id="+b.ID+"&direction=incoming")
	require.Len(t, page.Items, 1)
	require.Equal(t, reverse.ID, page.Items[0].ID)
	page = readEntityFacts(t, f, a.ID, "?other_entity_id="+b.ID+"&direction=outgoing&from=2026-10-24")
	require.Len(t, page.Items, 1)
	require.Equal(t, manual.ID, page.Items[0].ID)
	page = readEntityFacts(t, f, a.ID, "?from=2026-10-21&to=2026-10-21")
	require.Len(t, page.Items, 1)
	require.Equal(t, reverse.ID, page.Items[0].ID)
	page = readEntityFacts(t, f, a.ID, "?from=2026-10-21T23:59:59Z&to=2026-10-21T23:59:59Z")
	require.Len(t, page.Items, 1)
	graph := readEntityGraph(t, f, a.ID, "")
	require.Len(t, graph.Edges, 2)
	for _, edge := range graph.Edges {
		require.NotEqual(t, c.ID, edge.ObjectID, "hidden-only neighbors must disappear")
		if edge.SubjectID == a.ID {
			require.EqualValues(t, 2, edge.FactCount)
		}
	}
	for _, query := range []string{"?limit=101", "?offset=-1", "?limit=bad", "?direction=wrong", "?from=bad"} {
		f.request(t, http.MethodGet, "/api/v1/entities/"+a.ID+"/facts"+query, nil, 400)
	}
	f.request(t, http.MethodGet, "/api/v1/entities/missing/facts", nil, 404)
	// Material associations are independent of graph endpoints and remain readable.
	var detail struct{ Data types.EntityDetail }
	f.request(
		t,
		http.MethodPut,
		"/api/v1/entries/"+single.Sources[0].EntryID+"/entities",
		map[string]any{"entity_ids": []string{a.ID}},
		200,
	)
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/entities/"+a.ID, nil, 200), &detail))
	require.Len(t, detail.Data.Entries, 1)
	require.Equal(t, single.Sources[0].EntryID, detail.Data.Entries[0].ID)
}

func TestEntityActivityResolvesMergesSelfEdgesAndUnknownOccurrenceHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a, b, retained := f.entity(t, "Aurora"), f.entity(t, "Partner"), f.entity(t, "Retained")
	first := f.process(t, "first", candidateFact("Aurora", "Partner"))
	second := f.process(t, "second", candidateFact("Aurora", "Partner"))
	f.request(t, http.MethodPost, "/api/v1/facts/"+second.ID+"/merge",
		map[string]string{"keep_fact_id": first.ID}, 200)
	f.request(t, http.MethodPost, "/api/v1/entities/merge",
		map[string]string{"keep_entity_id": retained.ID, "merge_entity_id": b.ID}, 200)
	graph := readEntityGraph(t, f, b.ID, "")
	require.Equal(t, retained.ID, graph.Center.ID)
	require.Len(t, graph.Edges, 1)
	require.Equal(t, a.ID, graph.Edges[0].SubjectID)
	require.Equal(t, retained.ID, graph.Edges[0].ObjectID)
	require.EqualValues(t, 1, graph.Edges[0].FactCount, "two sources and a merged fact still form one occurrence")
	page := readEntityFacts(t, f, a.ID, "?other_entity_id="+b.ID)
	require.Len(t, page.Items, 1)
	require.Equal(t, first.ID, page.Items[0].ID)
	require.Len(t, page.Items[0].Sources, 2)
	self := f.process(t, "self", candidateFact("Aurora", "Aurora"))
	graph = readEntityGraph(t, f, a.ID, "")
	require.Len(t, graph.Nodes, 2)
	require.Len(t, graph.Edges, 2)
	page = readEntityFacts(t, f, a.ID, "?other_entity_id="+a.ID+"&direction=outgoing")
	require.Len(t, page.Items, 1)
	require.Equal(t, self.ID, page.Items[0].ID)
	require.Nil(t, page.Items[0].OccurredAt)
	day := self.CreatedAt.UTC().Format("2006-01-02")
	graph = readEntityGraph(t, f, a.ID, "?from="+day+"&to="+day)
	require.Len(t, graph.Edges, 2, "unknown occurrence dates use fact arrival time")
	page = readEntityFacts(t, f, a.ID, "?from=2000-01-01&to=2000-01-02")
	require.Empty(t, page.Items)
	require.Zero(t, page.Total)
}
