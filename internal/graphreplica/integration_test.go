package graphreplica

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/graph"
	"reliquary/internal/config"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Dedicated database: other destructive recovery tests reserve DB 13 and 15.
const graphTestRedisDB = 9

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }
func fixture(t *testing.T) (*Service, *types.Entry) {
	t.Helper()
	if os.Getenv("RELIQUARY_TEST_NEO4J_URI") == "" {
		t.Skip("set RELIQUARY_TEST_NEO4J_URI, RELIQUARY_TEST_NEO4J_USERNAME and RELIQUARY_TEST_NEO4J_PASSWORD to run real Neo4j tests")
	}
	db := pgtest.Open(t)
	store := NewStore(config.Neo4jConfig{URI: os.Getenv("RELIQUARY_TEST_NEO4J_URI"), Username: os.Getenv("RELIQUARY_TEST_NEO4J_USERNAME"), Password: os.Getenv("RELIQUARY_TEST_NEO4J_PASSWORD")})
	store.namespace = uuid.NewString()
	require.NoError(t, store.ensure(context.Background()))
	t.Cleanup(func() {
		require.NoError(t, store.replace(context.Background(), Snapshot{Nodes: []Node{}, Edges: []Edge{}}))
		require.NoError(t, store.Close())
	})
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Graph tests"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "library", SpaceID: 1, Name: "Mine", Template: types.TemplateOwn, Directory: "personal", SystemRole: "personal"}).Error)
	require.NoError(t, db.Create(&types.LibraryContentType{LibraryID: "library", Name: "text", Enabled: true, ExtractGraph: true}).Error)
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "library", Name: "cooperates", Enabled: true}).Error)
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, db.Create(&types.Entity{ID: id, SpaceID: 1, LibraryID: "library", CanonicalName: strings.ToUpper(id), NormalizedName: id, EntityType: "company", CreatedAt: time.Now(), UpdatedAt: time.Now()}).Error)
	}
	entry := &types.Entry{ID: "entry", SpaceID: 1, LibraryID: "library", Title: "Source", Type: types.EntryTypeManual, ContentType: "text", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, db.Create(entry).Error)
	return NewService(db, store, nil), entry
}
func publish(t *testing.T, s *Service, e *types.Entry) {
	t.Helper()
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
		if err := graph.Lock(tx, 1); err != nil {
			return err
		}
		return graph.ReplaceTx(tx, e, &types.EntryProcessingOutput{RunID: "run", BodyHash: "hash", Relations: []types.ProcessingRelation{{SubjectID: "a", ObjectID: "b", Type: "cooperates", Evidence: []types.RelationEvidence{{Locations: []types.SourceLocation{{Start: 0, End: 1}}}}}}})
	}))
}
func syncAll(t *testing.T, s *Service) {
	t.Helper()
	var entities []string
	require.NoError(t, s.db.Table("graph_pending_sync").Where("completed_at IS NULL").Distinct().Pluck("entity_id", &entities).Error)
	for _, id := range entities {
		require.NoError(t, s.Sync(context.Background(), 1, id))
	}
}
func consistent(t *testing.T, s *Service) {
	t.Helper()
	expected, err := readPG(s.db, 0, "")
	require.NoError(t, err)
	actual, err := s.store.snapshot(context.Background())
	require.NoError(t, err)
	// Sync projects notified entities; daily rebuild also includes isolated entities.
	linked := map[string]bool{}
	for _, e := range expected.Edges {
		linked[e.SubjectID] = true
		linked[e.ObjectID] = true
	}
	subset := []Node{}
	for _, n := range expected.Nodes {
		if linked[n.ID] {
			subset = append(subset, n)
		}
	}
	if len(actual.Nodes) < len(expected.Nodes) {
		expected.Nodes = subset
	}
	require.Equal(t, expected.Hash(), actual.Hash())
}
func TestRelationsSourcesDeletionMergeAndIdempotence(t *testing.T) {
	s, e := fixture(t)
	publish(t, s, e)
	syncAll(t, s)
	consistent(t, s)
	first, err := s.store.snapshot(context.Background())
	require.NoError(t, err)
	// A duplicate notification rereads the same version and writes the same graph.
	var row types.Relation
	require.NoError(t, s.db.First(&row).Error)
	require.NoError(t, s.db.Create(&types.GraphPendingSync{ID: uuid.NewString(), SpaceID: 1, EntityID: "a", RelationID: row.ID, RelationVersion: row.Version, CreatedAt: time.Now()}).Error)
	syncAll(t, s)
	again, err := s.store.snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, first.Hash(), again.Hash())
	e2 := *e
	e2.ID = "entry2"
	require.NoError(t, s.db.Create(&e2).Error)
	publish(t, s, &e2)
	syncAll(t, s)
	consistent(t, s)
	actual, err := s.store.snapshot(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, actual.Edges[0].SourceCount)
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
		if err := graph.Lock(tx, 1); err != nil {
			return err
		}
		return graph.WithdrawTx(tx, []string{e.ID})
	}))
	syncAll(t, s)
	consistent(t, s)
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
		if err := graph.MergeEntityTx(tx, 1, "a", "c"); err != nil {
			return err
		}
		return tx.Model(&types.Entity{}).Where("id='a'").Update("merged_into_id", "c").Error
	}))
	syncAll(t, s)
	consistent(t, s)
	require.NoError(t, s.db.First(&row).Error)
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error { _, err := graph.DeleteRelationTx(tx, 1, row.ID, "user"); return err }))
	syncAll(t, s)
	actual, err = s.store.snapshot(context.Background())
	require.NoError(t, err)
	require.Empty(t, actual.Edges)
	var unfinished int64
	require.NoError(t, s.db.Table("graph_pending_sync").Where("completed_at IS NULL").Count(&unfinished).Error)
	require.Zero(t, unfinished)
}
func TestLateOldTaskVersionFenceAndSerialExecution(t *testing.T) {
	s, e := fixture(t)
	publish(t, s, e)
	syncAll(t, s)
	old, err := readPG(s.db, 1, "a")
	require.NoError(t, err)
	e2 := *e
	e2.ID = "entry2"
	require.NoError(t, s.db.Create(&e2).Error)
	publish(t, s, &e2)
	syncAll(t, s)
	require.ErrorIs(t, s.store.apply(context.Background(), 1, "a", old), ErrVersionConflict)
	// Old payload versions are deliberately absent: the entity notification is read afresh.
	require.NoError(t, s.db.Create(&types.GraphPendingSync{ID: uuid.NewString(), SpaceID: 1, EntityID: "a", RelationID: old.Edges[0].ID, RelationVersion: 1, CreatedAt: time.Now()}).Error)
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- s.Sync(context.Background(), 1, "a") }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	consistent(t, s)
	beforeMerge, err := readPG(s.db, 1, "a")
	require.NoError(t, err)
	require.NoError(t, s.db.Transaction(func(tx *gorm.DB) error {
		if err := graph.MergeEntityTx(tx, 1, "a", "c"); err != nil {
			return err
		}
		return tx.Model(&types.Entity{}).Where("id='a'").Update("merged_into_id", "c").Error
	}))
	syncAll(t, s)
	require.ErrorIs(t, s.store.apply(context.Background(), 1, "a", beforeMerge), ErrVersionConflict)
	consistent(t, s)
}
func TestDailyCheckRepairsTamperingAndFullRebuild(t *testing.T) {
	s, e := fixture(t)
	publish(t, s, e)
	result, err := s.Rebuild(context.Background())
	require.NoError(t, err)
	require.Equal(t, result.PGHash, result.Neo4jHash)
	_, err = neo4j.ExecuteQuery(context.Background(), s.store.driver, `MATCH (a:Entity {namespace:$namespace,id:'a'})-[r]->(b) SET r.source_count=998,r.version=999,a.canonical_name='tampered' CREATE (b)-[:BROKEN {id:'rogue'}]->(a)`, map[string]any{"namespace": s.store.namespace}, neo4j.EagerResultTransformer)
	require.NoError(t, err)
	result, err = s.Check(context.Background())
	require.NoError(t, err)
	require.True(t, result.Repaired)
	require.NotEqual(t, result.PGHash, result.Neo4jHash)
	consistent(t, s)
	result, err = s.Check(context.Background())
	require.NoError(t, err)
	require.False(t, result.Repaired)
	// Malformed data must count even when it lacks the expected node label or ID.
	_, err = neo4j.ExecuteQuery(context.Background(), s.store.driver, `MATCH (a:Entity {namespace:$namespace,id:'a'}) CREATE (g:Unknown {namespace:$namespace,id:'ghost'}) CREATE (g)-[:BROKEN]->(a)`, map[string]any{"namespace": s.store.namespace}, neo4j.EagerResultTransformer)
	require.NoError(t, err)
	result, err = s.Check(context.Background())
	require.NoError(t, err)
	require.True(t, result.Repaired)
	require.Equal(t, 2, result.Neo4jEdges)
	consistent(t, s)
	var audit types.OperationLog
	require.NoError(t, s.db.Where("operation_type='graph_check'").Order("created_at desc").First(&audit).Error)
	require.Contains(t, string(audit.After), "pg_edges")
	// Offline writes never prevent PG relation commits, nor confirm their notifications.
	offline := NewStore(config.Neo4jConfig{URI: "bolt://127.0.0.1:19399", Username: "neo4j", Password: "unused"})
	defer offline.Close()
	unavailable := NewService(s.db, offline, nil)
	publish(t, s, e)
	require.ErrorIs(t, unavailable.Sync(context.Background(), 1, "a"), ErrUnavailable)
	syncAll(t, s)
	consistent(t, s)
}
func newQueue(t *testing.T, s *Service) *queue.Client {
	t.Helper()
	addr := os.Getenv("RELIQUARY_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set RELIQUARY_TEST_REDIS_ADDR for real Redis graph tests")
	}
	opt := asynq.RedisClientOpt{Addr: addr, DB: graphTestRedisDB, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second}
	client := queue.NewClientWithRedis(s.db, opt)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		var name string
		require.NoError(t, s.db.Raw("SELECT current_database()").Scan(&name).Error)
		inspector := asynq.NewInspector(opt)
		defer inspector.Close()
		queues, err := inspector.Queues()
		require.NoError(t, err)
		for _, q := range queues {
			if strings.HasPrefix(q, "reliquary:"+name+":") {
				require.NoError(t, inspector.DeleteQueue(q, true))
			}
		}
	})
	return client
}
func startQueue(t *testing.T, s *Service) {
	t.Helper()
	mux := queue.NewServeMux()
	mux.HandleFunc(TypeSync, s.HandleSync)
	mux.HandleFunc(TypeCheck, s.HandleCheck)
	mux.HandleFunc(TypeRebuild, s.HandleRebuild)
	require.NoError(t, s.queue.Start(context.Background(), mux, queue.Config{CheckInterval: 40 * time.Millisecond, DeliveryTimeout: 3 * time.Second, RetryDelayFunc: func(int, error, *queue.Task) time.Duration { return 80 * time.Millisecond }}))
}
func waitSynced(t *testing.T, s *Service) {
	t.Helper()
	require.Eventually(t, func() bool {
		var n int64
		return s.db.Table("graph_pending_sync").Where("completed_at IS NULL").Count(&n).Error == nil && n == 0
	}, 30*time.Second, 100*time.Millisecond)
}
func TestRedisFlushStartupReplayAndPeriodicNotificationSweep(t *testing.T) {
	s, e := fixture(t)
	require.NoError(t, s.db.Exec(`INSERT INTO search_records(id,space_id,source,method,query,libraries,request,results) VALUES('merge-search',1,'http','ordinary','retained query','["library"]','{}','[]')`).Error)
	var originalSearch string
	require.NoError(t, s.db.Raw("SELECT to_jsonb(r)::text FROM search_records r WHERE id='merge-search'").Scan(&originalSearch).Error)
	s.queue = newQueue(t, s)
	publish(t, s, e)
	require.NoError(t, s.Sweep(context.Background(), time.Now()))
	redisClient := redis.NewClient(&redis.Options{Addr: os.Getenv("RELIQUARY_TEST_REDIS_ADDR"), DB: graphTestRedisDB})
	defer redisClient.Close()
	require.NoError(t, redisClient.FlushDB(context.Background()).Err())
	startQueue(t, s)
	stop := s.Start(80 * time.Millisecond)
	defer stop()
	waitSynced(t, s)
	consistent(t, s)
	// Periodic sweep discovers new PG notifications without an immediate business wake.
	e2 := *e
	e2.ID = "entry2"
	require.NoError(t, s.db.Create(&e2).Error)
	publish(t, s, &e2)
	waitSynced(t, s)
	consistent(t, s)
	info, err := s.EnqueueRebuild(context.Background())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		op, err := s.queue.GetTaskInfo(context.Background(), types.QueueMaintenance, info.ID)
		return err == nil && op.State == queue.TaskStateCompleted
	}, 30*time.Second, 100*time.Millisecond)
	var n int64
	require.NoError(t, s.db.Model(&queue.Job{}).Where("task_type=?", TypeCheck).Count(&n).Error)
	require.EqualValues(t, 1, n)
	var calls int64
	require.NoError(t, s.db.Table("model_calls").Count(&calls).Error)
	require.Zero(t, calls)
	var afterSearch string
	require.NoError(t, s.db.Raw("SELECT to_jsonb(r)::text FROM search_records r WHERE id='merge-search'").Scan(&afterSearch).Error)
	require.JSONEq(t, originalSearch, afterSearch)
	for _, operation := range []string{"graph_check", "graph_rebuild"} {
		var count int64
		require.NoError(t, s.db.Table("operation_logs").Where("operation_type=?", operation).Count(&count).Error)
		require.Positive(t, count)
	}
	for _, task := range []string{TypeSync, TypeCheck, TypeRebuild} {
		require.Eventually(t, func() bool {
			var count int64
			return s.db.Model(&queue.Job{}).Where("task_type=? AND state=?", task, queue.TaskStateCompleted).Count(&count).Error == nil && count > 0
		}, 30*time.Second, 100*time.Millisecond)
	}
}
func TestStoppedNeo4jChangesReplayAfterRecovery(t *testing.T) {
	container := os.Getenv("RELIQUARY_TEST_NEO4J_CONTAINER")
	if container != "reliquary-test-neo4j" {
		t.Skip("set RELIQUARY_TEST_NEO4J_CONTAINER=reliquary-test-neo4j to exercise container outage")
	}
	s, e := fixture(t)
	s.queue = newQueue(t, s)
	startQueue(t, s)
	stop := s.Start(80 * time.Millisecond)
	defer stop()
	publish(t, s, e)
	waitSynced(t, s)
	output, err := exec.Command("docker", "stop", container).CombinedOutput()
	require.NoError(t, err, string(output))
	// Always recover the retained test container, even after an assertion fails.
	defer func() {
		output, err := exec.Command("docker", "start", container).CombinedOutput()
		require.NoError(t, err, string(output))
		require.Eventually(t, func() bool { return s.store.Available(context.Background()) == nil }, 60*time.Second, time.Second)
	}()
	e2 := *e
	e2.ID = "entry2"
	require.NoError(t, s.db.Create(&e2).Error)
	publish(t, s, &e2)
	require.Eventually(t, func() bool {
		var n int64
		s.db.Model(&queue.Job{}).Where("task_type=? AND fail_count>0", TypeSync).Count(&n)
		return n > 0
	}, 20*time.Second, 100*time.Millisecond)
	var n int64
	require.NoError(t, s.db.Table("graph_pending_sync").Where("completed_at IS NULL").Count(&n).Error)
	require.Positive(t, n)
	output, err = exec.Command("docker", "start", container).CombinedOutput()
	require.NoError(t, err, string(output))
	// Container startup must not consume the queue recovery deadline.
	require.Eventually(t, func() bool { return s.store.Available(context.Background()) == nil }, 60*time.Second, time.Second)
	waitSynced(t, s)
	consistent(t, s)
	var op queue.Job
	require.NoError(t, s.db.Where("task_type=? AND state=? AND fail_count>0", TypeSync, queue.TaskStateCompleted).First(&op).Error)
	payload := map[string]any{}
	require.NoError(t, json.Unmarshal(op.Payload, &payload))
	require.Contains(t, payload, "entity_id")
}

