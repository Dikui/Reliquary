package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/graphreplica"
	"reliquary/internal/handler"
	"reliquary/internal/mcpserver"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

type graphSearchFixture struct {
	*mcpFixture
	graph     *service.GraphSearchService
	replica   *graphreplica.Service
	store     *graphreplica.Store
	tasks     *queue.Client
	ids       []string
	otherIDs  []string
	libraries []string
}

func newGraphSearchFixture(t *testing.T) *graphSearchFixture {
	t.Helper()
	if os.Getenv("RELIQUARY_TEST_NEO4J_URI") == "" || os.Getenv("RELIQUARY_TEST_REDIS_ADDR") == "" {
		t.Skip("real Neo4j and Redis connection variables required")
	}
	f := newMCPFixture(t)
	cfg := config.Neo4jConfig{URI: os.Getenv("RELIQUARY_TEST_NEO4J_URI"), Username: os.Getenv("RELIQUARY_TEST_NEO4J_USERNAME"), Password: os.Getenv("RELIQUARY_TEST_NEO4J_PASSWORD")}
	store := graphreplica.NewStore(cfg)
	require.NoError(t, store.Available(f.ctx))
	tasks := queue.NewClientWithRedis(f.db, asynq.RedisClientOpt{Addr: os.Getenv("RELIQUARY_TEST_REDIS_ADDR"), DB: 2})
	out := &graphSearchFixture{mcpFixture: f, store: store, tasks: tasks, graph: service.NewGraphSearchService(f.db, store), replica: graphreplica.NewService(f.db, store, tasks)}
	var opinions types.Library
	require.NoError(t, f.db.Where("space_id=1 AND directory='media'").First(&opinions).Error)
	f.library = &opinions
	out.libraries = append(out.libraries, f.library.ID)
	var mine types.Library
	require.NoError(t, f.db.Where("space_id=1 AND directory='personal'").First(&mine).Error)
	out.libraries = append(out.libraries, mine.ID)
	for _, kind := range []string{"company", "person", "product", "project", "other"} {
		id := uuid.NewString()
		out.ids = append(out.ids, id)
		name := "Graph " + strings.ToUpper(kind)
		require.NoError(t, f.db.Create(&types.Entity{ID: id, SpaceID: 1, LibraryID: out.libraries[0], CanonicalName: name, NormalizedName: strings.ToLower(name), EntityType: kind}).Error)
	}
	for _, id := range out.ids {
		var entity types.Entity
		require.NoError(t, f.db.First(&entity, "id=?", id).Error)
		entity.ID = uuid.NewString()
		entity.LibraryID = out.libraries[1]
		out.otherIDs = append(out.otherIDs, entity.ID)
		require.NoError(t, f.db.Create(&entity).Error)
	}
	for _, lib := range out.libraries {
		for _, name := range []string{"cooperates", "supports"} {
			require.NoError(t, f.db.Create(&types.LibraryRelationType{LibraryID: lib, Name: name, Enabled: true}).Error)
		}
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, f.db.Create(&types.EntityAlias{ID: uuid.NewString(), SpaceID: 1, LibraryID: out.libraries[0], EntityID: out.ids[i], Alias: "Shared", NormalizedAlias: "shared", EntityType: []string{"company", "person"}[i]}).Error)
	}
	require.NoError(t, f.db.Create(&types.EntityAlias{ID: uuid.NewString(), SpaceID: 1, LibraryID: out.libraries[0], EntityID: out.ids[0], Alias: "Alias A", NormalizedAlias: "alias a", EntityType: "company"}).Error)
	f.router.POST("/api/v1/graph/search", handler.NewGraphSearchHandler(out.graph).Search)
	f.router.Any("/graph-mcp", gin.WrapH(mcpserver.New(mcpserver.Services{Entries: f.articles, Graph: out.graph})))
	evaluations := service.NewEvaluationService(f.db, nil, nil)
	evaluations.GraphSearch = out.graph.EvaluationSearch
	handler.NewEvaluationHandler(evaluations, service.NewSearchRecorder(f.db)).Register(f.router.Group("/api/v1"))
	mux := queue.NewServeMux()
	mux.HandleFunc(graphreplica.TypeSync, out.replica.HandleSync)
	require.NoError(t, tasks.Start(context.Background(), mux, queue.Config{Pools: types.WorkerPoolConcurrency{Maintenance: 1}, CheckInterval: 20 * time.Millisecond}))
	t.Cleanup(func() {
		require.NoError(t, tasks.Close())
		var name string
		require.NoError(t, f.db.Raw("SELECT current_database()").Scan(&name).Error)
		r := redis.NewClient(&redis.Options{Addr: os.Getenv("RELIQUARY_TEST_REDIS_ADDR"), DB: 2})
		defer r.Close()
		keys, err := r.Keys(context.Background(), "asynq:*"+name+"*").Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, r.Del(context.Background(), keys...).Err())
		}
		require.NoError(t, r.SRem(context.Background(), "asynq:queues", "reliquary:"+name+":low").Err())
		driver, err := neo4j.NewDriver(cfg.URI, neo4j.BasicAuth(cfg.Username, cfg.Password, ""))
		require.NoError(t, err)
		defer driver.Close(context.Background())
		_, err = neo4j.ExecuteQuery(context.Background(), driver, `MATCH (e:Entity {namespace:'',space_id:1}) WHERE e.id IN $ids DETACH DELETE e`, map[string]any{"ids": append(append([]string{}, out.ids...), out.otherIDs...)}, neo4j.EagerResultTransformer)
		require.NoError(t, err)
		require.NoError(t, store.Close())
	})
	return out
}
func (f *graphSearchFixture) add(t *testing.T, index, a, b, library int, kind string) string {
	t.Helper()
	id := uuid.NewString()
	entry := &types.Entry{ID: id, SpaceID: 1, LibraryID: f.libraries[library], Title: "Graph source " + id, Type: "manual", ParseStatus: "completed", EnableStatus: "enabled", ContentType: "event"}
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{PublishedAt: "2026-10-01", Author: "Speaker"}}))
	require.NoError(t, f.db.Create(entry).Error)
	ids := f.ids
	if library == 1 {
		ids = f.otherIDs
	}
	relation := types.Relation{ID: strings.Repeat("0", index) + uuid.NewString()[index:], SpaceID: 1, LibraryID: entry.LibraryID, SubjectID: ids[a], ObjectID: ids[b], RelationType: kind, Version: 1}
	require.NoError(t, f.db.Create(&relation).Error)
	require.NoError(t, f.db.Create(&types.RelationSource{RelationID: relation.ID, EntryID: id, RunID: "test", BodyHash: "hash", Evidence: []types.RelationEvidence{{Locations: []types.SourceLocation{{HeadingPath: []string{"Evidence"}, Paragraph: 2, Start: 10, End: 20, BodyHash: "hash", Mapping: "exact"}}, Speaker: &types.ProcessingSpeaker{EntityID: ids[1], Name: "Speaker", Selection: "author"}}}}).Error)
	for _, entity := range []string{relation.SubjectID, relation.ObjectID} {
		require.NoError(t, f.db.Create(&types.GraphPendingSync{ID: uuid.NewString(), SpaceID: 1, EntityID: entity, RelationID: relation.ID, RelationVersion: 1}).Error)
	}
	return id
}
func (f *graphSearchFixture) sync(t *testing.T) {
	t.Helper()
	var ids []string
	require.NoError(t, f.db.Model(&types.GraphPendingSync{}).Where("completed_at IS NULL").Distinct().Pluck("entity_id", &ids).Error)
	for _, id := range ids {
		payload, _ := json.Marshal(map[string]any{"space_id": 1, "entity_id": id, "job_id": "graph:" + id})
		_, err := f.tasks.Enqueue(queue.NewTask(graphreplica.TypeSync, payload), queue.Queue(types.QueueMaintenance))
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool {
		var n int64
		return f.db.Model(&types.GraphPendingSync{}).Where("completed_at IS NULL").Count(&n).Error == nil && n == 0
	}, 20*time.Second, 20*time.Millisecond)
}
func (f *graphSearchFixture) request() types.GraphSearchRequest {
	return types.GraphSearchRequest{Libraries: []string{f.libraries[0]}, Entity: types.GraphEntityInput{ID: f.ids[0]}, Limit: 100}
}
func TestGraphSearchNamesHopsPathsScopeSourcesAndPending(t *testing.T) {
	f := newGraphSearchFixture(t)
	first := f.add(t, 0, 0, 1, 0, "cooperates")
	f.add(t, 0, 1, 2, 0, "cooperates")
	f.add(t, 0, 2, 3, 0, "supports")
	f.add(t, 0, 0, 4, 1, "cooperates")
	request := f.request()
	out, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, out.Edges)
	require.EqualValues(t, 8, out.PendingSync.Global)
	require.EqualValues(t, 1, out.PendingSync.Involved)
	f.sync(t)
	for hop := 1; hop <= 3; hop++ {
		request.Hops = hop
		out, err = f.graph.Search(f.ctx, request)
		require.NoError(t, err)
		require.Len(t, out.Edges, hop)
		require.Zero(t, out.PendingSync.Global)
		require.False(t, out.Truncated)
	}
	request.Entity = types.GraphEntityInput{Name: "ＡＬＩＡＳ   A"}
	request.Hops = 1
	alias, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, alias.Edges, 1)
	request.Entity = types.GraphEntityInput{Name: "graph company"}
	canonical, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, alias.Edges, canonical.Edges)
	request.Entity = types.GraphEntityInput{Name: "SHARED"}
	ambiguous, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "needs_selection", ambiguous.Status)
	require.Len(t, ambiguous.Candidates["entity"], 2)
	require.Empty(t, ambiguous.Edges)
	request = f.request()
	request.Hops = 3
	request.RelationTypes = []string{"cooperates"}
	filtered, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, filtered.Edges, 2)
	request.RelationTypes = nil
	request.Libraries = append(request.Libraries, f.libraries[1])
	request.Hops = 1
	multi, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, multi.Edges, 1)
	request = f.request()
	request.Mode = "path"
	request.Hops = 3
	request.Target = &types.GraphEntityInput{ID: f.ids[3]}
	path, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, path.Paths, 1)
	require.Equal(t, f.ids[:4], path.Paths[0].EntityIDs)
	source := alias.Edges[0].Sources[0]
	require.Equal(t, first, source.EntryID)
	require.Equal(t, 2026, source.PublishedAt.Year())
	require.Equal(t, 2, source.Evidence[0].Locations[0].Paragraph)
	require.Equal(t, "Speaker", source.Evidence[0].Speaker.Name)
	require.Equal(t, f.ids[1], source.Evidence[0].Speaker.EntityID)
	request.Target = &types.GraphEntityInput{ID: f.ids[4]}
	excluded, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, excluded.Paths)
	request.Libraries = append(request.Libraries, f.libraries[1])
	included, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, included.Paths, "independent library identities cannot connect paths")
	// The replica still has a source whose document was deleted in PG.
	require.NoError(t, f.db.Exec("UPDATE entries SET deleted_at=now() WHERE id=?", first).Error)
	request = f.request()
	request.Hops = 3
	deleted, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, deleted.Edges)
	require.Empty(t, deleted.Documents)
	// New notifications change global and relevant counts without changing the replica.
	for i, entity := range []string{f.ids[0], f.ids[0], f.ids[4]} {
		require.NoError(t, f.db.Create(&types.GraphPendingSync{ID: uuid.NewString(), SpaceID: 1, EntityID: entity, RelationID: "pending", RelationVersion: int64(i + 1)}).Error)
	}
	pending, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.EqualValues(t, 3, pending.PendingSync.Global)
	require.EqualValues(t, 2, pending.PendingSync.Involved)
	require.NoError(t, f.db.Model(&types.GraphPendingSync{}).Where("entity_id=? AND completed_at IS NULL", f.ids[0]).Update("completed_at", time.Now()).Error)
	pending, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.EqualValues(t, 1, pending.PendingSync.Global)
	require.Zero(t, pending.PendingSync.Involved)
}
func TestGraphSearchTruncationUnavailableAndValidation(t *testing.T) {
	t.Setenv("RELIQUARY_GRAPH_EXPANSION_LIMIT", "1")
	f := newGraphSearchFixture(t)
	f.add(t, 0, 0, 1, 0, "cooperates")
	f.add(t, 0, 0, 2, 0, "cooperates")
	f.sync(t)
	request := f.request()
	out, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, out.Edges, 1)
	require.True(t, out.Truncated)
	require.Equal(t, "expansion_limit", out.Truncations[0].Reason)
	body, _ := json.Marshal(request)
	offline := graphreplica.NewStore(config.Neo4jConfig{URI: "bolt://127.0.0.1:19498", Username: "neo4j", Password: "unused"})
	defer offline.Close()
	svc := service.NewGraphSearchService(f.db, offline)
	_, err = svc.Search(f.ctx, request)
	require.ErrorIs(t, err, graphreplica.ErrUnavailable)
	f.router.POST("/api/v1/offline-graph", handler.NewGraphSearchHandler(svc).Search)
	response := personalRequest(t, f.router, "POST", "/api/v1/offline-graph", string(body), f.key, true)
	require.Equal(t, 503, response.Code)
	require.Contains(t, response.Body.String(), "Neo4j query replica unavailable")
	for _, body := range []string{`{"libraries":[],"entity":{"id":"x"}}`, `{"libraries":["unknown"],"entity":{"id":"x"}}`, `{"libraries":["official"],"entity":{"id":"x"},"hops":4}`, `{"libraries":["official"],"entity":{"id":"x","name":"x"}}`, `{"libraries":["official"],"entity":{"id":"x"},"mode":"path"}`, `{"libraries":["official"],"entity":{"id":"x"},"unknown":true}`} {
		response := personalRequest(t, f.router, "POST", "/api/v1/graph/search", body, f.key, true)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
}
func TestGraphSearchHTTPMCPRecordsAndEvaluation(t *testing.T) {
	f := newGraphSearchFixture(t)
	first := f.add(t, 0, 0, 1, 0, "cooperates")
	f.add(t, 0, 1, 2, 0, "cooperates")
	f.sync(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "graph-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: f.server.URL + "/graph-mcp", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: mcpBearerTransport{key: f.key, base: http.DefaultTransport}, Timeout: 10 * time.Second}}, nil)
	require.NoError(t, err)
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "graph_search" {
			found = true
			require.Contains(t, tool.Description, "不调用模型")
			require.Contains(t, tool.Description, "必须指定")
			require.Contains(t, tool.Description, "最多 3")
		}
	}
	require.True(t, found)
	request := f.request()
	for _, mode := range []string{"neighbors", "path"} {
		request.Mode = mode
		request.Hops = 2
		if mode == "path" {
			request.Target = &types.GraphEntityInput{ID: f.ids[2]}
		}
		body, _ := json.Marshal(request)
		response := personalRequest(t, f.router, "POST", "/api/v1/graph/search", string(body), f.key, true)
		require.Equal(t, 200, response.Code, response.Body.String())
		var httpOut struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &httpOut))
		mcpOut, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "graph_search", Arguments: request})
		require.NoError(t, err)
		require.False(t, mcpOut.IsError, "%+v", mcpOut)
		data, _ := json.Marshal(mcpOut.StructuredContent)
		require.JSONEq(t, string(httpOut.Data), string(data))
		var wire any
		require.NoError(t, json.Unmarshal(data, &wire))
		requireEntryWireKeys(t, wire)
		require.Contains(t, string(data), `"entry_id"`)
		require.NotContains(t, string(data), `"article_id"`)
	}
	records, err := service.NewSearchRecorder(f.db).List(f.ctx, service.SearchRecordFilter{Method: "graph"})
	require.NoError(t, err)
	require.Len(t, records, 4)
	origins := []string{}
	for _, r := range records {
		origins = append(origins, r.Source)
		require.Equal(t, []string{f.library.ID}, r.Libraries)
		require.Len(t, r.Results, 2)
		require.Equal(t, 1, r.Results[0].Rank)
		require.NotEmpty(t, r.Request)
	}
	require.ElementsMatch(t, []string{"http", "http", "mcp", "mcp"}, origins)
	evaluations := service.NewEvaluationService(f.db, nil, nil)
	evaluations.GraphSearch = f.graph.EvaluationSearch
	set, err := evaluations.SaveSet(f.ctx, "", "Graph evaluation", "Real graph")
	require.NoError(t, err)
	request = f.request()
	request.Libraries = []string{f.libraries[1]}
	request.Limit = 1
	raw, _ := json.Marshal(request)
	_, err = evaluations.SaveQuestion(f.ctx, set.ID, "", types.EvaluationQuestion{Method: "graph", Status: "confirmed", Request: types.EntrySearchRequest{Query: "Graph COMPANY", Libraries: []string{f.libraries[0]}, GraphParameters: raw}, Answers: []types.EvaluationAnswer{{EntryID: first, Grade: 1}}})
	require.NoError(t, err)
	response := personalRequest(t, f.router, "POST", "/api/v1/evaluation/sets/"+set.ID+"/runs", `{"k":1}`, f.key, true)
	require.Equal(t, 200, response.Code, response.Body.String())
	var run struct {
		Data types.EvaluationRun `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &run))
	require.Equal(t, "completed", run.Data.Status)
	require.EqualValues(t, 1, run.Data.Summary.Recall)
	require.Len(t, run.Data.Results[0].Results.Results, 1)
	evaluationRecords, err := service.NewSearchRecorder(f.db).List(f.ctx, service.SearchRecordFilter{RunID: run.Data.ID})
	require.NoError(t, err)
	require.Len(t, evaluationRecords, 1)
	require.Equal(t, "graph", evaluationRecords[0].Method)
	require.Equal(t, "evaluation", evaluationRecords[0].Source)
	require.Equal(t, []string{f.libraries[0]}, evaluationRecords[0].Libraries)
	var modelCalls int64
	require.NoError(t, f.db.Table("model_calls").Count(&modelCalls).Error)
	require.Zero(t, modelCalls)
}

func TestGraphSearchDeletedSourcesAndStaleReplicaDoNotBridge(t *testing.T) {
	f := newGraphSearchFixture(t)
	first := f.add(t, 0, 0, 1, 0, "cooperates")
	f.add(t, 0, 1, 2, 0, "cooperates")
	f.sync(t)
	var source types.RelationSource
	require.NoError(t, f.db.Where("entry_id=?", first).First(&source).Error)
	var entry types.Entry
	require.NoError(t, f.db.First(&entry, "id=?", first).Error)
	entry.ID = uuid.NewString()
	require.NoError(t, f.db.Create(&entry).Error)
	second := source
	second.EntryID = entry.ID
	require.NoError(t, f.db.Create(&second).Error)
	request := f.request()
	out, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, 2, out.Edges[0].SourceCount)
	require.NoError(t, f.db.Exec("UPDATE entries SET deleted_at=now() WHERE id=?", first).Error)
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, out.Edges[0].SourceCount)
	require.Equal(t, entry.ID, out.Edges[0].Sources[0].EntryID)
	require.NoError(t, f.db.Unscoped().Delete(&types.Entry{}, "id=?", entry.ID).Error)
	request.Hops = 3
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, out.Edges)
	require.Empty(t, out.Documents)
	request.Mode = "path"
	request.Target = &types.GraphEntityInput{ID: f.ids[2]}
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, out.Paths)
}

func TestGraphSearchPathBoundsAndLimits(t *testing.T) {
	f := newGraphSearchFixture(t)
	f.add(t, 0, 0, 1, 0, "cooperates")
	f.add(t, 0, 1, 2, 0, "cooperates")
	f.add(t, 0, 2, 3, 0, "cooperates")
	f.sync(t)
	request := f.request()
	request.Mode = "path"
	request.Target = &types.GraphEntityInput{ID: f.ids[3]}
	for hop := 1; hop <= 3; hop++ {
		request.Hops = hop
		out, err := f.graph.Search(f.ctx, request)
		require.NoError(t, err)
		if hop < 3 {
			require.Empty(t, out.Paths)
		} else {
			require.Len(t, out.Paths, 1)
		}
	}
	request.Entity = types.GraphEntityInput{ID: f.ids[3]}
	request.Target = &types.GraphEntityInput{ID: f.ids[0]}
	out, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, []string{f.ids[3], f.ids[2], f.ids[1], f.ids[0]}, out.Paths[0].EntityIDs)
	request.Target = &types.GraphEntityInput{Name: "Shared"}
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "needs_selection", out.Status)
	require.Len(t, out.Candidates["target"], 2)
	// Add a second simple route and verify a path cap, preserving original directions.
	f.add(t, 0, 0, 2, 0, "supports")
	f.sync(t)
	request = f.request()
	request.Mode = "path"
	request.Hops = 3
	request.Target = &types.GraphEntityInput{ID: f.ids[3]}
	request.Limit = 1
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, out.Paths, 1)
	require.True(t, out.Truncated)
	reasons := []string{}
	for _, truncation := range out.Truncations {
		reasons = append(reasons, truncation.Reason)
	}
	require.Contains(t, reasons, "path_limit")
	request = f.request()
	request.Hops = 3
	request.Limit = 1
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, out.Edges, 1)
	require.True(t, out.Truncated)
	// A changed type in PG invalidates the obsolete replica edge before expansion.
	require.NoError(t, f.db.Model(&types.Relation{}).Where("subject_id=? AND object_id=?", f.ids[0], f.ids[1]).Update("relation_type", "supports").Error)
	request = f.request()
	request.RelationTypes = []string{"cooperates"}
	request.Hops = 3
	out, err = f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Empty(t, out.Edges)
}

func TestGraphSearchFiltersBeforeExpansionCapAndScopeValidation(t *testing.T) {
	t.Setenv("RELIQUARY_GRAPH_EXPANSION_LIMIT", "1")
	f := newGraphSearchFixture(t)
	f.add(t, 3, 0, 1, 1, "cooperates")
	f.add(t, 2, 0, 2, 0, "supports")
	f.add(t, 1, 0, 3, 0, "cooperates")
	f.sync(t)
	request := f.request()
	request.RelationTypes = []string{"cooperates"}
	out, err := f.graph.Search(f.ctx, request)
	require.NoError(t, err)
	require.Len(t, out.Edges, 1)
	require.Equal(t, f.ids[3], out.Edges[0].ObjectID)
	require.False(t, out.Truncated)
	request.Libraries = []string{"private"}
	_, err = f.graph.Search(f.ctx, request)
	require.ErrorContains(t, err, "unknown library")
	foreignContext := context.WithValue(f.ctx, types.SpaceIDContextKey, uint64(2))
	_, err = f.graph.Search(foreignContext, f.request())
	require.ErrorContains(t, err, "unknown library")
	_, err = f.graph.Search(context.Background(), f.request())
	require.ErrorContains(t, err, "workspace required")
}
