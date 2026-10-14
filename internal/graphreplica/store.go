// Package graphreplica is the only Neo4j writer. Business transactions use PG.
package graphreplica

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	neo4jconfig "github.com/neo4j/neo4j-go-driver/v6/neo4j/config"
	"reliquary/internal/config"
)

var ErrUnavailable = errors.New("Neo4j query replica unavailable; PostgreSQL intake remains available")
var ErrVersionConflict = errors.New("Neo4j relation version is newer than PostgreSQL snapshot")

// Store owns a dedicated Neo4j database. Namespace is only for isolated tests.
// No business service receives a writable driver.
type Store struct {
	driver    neo4j.Driver
	namespace string
	initErr   error
	mu        sync.Mutex
	schema    bool
}

func NewStore(c config.Neo4jConfig) *Store {
	d, err := neo4j.NewDriver(c.URI, neo4j.BasicAuth(c.Username, c.Password, ""), func(c *neo4jconfig.Config) {
		c.SocketConnectTimeout = 2 * time.Second
		c.ConnectionAcquisitionTimeout = 3 * time.Second
		c.MaxTransactionRetryTime = 2 * time.Second
	})
	return &Store{driver: d, initErr: err}
}
func (s *Store) Close() error {
	if s.driver == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.driver.Close(ctx)
}
func (s *Store) Available(ctx context.Context) error {
	if s == nil {
		return ErrUnavailable
	}
	if s.initErr != nil || s.driver == nil {
		return fmt.Errorf("%w: invalid connection configuration", ErrUnavailable)
	}
	if err := s.driver.VerifyConnectivity(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}
func (s *Store) ensure(ctx context.Context) error {
	if err := s.Available(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.schema {
		return nil
	}
	_, err := neo4j.ExecuteQuery(ctx, s.driver, `CREATE CONSTRAINT reliquary_entity_identity IF NOT EXISTS FOR (e:Entity) REQUIRE (e.namespace,e.space_id,e.id) IS UNIQUE`, nil, neo4j.EagerResultTransformer)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	s.schema = true
	return nil
}
func run(ctx context.Context, tx neo4j.ManagedTransaction, q string, p map[string]any) error {
	r, err := tx.Run(ctx, q, p)
	if err != nil {
		return err
	}
	_, err = r.Consume(ctx)
	return err
}
func (s *Store) write(ctx context.Context, fn func(neo4j.ManagedTransaction) error) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) { return nil, fn(tx) })
	if err != nil && !errors.Is(err, ErrVersionConflict) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return err
}
func (s *Store) parameters(space uint64) map[string]any {
	return map[string]any{"namespace": s.namespace, "space": int64(space)}
}
func nodeParams(n Node, namespace string) map[string]any {
	return map[string]any{"namespace": namespace, "space_id": int64(n.SpaceID), "id": n.ID, "canonical_name": n.CanonicalName, "entity_type": n.EntityType}
}
func edgeParams(e Edge, namespace string) map[string]any {
	return map[string]any{"namespace": namespace, "space_id": int64(e.SpaceID), "id": e.ID, "subject_id": e.SubjectID, "object_id": e.ObjectID, "relation_type": e.RelationType, "library_id": e.LibraryID, "source_count": e.SourceCount, "version": e.Version}
}
func (s *Store) put(ctx context.Context, tx neo4j.ManagedTransaction, snapshot Snapshot, force bool) error {
	nodes := []any{}
	for _, n := range snapshot.Nodes {
		nodes = append(nodes, nodeParams(n, s.namespace))
	}
	if err := run(ctx, tx, `UNWIND $nodes AS row MERGE (e:Entity {namespace:row.namespace,space_id:row.space_id,id:row.id}) SET e = row`, map[string]any{"nodes": nodes}); err != nil {
		return err
	}
	edges := []any{}
	for _, e := range snapshot.Edges {
		edges = append(edges, edgeParams(e, s.namespace))
	}
	result, err := tx.Run(ctx, `UNWIND $edges AS row
 MATCH (a:Entity {namespace:row.namespace,space_id:row.space_id,id:row.subject_id}), (b:Entity {namespace:row.namespace,space_id:row.space_id,id:row.object_id})
 MERGE (a)-[r:RELATION {id:row.id}]->(b)
 WITH r,row WHERE $force OR coalesce(r.version,0)<=row.version
 SET r = row RETURN count(r) AS updated`, map[string]any{"edges": edges, "force": force})
	if err != nil {
		return err
	}
	record, err := result.Single(ctx)
	if err != nil {
		return err
	}
	if record.Values[0].(int64) != int64(len(edges)) {
		return ErrVersionConflict
	}
	return nil
}
func (s *Store) apply(ctx context.Context, space uint64, entity string, snapshot Snapshot) error {
	return s.write(ctx, func(tx neo4j.ManagedTransaction) error {
		// Validate before deleting a previous endpoint: a merge can keep the ID
		// while moving the edge, and deletion must not bypass its version fence.
		edges := []any{}
		for _, edge := range snapshot.Edges {
			edges = append(edges, edgeParams(edge, s.namespace))
		}
		versions, err := tx.Run(ctx, `UNWIND $edges AS row MATCH ()-[r:RELATION {id:row.id}]->() WHERE r.namespace=row.namespace AND r.space_id=row.space_id AND r.version>row.version RETURN count(r)`, map[string]any{"edges": edges})
		if err != nil {
			return err
		}
		record, err := versions.Single(ctx)
		if err != nil {
			return err
		}
		if record.Values[0].(int64) > 0 {
			return ErrVersionConflict
		}
		p := s.parameters(space)
		p["entity"] = entity
		ids := []string{}
		for _, e := range snapshot.Edges {
			ids = append(ids, e.ID)
		}
		p["ids"] = ids
		// The PG snapshot owns both incoming and outgoing edges, including tombstones.
		if err := run(ctx, tx, `MATCH (e:Entity {namespace:$namespace,space_id:$space,id:$entity})-[r]-() WHERE r.id IS NULL OR NOT r.id IN $ids DELETE r`, p); err != nil {
			return err
		}
		for _, edge := range snapshot.Edges {
			p["id"] = edge.ID
			p["subject"] = edge.SubjectID
			p["object"] = edge.ObjectID
			if err := run(ctx, tx, `MATCH (a:Entity {namespace:$namespace,space_id:$space})-[r:RELATION {id:$id}]->(b) WHERE a.id<>$subject OR b.id<>$object DELETE r`, p); err != nil {
				return err
			}
		}
		if err := s.put(ctx, tx, snapshot, false); err != nil {
			return err
		}
		exists := false
		for _, n := range snapshot.Nodes {
			if n.ID == entity {
				exists = true
			}
		}
		if !exists {
			return run(ctx, tx, `MATCH (e:Entity {namespace:$namespace,space_id:$space,id:$entity}) DETACH DELETE e`, p)
		}
		return nil
	})
}
func (s *Store) replace(ctx context.Context, snapshot Snapshot) error {
	return s.write(ctx, func(tx neo4j.ManagedTransaction) error {
		q := `MATCH (n) DETACH DELETE n`
		p := map[string]any{}
		if s.namespace != "" {
			q = `MATCH (n {namespace:$namespace}) DETACH DELETE n`
			p["namespace"] = s.namespace
		}
		if err := run(ctx, tx, q, p); err != nil {
			return err
		}
		return s.put(ctx, tx, snapshot, true)
	})
}
