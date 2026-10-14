package graphreplica

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// Expand reads both directions. Every relationship and endpoint is scoped,
// before ordering and limiting. The extra row detects incomplete expansion.
func (s *Store) Expand(ctx context.Context, space uint64, entity string, libraries, relationTypes []string, limit int) ([]Edge, bool, error) {
	if s == nil || s.driver == nil || s.initErr != nil {
		return nil, false, ErrUnavailable
	}
	if relationTypes == nil {
		relationTypes = []string{}
	}
	p := s.parameters(space)
	p["entity"], p["libraries"], p["types"], p["limit"] = entity, libraries, relationTypes, limit+1
	result, err := neo4j.ExecuteQuery(ctx, s.driver, `MATCH (a:Entity {namespace:$namespace,space_id:$space,id:$entity})-[r:RELATION]-(b:Entity {namespace:$namespace,space_id:$space})
 WHERE r.namespace=$namespace AND r.space_id=$space AND r.library_id IN $libraries
 AND (size($types)=0 OR r.relation_type IN $types)
 RETURN r.id,r.subject_id,r.object_id,r.library_id,r.relation_type,r.version,r.source_count
 ORDER BY r.id LIMIT $limit`, p, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithReadersRouting())
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	edges := []Edge{}
	for _, row := range result.Records {
		edges = append(edges, Edge{SpaceID: space, ID: str(row.Values[0]), SubjectID: str(row.Values[1]), ObjectID: str(row.Values[2]), LibraryID: str(row.Values[3]), RelationType: str(row.Values[4]), Version: integer(row.Values[5]), SourceCount: integer(row.Values[6])})
	}
	truncated := len(edges) > limit
	if truncated {
		edges = edges[:limit]
	}
	return edges, truncated, nil
}