func TestLargeBacklogSyncAndRebuildAcknowledgement(t *testing.T) {
	s, e := fixture(t)
	publish(t, s, e)
	var relation types.Relation
	require.NoError(t, s.db.First(&relation).Error)
	backlog := []types.GraphPendingSync{}
	for i := 0; i < 2001; i++ {
		backlog = append(backlog, types.GraphPendingSync{ID: uuid.NewString(), SpaceID: 1, EntityID: "a", RelationID: relation.ID, RelationVersion: relation.Version, CreatedAt: time.Now()})
	}
	require.NoError(t, s.db.CreateInBatches(backlog, 200).Error)
	require.NoError(t, s.Sync(context.Background(), 1, "a"))
	var pending int64
	require.NoError(t, s.db.Table("graph_pending_sync").Where("entity_id='a' AND completed_at IS NULL").Count(&pending).Error)
	require.Zero(t, pending)
	backlog = []types.GraphPendingSync{}
	for i := 0; i < 2001; i++ {
		backlog = append(backlog, types.GraphPendingSync{ID: uuid.NewString(), SpaceID: 1, EntityID: "gone", RelationID: uuid.NewString(), RelationVersion: 2, Deleted: true, CreatedAt: time.Now()})
	}
	require.NoError(t, s.db.CreateInBatches(backlog, 200).Error)
	result, err := s.Rebuild(context.Background())
	require.NoError(t, err)
	require.Equal(t, result.PGHash, result.Neo4jHash)
	require.NoError(t, s.db.Table("graph_pending_sync").Where("completed_at IS NULL").Count(&pending).Error)
	require.Zero(t, pending)
}

