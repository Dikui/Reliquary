package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/graphreplica"
	"reliquary/internal/types"
)

type GraphSearchService struct {
	db             *gorm.DB
	store          *graphreplica.Store
	expansionLimit int
}

func NewGraphSearchService(db *gorm.DB, store *graphreplica.Store) *GraphSearchService {
	limit := 100
	if raw := os.Getenv("RELIQUARY_GRAPH_EXPANSION_LIMIT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000 {
			panic("RELIQUARY_GRAPH_EXPANSION_LIMIT must be 1 to 1000")
		}
		limit = n
	}
	return &GraphSearchService{db: db, store: store, expansionLimit: limit}
}
func validateGraphRequest(r *types.GraphSearchRequest) error {
	if len(r.Libraries) < 1 || len(r.Libraries) > 100 {
		return apperrors.NewBadRequestError("libraries requires 1 to 100 library IDs or directories")
	}
	if r.Mode == "" {
		r.Mode = "neighbors"
	}
	if r.Mode != "neighbors" && r.Mode != "path" {
		return apperrors.NewBadRequestError("mode must be neighbors or path")
	}
	if r.Hops == 0 {
		r.Hops = 1
	}
	if r.Hops < 1 || r.Hops > 3 {
		return apperrors.NewBadRequestError("hops must be 1 to 3")
	}
	if r.Limit == 0 {
		r.Limit = 20
	}
	if r.Limit < 1 || r.Limit > 100 {
		return apperrors.NewBadRequestError("limit must be 1 to 100")
	}
	valid := func(e types.GraphEntityInput) bool {
		return (strings.TrimSpace(e.ID) != "") != (strings.TrimSpace(e.Name) != "") && len(e.ID) <= 512 && len(e.Name) <= 2048
	}
	if !valid(r.Entity) {
		return apperrors.NewBadRequestError("entity requires exactly one of id or name")
	}
	if r.Mode == "path" && (r.Target == nil || !valid(*r.Target)) {
		return apperrors.NewBadRequestError("path requires target with exactly one of id or name")
	}
	if r.Mode == "neighbors" && r.Target != nil {
		return apperrors.NewBadRequestError("target requires path mode")
	}
	if len(r.RelationTypes) > 100 {
		return apperrors.NewBadRequestError("at most 100 relation types")
	}
	for _, value := range append(append([]string{}, r.Libraries...), r.RelationTypes...) {
		if strings.TrimSpace(value) == "" || len(value) > 512 {
			return apperrors.NewBadRequestError("empty or oversized library or relation type")
		}
	}
	return nil
}
func (s *GraphSearchService) resolveEntity(ctx context.Context, space uint64, libraries []string, input types.GraphEntityInput) ([]types.EntrySearchEntity, error) {
	q := s.db.WithContext(ctx).Model(&types.Entity{}).Where("space_id=? AND library_id IN ? AND coalesce(merged_into_id,'')=''", space, libraries)
	if input.ID != "" {
		q = q.Where("id=?", input.ID)
	} else {
		name := normalizeEntityName(input.Name)
		q = q.Where("normalized_name=? OR id IN (SELECT entity_id FROM entity_aliases WHERE space_id=? AND normalized_alias=?)", name, space, name)
	}
	var entities []types.Entity
	if err := q.Order("canonical_name,entity_type,id").Find(&entities).Error; err != nil {
		return nil, err
	}
	results := []types.EntrySearchEntity{}
	for _, e := range entities {
		aliases := []string{}
		if err := s.db.WithContext(ctx).Model(&types.EntityAlias{}).Where("space_id=? AND entity_id=?", space, e.ID).Order("alias").Pluck("alias", &aliases).Error; err != nil {
			return nil, err
		}
		results = append(results, types.EntrySearchEntity{ID: e.ID, CanonicalName: e.CanonicalName, EntityType: e.EntityType, Aliases: aliases})
	}
	if len(results) == 0 {
		return nil, apperrors.NewNotFoundError("graph entity not found")
	}
	return results, nil
}

