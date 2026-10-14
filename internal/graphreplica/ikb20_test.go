package graphreplica

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
)

func TestIKB20ReadOnlyVerificationReportsEdgeAndSourceCountTampering(t *testing.T) {
	s, e := fixture(t)
	ctx := context.Background()
	publish(t, s, e)
	_, err := s.Rebuild(ctx)
	require.NoError(t, err)
	clean, err := s.Verify(ctx)
	require.NoError(t, err)
	require.Empty(t, clean.Differences)
	_, err = neo4j.ExecuteQuery(ctx, s.store.driver, `MATCH ()-[r:RELATION {namespace:$namespace}]->() SET r.source_count=123`, map[string]any{"namespace": s.store.namespace}, neo4j.EagerResultTransformer)
	require.NoError(t, err)
	tampered, err := s.Verify(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, tampered.Differences)
	require.Contains(t, tampered.Differences[0], "relations row")
	require.Contains(t, tampered.Differences[0], `"source_count":123`)
	still, err := s.Verify(ctx)
	require.NoError(t, err)
	require.Equal(t, tampered.Neo4jHash, still.Neo4jHash, "verify must not silently repair")
	_, err = s.Rebuild(ctx)
	require.NoError(t, err)
	require.NoError(t, s.db.Exec("DELETE FROM relation_sources").Error)
	tampered, err = s.Verify(ctx)
	require.NoError(t, err)
	require.Contains(t, tampered.Differences[0], `"source_count":0`)
}

func TestIKB20DrillNodesAreInvisibleToProductionInspection(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	oldNamespace := s.store.namespace
	s.store.namespace = "reliquary_drill_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	t.Cleanup(func() { s.store.namespace = oldNamespace })
	defer func() { require.NoError(t, s.store.ClearDrill(ctx)) }()
	marker := "only-in-drill-" + s.store.namespace
	require.NoError(t, s.store.replace(ctx, Snapshot{Nodes: []Node{{SpaceID: 1, ID: marker, CanonicalName: marker, EntityType: "company"}}, Edges: []Edge{}}))
	production := &Store{driver: s.store.driver}
	rows, err := production.snapshot(ctx)
	require.NoError(t, err)
	for _, row := range rows.Nodes {
		require.NotEqual(t, marker, row.ID)
	}
	isolated, err := s.store.snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, isolated.Nodes, 1)
	require.Equal(t, marker, isolated.Nodes[0].ID)
}

func TestIKB20DrillScopeRequiresGeneratedIdentity(t *testing.T) {
	_, err := NewDrillStore(config.Neo4jConfig{}, "production")
	require.Error(t, err)
	s, err := NewDrillStore(config.Neo4jConfig{}, "reliquary_drill_0123456789abcdef")
	require.NoError(t, err)
	defer s.Close()
	require.Equal(t, "reliquary_drill_0123456789abcdef", s.namespace)
}