// Managed-library contract: the PostgreSQL projection includes manually
// maintained edges without sources, but hides archived endpoints.
func TestManagedSnapshotExcludesArchivedEndpoints(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Managed snapshot"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "managed", SpaceID: 1, Name: "Managed", Directory: "managed", EntityMode: types.EntityModeManaged}).Error)
	require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "managed", Name: "belongs_to", Enabled: true}).Error)
	require.NoError(t, db.Create(&[]types.Entity{
		{ID: "child", SpaceID: 1, LibraryID: "managed", CanonicalName: "Child", NormalizedName: "child", EntityType: "product"},
		{ID: "parent", SpaceID: 1, LibraryID: "managed", CanonicalName: "Parent", NormalizedName: "parent", EntityType: "product"},
	}).Error)
	require.NoError(t, db.Create(&types.Relation{ID: "managed-edge", SpaceID: 1, LibraryID: "managed", SubjectID: "child", ObjectID: "parent", RelationType: "belongs_to", Version: 1}).Error)
	before, err := readPG(db, 0, "")
	require.NoError(t, err)
	require.Len(t, before.Nodes, 2)
	require.Len(t, before.Edges, 1)
	require.Zero(t, before.Edges[0].SourceCount)

	require.NoError(t, db.Model(&types.Entity{}).Where("id=?", "child").Update("archived_at", time.Now()).Error)
	archived, err := readPG(db, 0, "")
	require.NoError(t, err)
	require.Len(t, archived.Nodes, 1)
	require.Equal(t, "parent", archived.Nodes[0].ID)
	require.Empty(t, archived.Edges)

	require.NoError(t, db.Model(&types.Entity{}).Where("id=?", "child").Update("archived_at", nil).Error)
	restored, err := readPG(db, 0, "")
	require.NoError(t, err)
	require.Equal(t, before.Hash(), restored.Hash())
}