// Search is the shared HTTP, MCP and evaluation entry point. It never calls models.
func (s *GraphSearchService) Search(ctx context.Context, request types.GraphSearchRequest) (out *types.GraphSearchResponse, resultErr error) {
	started := time.Now()
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if err = validateGraphRequest(&request); err != nil {
		return nil, err
	}
	space, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, apperrors.NewUnauthorizedError("workspace required")
	}
	libraries := []types.Library{}
	if err = s.db.WithContext(ctx).Where("space_id=? AND (id IN ? OR directory IN ?)", space, request.Libraries, request.Libraries).Order("id").Find(&libraries).Error; err != nil {
		return nil, err
	}
	for _, value := range request.Libraries {
		if !slices.ContainsFunc(libraries, func(l types.Library) bool { return l.ID == value || l.Directory == value }) {
			return nil, apperrors.NewBadRequestError("unknown library: " + value)
		}
	}
	request.Libraries = []string{}
	for _, l := range libraries {
		request.Libraries = append(request.Libraries, l.ID)
	}
	defer func() {
		record := &types.SearchRecord{Method: "graph", Query: request.Entity.ID + request.Entity.Name, Libraries: request.Libraries, Request: raw, DurationMS: time.Since(started).Milliseconds()}
		if request.Target != nil {
			record.Query += " / " + request.Target.ID + request.Target.Name
		}
		if resultErr != nil {
			record.Error = resultErr.Error()
		}
		if out != nil {
			for i, d := range out.Documents {
				record.Results = append(record.Results, types.RankedEntry{EntryID: d.EntryID, Rank: i + 1, Score: d.Score})
			}
		}
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := NewSearchRecorder(s.db).RecordSearch(recordCtx, record); err != nil {
			resultErr = fmt.Errorf("persist graph search record: %w", err)
		}
	}()
	out = &types.GraphSearchResponse{Status: "ok", Libraries: request.Libraries, Mode: request.Mode, Hops: request.Hops, ExpansionLimit: s.expansionLimit, Candidates: map[string][]types.EntrySearchEntity{}, Entities: []types.EntrySearchEntity{}, Edges: []types.GraphSearchEdge{}, Paths: []types.GraphSearchPath{}, Documents: []*types.EntrySearchResult{}, Truncations: []types.GraphTruncation{}}
	candidates, err := s.resolveEntity(ctx, space, request.Libraries, request.Entity)
	if err != nil {
		return nil, err
	}
	out.Candidates["entity"] = candidates
	var targets []types.EntrySearchEntity
	if request.Target != nil {
		targets, err = s.resolveEntity(ctx, space, request.Libraries, *request.Target)
		if err != nil {
			return nil, err
		}
		out.Candidates["target"] = targets
	}
	involved := []string{}
	for _, list := range out.Candidates {
		for _, e := range list {
			involved = append(involved, e.ID)
		}
	}
	if len(candidates) > 1 || len(targets) > 1 {
		out.Status = "needs_selection"
		return out, s.pending(ctx, space, involved, out)
	}
	out.Candidates = map[string][]types.EntrySearchEntity{}
	if err := s.store.Available(ctx); err != nil {
		return nil, err
	}
	start := candidates[0].ID
	target := ""
	if len(targets) == 1 {
		target = targets[0].ID
	}
	frontier := []string{start}
	visited := map[string]bool{start: true}
	seenEdges := map[string]bool{}
	edges := []types.GraphSearchEdge{}
	for hop := 1; hop <= request.Hops && len(frontier) > 0; hop++ {
		sort.Strings(frontier)
		next := []string{}
		remaining := s.expansionLimit
		for i, id := range frontier {
			if remaining == 0 {
				out.Truncations = append(out.Truncations, types.GraphTruncation{Hop: hop, EntityID: id, Reason: "expansion_limit"})
				break
			}
			rawEdges, truncated, err := s.store.Expand(ctx, space, id, request.Libraries, request.RelationTypes, remaining)
			if err != nil {
				return nil, err
			}
			remaining -= len(rawEdges)
			if truncated {
				out.Truncations = append(out.Truncations, types.GraphTruncation{Hop: hop, EntityID: id, Reason: "expansion_limit"})
			}
			fresh := []graphreplica.Edge{}
			for _, e := range rawEdges {
				if !seenEdges[e.ID] {
					seenEdges[e.ID] = true
					fresh = append(fresh, e)
				}
			}
			live, err := s.hydrate(ctx, space, libraries, fresh)
			if err != nil {
				return nil, err
			}
			for _, e := range live {
				edges = append(edges, e)
				for _, end := range []string{e.SubjectID, e.ObjectID} {
					if !visited[end] {
						visited[end] = true
						next = append(next, end)
					}
				}
			}
			if remaining == 0 && i < len(frontier)-1 {
				out.Truncations = append(out.Truncations, types.GraphTruncation{Hop: hop, Reason: "expansion_limit"})
				break
			}
		}
		frontier = next
	}
	if request.Mode == "path" {
		paths, reason := boundedGraphPaths(edges, start, target, request.Hops, request.Limit)
		out.Paths = paths
		if reason != "" {
			out.Truncations = append(out.Truncations, types.GraphTruncation{Reason: reason})
		}
		used := map[string]bool{}
		for _, p := range paths {
			for _, id := range p.RelationIDs {
				used[id] = true
			}
		}
		for _, e := range edges {
			if used[e.ID] {
				out.Edges = append(out.Edges, e)
			}
		}
	} else {
		if len(edges) > request.Limit {
			edges = edges[:request.Limit]
			out.Truncations = append(out.Truncations, types.GraphTruncation{Reason: "result_limit"})
		}
		out.Edges = edges
	}
	ids := []string{start}
	if target != "" {
		ids = append(ids, target)
	}
	for _, e := range out.Edges {
		ids = append(ids, e.SubjectID, e.ObjectID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var nodes []types.Entity
	if err := s.db.WithContext(ctx).Where("space_id=? AND id IN ?", space, ids).Order("id").Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, n := range nodes {
		out.Entities = append(out.Entities, types.EntrySearchEntity{ID: n.ID, CanonicalName: n.CanonicalName, EntityType: n.EntityType, Aliases: []string{}})
	}
	out.Documents = graphDocuments(out.Edges)
	if len(out.Documents) > request.Limit {
		out.Documents = out.Documents[:request.Limit]
		out.Truncations = append(out.Truncations, types.GraphTruncation{Reason: "document_limit"})
	}
	out.Truncated = len(out.Truncations) > 0
	return out, s.pending(ctx, space, ids, out)
}
func (s *GraphSearchService) pending(ctx context.Context, space uint64, ids []string, out *types.GraphSearchResponse) error {
	// One statement observes both counts at the same PG snapshot. Global means the personal space.
	return s.db.WithContext(ctx).Model(&types.GraphPendingSync{}).Select("count(*) AS global, count(*) FILTER (WHERE entity_id IN ?) AS involved", ids).Where("space_id=? AND completed_at IS NULL", space).Scan(&out.PendingSync).Error
}

// EvaluationSearch overrides embedded scope and result size with the validated
// question and run options. Ambiguity is a failed question requiring a chosen ID.
func (s *GraphSearchService) EvaluationSearch(ctx context.Context, r types.EntrySearchRequest) (*types.EntrySearchResponse, error) {
	var request types.GraphSearchRequest
	if len(r.GraphParameters) == 0 {
		return nil, apperrors.NewBadRequestError("graph_parameters required")
	}
	decoder := json.NewDecoder(bytes.NewReader(r.GraphParameters))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, apperrors.NewBadRequestError("invalid graph_parameters: " + err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, apperrors.NewBadRequestError("graph_parameters requires one object")
	}
	request.Libraries = append([]string{}, r.Libraries...)
	request.Limit = r.Limit
	out, err := s.Search(ctx, request)
	if err != nil {
		return nil, err
	}
	if out.Status != "ok" {
		return nil, apperrors.NewBadRequestError("graph entity ambiguous; choose an entity ID")
	}
	snapshot, _ := json.Marshal(map[string]any{"method": "graph", "request": request, "expansion_limit": out.ExpansionLimit, "truncated": out.Truncated, "truncations": out.Truncations, "pending_sync": out.PendingSync, "models_called": false})
	return &types.EntrySearchResponse{Results: out.Documents, RerankStatus: "unconfigured", Snapshot: snapshot}, nil
}
