package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Filters are evaluated in PostgreSQL against effective snapshots and edges.
// Each walk step intersects admitted parts, rather than expanding configurations.
const shelfQuerySQL = `WITH RECURSIVE
vars AS (SELECT ?::text library_id, ?::bigint space_id, ?::timestamptz at, ?::jsonb filters, ?::text ancestor_id),
nodes AS MATERIALIZED (
 SELECT e.id,e.kind,e.canonical_name,v.attributes FROM entities e JOIN vars x ON e.library_id=x.library_id AND e.space_id=x.space_id
 JOIN LATERAL (SELECT attributes FROM entity_attribute_versions WHERE entity_id=e.id AND space_id=x.space_id AND effective_at<=x.at ORDER BY effective_at DESC,revision DESC LIMIT 1) v ON true
 WHERE e.archived_at IS NULL AND e.merged_into_id=''),
edges AS MATERIALIZED (SELECT r.* FROM relations r JOIN vars x ON r.library_id=x.library_id AND r.space_id=x.space_id
 WHERE r.valid_from<=x.at AND (r.valid_to IS NULL OR r.valid_to>x.at)),
ancestors(model_id,id,depth) AS (
 SELECT id,id,0 FROM nodes WHERE kind='model'
 UNION ALL SELECT a.model_id,r.object_id,a.depth+1 FROM ancestors a JOIN edges r ON r.subject_id=a.id AND r.relation_type='belongs_to' JOIN nodes n ON n.id=r.object_id WHERE a.depth<3),
candidates AS (SELECT n.* FROM nodes n,vars x WHERE n.kind='model' AND (x.ancestor_id='' OR EXISTS (SELECT 1 FROM ancestors a WHERE a.model_id=n.id AND a.id=x.ancestor_id))),
parts AS MATERIALIZED (SELECT r.subject_id model_id,r.object_id part_id,r.attributes->>'module_type' module FROM edges r JOIN nodes n ON n.id=r.object_id AND n.kind='part' WHERE r.relation_type='composed_of'),
filters AS (SELECT (ordinality-1)::int step,value FROM vars,jsonb_array_elements(filters) WITH ORDINALITY),
facts AS (
 SELECT m.id model_id,f.step,coalesce(nullif(row->'conditions','null'::jsonb),'[]'::jsonb) conditions
 FROM candidates m CROSS JOIN filters f CROSS JOIN LATERAL jsonb_array_elements(coalesce(m.attributes->'sale_statuses','[]'::jsonb)) row
 WHERE f.value->>'type'='status' AND row->>'region'=f.value->>'region' AND row->>'status'=f.value->>'status'
 UNION ALL
 SELECT m.id,f.step,coalesce(nullif(row->'conditions','null'::jsonb),'[]'::jsonb)
 FROM candidates m CROSS JOIN filters f CROSS JOIN LATERAL jsonb_array_elements(coalesce(m.attributes->(f.value->>'field'),'[]'::jsonb)) row
 WHERE f.value->>'type'='parameter'
 AND (f.value->>'min' IS NULL OR (CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'max')::numeric END)>=(f.value->>'min')::numeric)
 AND (f.value->>'max' IS NULL OR (CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'min')::numeric END)<=(f.value->>'max')::numeric)
 UNION ALL
 SELECT m.id,f.step,jsonb_build_array(jsonb_build_object('module_type',f.value->>'module_type','part_ids',f.value->'part_ids')) FROM candidates m CROSS JOIN filters f WHERE f.value->>'type'='composition'
 UNION ALL
 SELECT p.model_id,f.step,jsonb_build_array(jsonb_build_object('module_type',p.module,'part_ids',jsonb_build_array(p.part_id))) FROM parts p CROSS JOIN filters f WHERE f.value->>'type'='part' AND p.part_id=f.value->>'part_id'
 UNION ALL
 SELECT m.id,f.step,coalesce(nullif(c.attributes->'certification'->'conditions','null'::jsonb),'[]'::jsonb)||coalesce((
  SELECT jsonb_agg(jsonb_build_object('module_type',p.module,'part_ids',jsonb_build_array(p.part_id))) FROM parts p
  WHERE p.model_id=m.id AND p.part_id IN (c.attributes->'certification'->>'owner_id',c.attributes->'certification'->>'cell_part_id')
 ),'[]'::jsonb)
 FROM candidates m CROSS JOIN filters f JOIN nodes c ON c.kind='certification'
 JOIN edges link ON link.relation_type='has_certification' AND link.subject_id=c.attributes->'certification'->>'owner_id' AND link.object_id=c.id
 WHERE f.value->>'type'='certification'
 AND (NOT jsonb_exists(f.value,'standard') OR c.attributes->'certification'->>'standard'=f.value->>'standard')
 AND (NOT jsonb_exists(f.value,'progress') OR coalesce(c.attributes->'certification'->>'progress','')=f.value->>'progress')
 AND (NOT jsonb_exists(f.value,'region') OR c.attributes->'certification'->>'region'=f.value->>'region')
 AND (c.attributes->'certification'->>'owner_id'=m.id
  OR EXISTS (SELECT 1 FROM ancestors a JOIN nodes n ON n.id=a.id AND n.kind IN ('product','platform') WHERE a.model_id=m.id AND a.id=c.attributes->'certification'->>'owner_id')
  OR EXISTS (SELECT 1 FROM parts p WHERE p.model_id=m.id AND p.part_id=c.attributes->'certification'->>'owner_id'))
 AND (coalesce(c.attributes->'certification'->>'cell_part_id','')='' OR EXISTS (SELECT 1 FROM parts p WHERE p.model_id=m.id AND p.part_id=c.attributes->'certification'->>'cell_part_id'))
),
walk(model_id,step,conditions) AS (
 SELECT id,0,'[]'::jsonb FROM candidates
 UNION SELECT w.model_id,w.step+1,w.conditions||f.conditions FROM walk w JOIN facts f ON f.model_id=w.model_id AND f.step=w.step
 WHERE NOT EXISTS (
  SELECT 1 FROM jsonb_array_elements(w.conditions||f.conditions) c WHERE NOT EXISTS (
   SELECT 1 FROM parts p WHERE p.model_id=w.model_id AND p.module=c->>'module_type' AND NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(w.conditions||f.conditions) d WHERE d->>'module_type'=p.module AND NOT jsonb_exists(d->'part_ids',p.part_id))))
),
matched AS (SELECT w.model_id id,jsonb_agg(DISTINCT w.conditions) conditions FROM walk w,vars x WHERE w.step=jsonb_array_length(x.filters) GROUP BY w.model_id)
SELECT (SELECT count(*) FROM matched) total,coalesce((SELECT jsonb_agg(to_jsonb(page)) FROM (
 SELECT m.id,m.conditions FROM matched m JOIN candidates c ON c.id=m.id ORDER BY c.canonical_name,c.id LIMIT ? OFFSET ?
) page),'[]'::jsonb) items`

