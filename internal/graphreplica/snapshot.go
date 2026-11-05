package graphreplica

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"gorm.io/gorm"
)

type Node struct {
	SpaceID       uint64 `json:"space_id"`
	ID            string `json:"id"`
	CanonicalName string `json:"canonical_name"`
	EntityType    string `json:"entity_type"`
}
type Edge struct {
	SpaceID      uint64 `json:"space_id"`
	ID           string `json:"id"`
	SubjectID    string `json:"subject_id"`
	ObjectID     string `json:"object_id"`
	RelationType string `json:"relation_type"`
	LibraryID    string `json:"library_id"`
	SourceCount  int64  `json:"source_count"`
	Version      int64  `json:"version"`
}
type Snapshot struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

func (s Snapshot) Hash() string {
	sort.Slice(s.Nodes, func(i, j int) bool {
		return fmt.Sprint(s.Nodes[i].SpaceID, s.Nodes[i].ID) < fmt.Sprint(s.Nodes[j].SpaceID, s.Nodes[j].ID)
	})
	sort.Slice(s.Edges, func(i, j int) bool { return s.Edges[i].ID < s.Edges[j].ID })
	data, _ := json.Marshal(s)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func readPG(tx *gorm.DB, space uint64, entity string) (Snapshot, error) {
	s := Snapshot{Nodes: []Node{}, Edges: []Edge{}}
	edges := tx.Table("relations r").Select("r.space_id,r.id,r.subject_id,r.object_id,r.relation_type,r.library_id,r.version,count(src.entry_id) AS source_count").Joins("LEFT JOIN relation_sources src ON src.relation_id=r.id").Group("r.id").Where("EXISTS (SELECT 1 FROM entities e WHERE e.id=r.subject_id AND e.archived_at IS NULL) AND EXISTS (SELECT 1 FROM entities e WHERE e.id=r.object_id AND e.archived_at IS NULL)")
	if entity != "" {
		edges = edges.Where("r.space_id=? AND (r.subject_id=? OR r.object_id=?)", space, entity, entity)
	}
	if err := edges.Scan(&s.Edges).Error; err != nil {
		return s, err
	}
	nodes := tx.Table("entities").Select("space_id,id,canonical_name,entity_type").Where("(merged_into_id='' OR merged_into_id IS NULL) AND archived_at IS NULL")
	if entity != "" {
		ids := []string{entity}
		for _, e := range s.Edges {
			ids = append(ids, e.SubjectID, e.ObjectID)
		}
		nodes = nodes.Where("space_id=? AND id IN ?", space, ids)
	}
	if err := nodes.Scan(&s.Nodes).Error; err != nil {
		return s, err
	}
	return s, nil
}

// snapshot is internal maintenance inspection, not the graph retrieval API.
func (s *Store) snapshot(ctx context.Context) (Snapshot, error) {
	result := Snapshot{Nodes: []Node{}, Edges: []Edge{}}
	nodes, err := neo4j.ExecuteQuery(ctx, s.driver, `MATCH (n) WHERE ($namespace<>'' AND n.namespace=$namespace) OR ($namespace='' AND NOT coalesce(n.namespace =~ 'reliquary_drill_[a-f0-9]{16}',false)) RETURN n.space_id,n.id,n.canonical_name,n.entity_type,labels(n)`, map[string]any{"namespace": s.namespace}, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithReadersRouting())
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	for _, r := range nodes.Records {
		kind := str(r.Values[3])
		labels, ok := r.Values[4].([]any)
		if !ok || len(labels) != 1 || str(labels[0]) != "Entity" {
			kind = "invalid:labels:" + kind
		}
		result.Nodes = append(result.Nodes, Node{SpaceID: uint64(integer(r.Values[0])), ID: str(r.Values[1]), CanonicalName: str(r.Values[2]), EntityType: kind})
	}
	edges, err := neo4j.ExecuteQuery(ctx, s.driver, `MATCH (a)-[r]->(b) WHERE ($namespace<>'' AND (a.namespace=$namespace OR b.namespace=$namespace)) OR ($namespace='' AND NOT (coalesce(a.namespace =~ 'reliquary_drill_[a-f0-9]{16}',false) AND coalesce(b.namespace =~ 'reliquary_drill_[a-f0-9]{16}',false))) RETURN r.space_id,r.id,a.id,b.id,r.relation_type,r.library_id,r.source_count,r.version,type(r),r.subject_id,r.object_id,r.namespace`, map[string]any{"namespace": s.namespace}, neo4j.EagerResultTransformer, neo4j.ExecuteQueryWithReadersRouting())
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	for _, r := range edges.Records {
		kind := str(r.Values[4])
		if str(r.Values[8]) != "RELATION" {
			kind = "invalid:" + str(r.Values[8]) + ":" + kind
		}
		if str(r.Values[9]) != str(r.Values[2]) || str(r.Values[10]) != str(r.Values[3]) || str(r.Values[11]) != s.namespace {
			kind = "invalid:identity:" + kind
		}
		result.Edges = append(result.Edges, Edge{SpaceID: uint64(integer(r.Values[0])), ID: str(r.Values[1]), SubjectID: str(r.Values[2]), ObjectID: str(r.Values[3]), RelationType: kind, LibraryID: str(r.Values[5]), SourceCount: integer(r.Values[6]), Version: integer(r.Values[7])})
	}
	return result, nil
}
func integer(value any) int64 { v, _ := value.(int64); return v }
func str(value any) string    { v, _ := value.(string); return v }