func (s *EntityService) ShelfQuery(ctx context.Context, libraryID string, input types.ShelfQueryRequest) (*types.ShelfQueryResult, error) {
	var result *types.ShelfQueryResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := *s
		reader.db = tx
		var err error
		result, err = reader.shelfQuery(ctx, libraryID, input)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

func (s *EntityService) shelfQuery(ctx context.Context, libraryID string, input types.ShelfQueryRequest) (*types.ShelfQueryResult, error) {
	library, err := s.shelfLibraryForRead(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	limit, err := shelfPage(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	at := time.Now()
	if input.AsOf != nil {
		at = *input.AsOf
	}
	if at.IsZero() {
		return nil, apperrors.NewBadRequestError("as_of must be a nonzero time")
	}
	if len(input.SaleStatuses) > 20 {
		return nil, apperrors.NewBadRequestError("at most 20 regional filters are allowed")
	}
	filters := []map[string]any{}
	for _, status := range input.SaleStatuses {
		if !slices.ContainsFunc(library.ShelfConfig.Regions, func(region types.ShelfOption) bool { return region.Key == status.Region }) || !slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, status.Status) {
			return nil, apperrors.NewBadRequestError("regional filters require a configured region and valid status")
		}
		filters = append(filters, map[string]any{"type": "status", "region": status.Region, "status": status.Status})
	}
	if len(input.Parameters) > 20 {
		return nil, apperrors.NewBadRequestError("at most 20 parameter filters are allowed")
	}
	for _, parameter := range input.Parameters {
		if (parameter.Min == nil && parameter.Max == nil) || (parameter.Min != nil && parameter.Max != nil && *parameter.Min > *parameter.Max) {
			return nil, apperrors.NewBadRequestError("parameter bounds require min or max in ascending order")
		}
		if !slices.ContainsFunc(library.EntitySchema.Fields, func(field types.EntityFieldDefinition) bool {
			return field.Key == parameter.Field && field.Type == "number" && len(field.ProductLineIDs) > 0
		}) {
			return nil, apperrors.NewBadRequestError("parameter filter requires a configured numeric shelf field")
		}
		filters = append(filters, map[string]any{"type": "parameter", "field": parameter.Field, "min": parameter.Min, "max": parameter.Max})
	}
	if err := types.ValidateShelfConditions(input.Composition); err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	for _, condition := range input.Composition {
		if !slices.ContainsFunc(library.ShelfConfig.ModuleTypes, func(module types.ShelfOption) bool { return module.Key == condition.ModuleType }) {
			return nil, apperrors.NewBadRequestError("composition filter requires a configured module")
		}
		filters = append(filters, map[string]any{"type": "composition", "module_type": condition.ModuleType, "part_ids": condition.PartIDs})
	}
	if input.PartID != "" {
		filters = append(filters, map[string]any{"type": "part", "part_id": input.PartID})
	}
	if input.Certification != nil {
		cert := input.Certification
		if cert.Standard == "" && cert.Progress == nil && cert.Region == nil {
			return nil, apperrors.NewBadRequestError("certification filter requires a standard, progress or region")
		}
		filter := map[string]any{"type": "certification"}
		if cert.Standard != "" {
			standard, ok := library.ShelfConfig.Standard(cert.Standard)
			if !ok {
				return &types.ShelfQueryResult{Items: []types.ShelfQueryItem{}, AsOf: at}, nil
			}
			filter["standard"] = standard.Key
		}
		if cert.Progress != nil {
			filter["progress"] = *cert.Progress
		}
		if cert.Region != nil {
			filter["region"] = *cert.Region
		}
		filters = append(filters, filter)
	}
	data, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	var row struct {
		Total int64
		Items types.JSON
	}
	if err := s.db.WithContext(ctx).Raw(shelfQuerySQL, library.ID, library.SpaceID, at, string(data), input.AncestorID, limit, input.Offset).Scan(&row).Error; err != nil {
		return nil, err
	}
	var matches []struct {
		ID         string
		Conditions [][]types.ShelfCondition
	}
	if err := json.Unmarshal(row.Items, &matches); err != nil {
		return nil, err
	}
	result := &types.ShelfQueryResult{Items: []types.ShelfQueryItem{}, Total: row.Total, AsOf: at}
	for _, match := range matches {
		detail, err := s.ShelfGet(ctx, libraryID, match.ID, types.ShelfReadOptions{AsOf: &at})
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, types.ShelfQueryItem{ShelfNodeDetail: *detail, MatchingConditions: match.Conditions, PartialOptions: shelfMatchesPartial(match.Conditions, detail.Shelf.Composition)})
	}
	result.Truncated = int64(len(result.Items)) < result.Total
	return result, nil
}

// Subtract matching rectangles from the domains of optional parts. This proves
// coverage without generating the Cartesian product of complete configurations.
func shelfMatchesPartial(matches [][]types.ShelfCondition, composition []types.ShelfComposition) bool {
	domain := map[string][]string{}
	modules := []string{}
	for _, group := range composition {
		modules = append(modules, group.ModuleType)
		for _, part := range group.Parts {
			domain[group.ModuleType] = append(domain[group.ModuleType], part.ID)
		}
	}
	remaining := []map[string][]string{domain}
	for _, conditions := range matches {
		covered := cloneShelfDomains(domain)
		for _, condition := range conditions {
			covered[condition.ModuleType] = shelfPartIntersection(covered[condition.ModuleType], condition.PartIDs)
		}
		next := []map[string][]string{}
		for _, box := range remaining {
			intersection := map[string][]string{}
			overlaps := true
			for _, module := range modules {
				intersection[module] = shelfPartIntersection(box[module], covered[module])
				if len(intersection[module]) == 0 {
					overlaps = false
					break
				}
			}
			if !overlaps {
				next = append(next, box)
				continue
			}
			prefix := cloneShelfDomains(box)
			for _, module := range modules {
				outside := []string{}
				for _, id := range prefix[module] {
					if !slices.Contains(intersection[module], id) {
						outside = append(outside, id)
					}
				}
				if len(outside) > 0 {
					piece := cloneShelfDomains(prefix)
					piece[module] = outside
					next = append(next, piece)
				}
				prefix[module] = intersection[module]
			}
		}
		remaining = next
		if len(remaining) == 0 {
			return false
		}
	}
	return len(remaining) > 0
}

func cloneShelfDomains(domain map[string][]string) map[string][]string {
	copy := map[string][]string{}
	for module, ids := range domain {
		copy[module] = ids
	}
	return copy
}

func shelfPartIntersection(left, right []string) []string {
	result := []string{}
	for _, id := range left {
		if slices.Contains(right, id) {
			result = append(result, id)
		}
	}
	return result
}
