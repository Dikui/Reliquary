package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Filters are evaluated in PostgreSQL against effective snapshots and edges.
// Each walk step intersects admitted parts, rather than expanding configurations.
const shelfQueryMatchSQL = `WITH RECURSIVE
vars AS (SELECT ?::text library_id, ?::bigint space_id, ?::timestamptz at, ?::jsonb filters, ?::text ancestor_id),
nodes AS MATERIALIZED (
 SELECT e.id,e.kind,e.canonical_name,e.archived_at,e.merged_into_id,v.attributes FROM entities e JOIN vars x ON e.library_id=x.library_id AND e.space_id=x.space_id
 LEFT JOIN LATERAL (SELECT attributes FROM entity_attribute_versions WHERE entity_id=e.id AND space_id=x.space_id AND effective_at<=x.at ORDER BY effective_at DESC,revision DESC LIMIT 1) v ON true),
edges AS MATERIALIZED (SELECT r.* FROM relations r JOIN vars x ON r.library_id=x.library_id AND r.space_id=x.space_id
 WHERE r.valid_from<=x.at AND (r.valid_to IS NULL OR r.valid_to>x.at)),
ancestors(model_id,id,depth) AS (
 SELECT id,id,0 FROM nodes WHERE kind='model'
 UNION ALL SELECT a.model_id,r.object_id,a.depth+1 FROM ancestors a JOIN edges r ON r.subject_id=a.id AND r.relation_type='belongs_to' JOIN nodes n ON n.id=r.object_id WHERE a.depth<3),
candidates AS (SELECT n.* FROM nodes n,vars x WHERE n.kind='model' AND n.archived_at IS NULL AND n.merged_into_id='' AND n.attributes IS NOT NULL AND (x.ancestor_id='' OR EXISTS (SELECT 1 FROM ancestors a WHERE a.model_id=n.id AND a.id=x.ancestor_id))),
parts AS MATERIALIZED (SELECT r.subject_id model_id,r.object_id part_id,r.attributes->>'module_type' module FROM edges r JOIN nodes n ON n.id=r.object_id AND n.kind='part' WHERE r.relation_type='composed_of'),
filters AS (SELECT (ordinality-1)::int step,value FROM vars,jsonb_array_elements(filters) WITH ORDINALITY),
facts AS (
 SELECT m.id model_id,f.step,coalesce(nullif(row->'conditions','null'::jsonb),'[]'::jsonb) conditions
 FROM candidates m CROSS JOIN filters f CROSS JOIN LATERAL jsonb_array_elements(coalesce(m.attributes->'sale_statuses','[]'::jsonb)) row
 WHERE f.value->>'type'='status' AND (f.value->>'region'='' OR row->>'region'=f.value->>'region')
 AND (f.value->>'status'='' OR row->>'status'=f.value->>'status')
 AND (f.value->>'planned_eom' IS NULL OR coalesce((row->>'planned_eom')::boolean,false)=(f.value->>'planned_eom')::boolean)
 UNION ALL
 SELECT m.id,f.step,'[]'::jsonb FROM candidates m CROSS JOIN filters f
 WHERE f.value->>'type'='no_status' AND NOT EXISTS (
  SELECT 1 FROM jsonb_array_elements(coalesce(m.attributes->'sale_statuses','[]'::jsonb)) row
  WHERE f.value->>'region'='' OR row->>'region'=f.value->>'region')
 UNION ALL
 SELECT m.id,f.step,coalesce(nullif(row->'conditions','null'::jsonb),'[]'::jsonb)
 FROM candidates m CROSS JOIN filters f CROSS JOIN LATERAL jsonb_array_elements(coalesce(m.attributes->(f.value->>'field'),'[]'::jsonb)) row
 WHERE f.value->>'type'='parameter'
 AND (f.value->>'min' IS NULL OR (CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'max')::numeric END)>=(f.value->>'min')::numeric)
 AND (f.value->>'max' IS NULL OR (CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'min')::numeric END)<=(f.value->>'max')::numeric)
 UNION ALL
 SELECT m.id,f.step,'[]'::jsonb FROM candidates m CROSS JOIN filters f
 WHERE f.value->>'type'='parameter_missing' AND EXISTS (SELECT 1 FROM ancestors a WHERE a.model_id=m.id AND jsonb_exists(f.value->'lines',a.id))
 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(m.attributes->(f.value->>'field'))='array' THEN m.attributes->(f.value->>'field') ELSE '[]'::jsonb END) row
  WHERE (CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'min')::numeric END) IS NOT NULL)
 UNION ALL
 SELECT m.id,f.step,jsonb_build_array(jsonb_build_object('module_type',f.value->>'module_type','part_ids',f.value->'part_ids')) FROM candidates m CROSS JOIN filters f WHERE f.value->>'type'='composition'
 UNION ALL
 SELECT p.model_id,f.step,jsonb_build_array(jsonb_build_object('module_type',p.module,'part_ids',jsonb_build_array(p.part_id))) FROM parts p CROSS JOIN filters f WHERE f.value->>'type'='part' AND p.part_id=f.value->>'part_id'
 UNION ALL
 SELECT m.id,f.step,gap->'conditions' FROM candidates m CROSS JOIN filters f CROSS JOIN LATERAL jsonb_array_elements(f.value->'gaps') gap
 WHERE f.value->>'type'='document_gap' AND gap->>'model_id'=m.id
 UNION ALL
 SELECT m.id,f.step,coalesce(nullif(c.attributes->'certification'->'conditions','null'::jsonb),'[]'::jsonb)||coalesce((
  SELECT jsonb_agg(jsonb_build_object('module_type',p.module,'part_ids',jsonb_build_array(p.part_id))) FROM parts p
  WHERE p.model_id=m.id AND p.part_id IN (c.attributes->'certification'->>'owner_id',c.attributes->'certification'->>'cell_part_id')
 ),'[]'::jsonb)
 FROM candidates m CROSS JOIN filters f JOIN nodes c ON c.kind='certification' AND c.archived_at IS NULL AND c.merged_into_id=''
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

// shelfQueryFacetSQL extends the page select with counts over every matched
// model. Its one argument lists the numeric shelf fields with their lines.
const shelfQueryFacetSQL = `,(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY region,status),'[]'::jsonb) FROM (
 SELECT region,status,count(*) models,count(*) FILTER (WHERE conditional) conditional_models
 FROM (SELECT row->>'region' region,row->>'status' status,bool_and(jsonb_array_length(coalesce(nullif(row->'conditions','null'::jsonb),'[]'::jsonb))>0) conditional
  FROM matched m JOIN candidates c ON c.id=m.id CROSS JOIN LATERAL jsonb_array_elements(coalesce(c.attributes->'sale_statuses','[]'::jsonb)) row
  GROUP BY c.id,1,2) s GROUP BY region,status) t) sale_statuses,
(SELECT coalesce(jsonb_agg(jsonb_build_object('module_type',module,'part_id',part_id,'part_name',part_name,'models',models) ORDER BY module,part_name),'[]'::jsonb)
 FROM (SELECT p.module,p.part_id,n.canonical_name part_name,count(DISTINCT p.model_id) models FROM matched m JOIN parts p ON p.model_id=m.id JOIN nodes n ON n.id=p.part_id GROUP BY 1,2,3) s) composition,
(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY field),'[]'::jsonb) FROM (
 SELECT field,count(lo) models,count(*)-count(lo) missing,min(lo) min,max(hi) max
 FROM matched m JOIN candidates c ON c.id=m.id CROSS JOIN jsonb_array_elements(?::jsonb) k
 CROSS JOIN LATERAL (SELECT k->>'field' field) f
 LEFT JOIN LATERAL (SELECT min(CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'min')::numeric END) lo,
  max(CASE WHEN jsonb_typeof(row->'value')='number' THEN (row->>'value')::numeric ELSE (row->'value'->>'max')::numeric END) hi
  FROM jsonb_array_elements(CASE WHEN jsonb_typeof(c.attributes->f.field)='array' THEN c.attributes->f.field ELSE '[]'::jsonb END) row) v ON true
 WHERE EXISTS (SELECT 1 FROM ancestors a WHERE a.model_id=c.id AND jsonb_exists(k->'lines',a.id))
 GROUP BY field) t) parameters,
(SELECT coalesce(jsonb_agg(jsonb_build_object('standard',standard,'progress',progress,'region',region,'models',models) ORDER BY standard,progress,region),'[]'::jsonb)
 FROM (SELECT cert->>'standard' standard,coalesce(cert->>'progress','') progress,coalesce(cert->>'region','') region,count(DISTINCT m.id) models
  FROM matched m JOIN nodes c ON c.kind='certification' AND c.archived_at IS NULL AND c.merged_into_id=''
  CROSS JOIN LATERAL (SELECT c.attributes->'certification' cert) x
  JOIN edges link ON link.relation_type='has_certification' AND link.subject_id=x.cert->>'owner_id' AND link.object_id=c.id
  WHERE (x.cert->>'owner_id'=m.id
   OR EXISTS (SELECT 1 FROM ancestors a JOIN nodes n ON n.id=a.id AND n.kind IN ('product','platform') WHERE a.model_id=m.id AND a.id=x.cert->>'owner_id')
   OR EXISTS (SELECT 1 FROM parts p WHERE p.model_id=m.id AND p.part_id=x.cert->>'owner_id'))
  AND (coalesce(x.cert->>'cell_part_id','')='' OR EXISTS (SELECT 1 FROM parts p WHERE p.model_id=m.id AND p.part_id=x.cert->>'cell_part_id'))
  GROUP BY 1,2,3) s) certifications`

func (s *EntityService) ShelfQuery(ctx context.Context, libraryID string, input types.ShelfQueryRequest) (*types.ShelfQueryResult, error) {
	// Callers outside the service default to summary; internal readers that need
	// full model details call shelfQuery directly with an empty detail.
	switch input.Detail {
	case "":
		input.Detail = types.ShelfQueryDetailSummary
	case types.ShelfQueryDetailSummary, types.ShelfQueryDetailFull:
	default:
		return nil, apperrors.NewBadRequestError(fmt.Sprintf("unknown detail %q; use summary or full", input.Detail))
	}
	ctx = context.WithValue(ctx, shelfChecklistCacheKey{}, &shelfChecklistCache{})
	var result *types.ShelfQueryResult
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		var err error
		result, err = reader.shelfQuery(ctx, libraryID, input)
		return err
	})
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
	detailLevel := types.ShelfQueryDetailFull
	if input.Detail == types.ShelfQueryDetailSummary {
		detailLevel = types.ShelfQueryDetailSummary
	}
	standardKey := ""
	filters := []map[string]any{}
	for _, status := range input.SaleStatuses {
		if status.Region != "" && !slices.ContainsFunc(library.ShelfConfig.Regions, func(region types.ShelfOption) bool { return region.Key == status.Region }) {
			keys := make([]string, 0, len(library.ShelfConfig.Regions))
			for _, region := range library.ShelfConfig.Regions {
				keys = append(keys, region.Key+" ("+region.Name+")")
			}
			return nil, apperrors.NewBadRequestError(fmt.Sprintf("unknown region %q; use a configured region key, or omit region for any region: %s", status.Region, strings.Join(keys, ", ")))
		}
		if status.Status != "" && !slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, status.Status) {
			return nil, apperrors.NewBadRequestError(fmt.Sprintf("unknown status %q; use GA, DEV, Planning, EOM or 待定, planned_eom for planned retirement, no_record for missing records", status.Status))
		}
		if status.NoRecord && (status.Status != "" || status.PlannedEOM != nil) {
			return nil, apperrors.NewBadRequestError("no_record cannot be combined with status or planned_eom")
		}
		if status.Region == "" && status.Status == "" && status.PlannedEOM == nil && !status.NoRecord {
			return nil, apperrors.NewBadRequestError("each regional filter needs region, status, planned_eom or no_record")
		}
		filterType := "status"
		if status.NoRecord {
			filterType = "no_status"
		}
		filters = append(filters, map[string]any{"type": filterType, "region": status.Region, "status": status.Status, "planned_eom": status.PlannedEOM})
	}
	if len(input.Parameters) > 20 {
		return nil, apperrors.NewBadRequestError("at most 20 parameter filters are allowed")
	}
	for _, parameter := range input.Parameters {
		if parameter.Missing && (parameter.Min != nil || parameter.Max != nil) {
			return nil, apperrors.NewBadRequestError("parameter missing cannot be combined with min or max")
		}
		if !parameter.Missing && ((parameter.Min == nil && parameter.Max == nil) || (parameter.Min != nil && parameter.Max != nil && *parameter.Min > *parameter.Max)) {
			return nil, apperrors.NewBadRequestError("parameter bounds require min or max in ascending order, or missing=true")
		}
		index := slices.IndexFunc(library.EntitySchema.Fields, func(field types.EntityFieldDefinition) bool {
			return field.Key == parameter.Field && field.Type == "number" && len(field.ProductLineIDs) > 0
		})
		if index < 0 {
			return nil, apperrors.NewBadRequestError("parameter filter requires a configured numeric shelf field")
		}
		if parameter.Missing {
			filters = append(filters, map[string]any{"type": "parameter_missing", "field": parameter.Field, "lines": library.EntitySchema.Fields[index].ProductLineIDs})
			continue
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
		if cert.Region != nil && *cert.Region != "" && !shelfOptionExists(library.ShelfConfig.Regions, *cert.Region) {
			return nil, apperrors.NewBadRequestError("certification region filter requires a configured region key or an explicit empty string")
		}
		if cert.Standard == "" && cert.Progress == nil && cert.Region == nil {
			return nil, apperrors.NewBadRequestError("certification filter requires a standard, progress or region")
		}
		filter := map[string]any{"type": "certification"}
		if cert.Standard != "" {
			standard, ok := library.ShelfConfig.Standard(cert.Standard)
			if !ok {
				result := &types.ShelfQueryResult{Items: []types.ShelfQueryItem{}, AsOf: at, Detail: detailLevel}
				if input.Facets {
					result.Facets = &types.ShelfQueryFacets{SaleStatuses: []types.ShelfStatusFacet{}, Composition: []types.ShelfCompositionFacet{}, Parameters: []types.ShelfParameterFacet{}, Certifications: []types.ShelfCertificationFacet{}}
				}
				return result, nil
			}
			filter["standard"] = standard.Key
			standardKey = standard.Key
		}
		if cert.Progress != nil {
			filter["progress"] = *cert.Progress
		}
		if cert.Region != nil {
			filter["region"] = *cert.Region
		}
		filters = append(filters, filter)
	}
	if input.DocumentGaps {
		data, err := s.loadShelfChecklist(ctx, library, at)
		if err != nil {
			return nil, err
		}
		gaps := []map[string]any{}
		for id, e := range data.nodes {
			if e.Kind != "model" || e.ArchivedAt != nil || e.MergedIntoID != "" {
				continue
			}
			_, _, rows := data.model(id, nil)
			for _, r := range rows {
				if r.Status == "missing" {
					gaps = append(gaps, map[string]any{"model_id": id, "conditions": r.Conditions})
				}
			}
		}
		filters = append(filters, map[string]any{"type": "document_gap", "gaps": gaps})
	}
	data, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	var row struct {
		Total          int64
		Items          types.JSON
		SaleStatuses   types.JSON
		Composition    types.JSON
		Parameters     types.JSON
		Certifications types.JSON
	}
	query, args := shelfQueryMatchSQL, []any{library.ID, library.SpaceID, at, string(data), input.AncestorID, limit, input.Offset}
	if input.Facets {
		fields := []map[string]any{}
		for _, field := range library.EntitySchema.Fields {
			if field.Type == "number" && len(field.ProductLineIDs) > 0 && !field.Archived {
				fields = append(fields, map[string]any{"field": field.Key, "lines": field.ProductLineIDs})
			}
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		query, args = query+shelfQueryFacetSQL, append(args, string(encoded))
	}
	if err := s.db.WithContext(ctx).Raw(query, args...).Scan(&row).Error; err != nil {
		return nil, err
	}
	var matches []struct {
		ID         string
		Conditions [][]types.ShelfCondition
	}
	if err := json.Unmarshal(row.Items, &matches); err != nil {
		return nil, err
	}
	result := &types.ShelfQueryResult{Items: []types.ShelfQueryItem{}, Total: row.Total, AsOf: at, Detail: detailLevel}
	if input.Facets {
		if result.Facets, err = shelfQueryFacets(library, row.SaleStatuses, row.Composition, row.Parameters, row.Certifications); err != nil {
			return nil, err
		}
	}
	for _, match := range matches {
		detail, err := s.shelfGet(ctx, libraryID, match.ID, types.ShelfReadOptions{AsOf: &at})
		if err != nil {
			return nil, err
		}
		item := types.ShelfQueryItem{ShelfNodeDetail: *detail, MatchingConditions: match.Conditions, PartialOptions: shelfMatchesPartial(match.Conditions, detail.Shelf.Composition)}
		if result.Detail == types.ShelfQueryDetailSummary {
			item.ShelfNodeDetail, item.CurrentStage = shelfQuerySummary(detail, input, standardKey)
		}
		result.Items = append(result.Items, item)
	}
	result.Truncated = int64(len(result.Items)) < result.Total
	return result, nil
}

// shelfQueryFacets decodes the aggregate columns and adds display names from
// the library configuration so options can be offered without another read.
func shelfQueryFacets(library *types.Library, statuses, composition, parameters, certifications types.JSON) (*types.ShelfQueryFacets, error) {
	facets := &types.ShelfQueryFacets{}
	for _, part := range []struct {
		raw    types.JSON
		target any
	}{{statuses, &facets.SaleStatuses}, {composition, &facets.Composition}, {parameters, &facets.Parameters}, {certifications, &facets.Certifications}} {
		if err := json.Unmarshal(part.raw, part.target); err != nil {
			return nil, err
		}
	}
	for i, facet := range facets.SaleStatuses {
		for _, region := range library.ShelfConfig.Regions {
			if region.Key == facet.Region {
				facets.SaleStatuses[i].RegionName = region.Name
			}
		}
	}
	for i, facet := range facets.Parameters {
		for _, field := range library.EntitySchema.Fields {
			if field.Key == facet.Field {
				facets.Parameters[i].Name = field.Name
			}
		}
	}
	for i, facet := range facets.Certifications {
		if standard, ok := library.ShelfConfig.Standard(facet.Standard); ok {
			facets.Certifications[i].StandardName = standard.Name
		}
	}
	return facets, nil
}

// shelfQuerySummary keeps identity, the description (which may record values
// absent from structured fields) and only the facts that the filters examined.
func shelfQuerySummary(detail *types.ShelfNodeDetail, input types.ShelfQueryRequest, standardKey string) (types.ShelfNodeDetail, string) {
	entity := detail.Entity
	entity.Attributes, entity.AutoMatch = nil, nil
	summary := types.ShelfNodeDetail{Entity: entity, AsOf: detail.AsOf, Recorded: detail.Recorded, Facts: []types.ShelfFact{}}
	for _, fact := range detail.Facts {
		if shelfQueryFactMatched(fact, input, standardKey) {
			summary.Facts = append(summary.Facts, fact)
		}
	}
	stage := ""
	if detail.Shelf != nil {
		stage = detail.Shelf.CurrentStage
	}
	return summary, stage
}

func shelfQueryFactMatched(fact types.ShelfFact, input types.ShelfQueryRequest, standardKey string) bool {
	switch value := fact.Value.(type) {
	case types.ShelfSaleStatus:
		if fact.Field != "sale_statuses" {
			return false
		}
		return slices.ContainsFunc(input.SaleStatuses, func(filter types.ShelfStatusFilter) bool {
			return filter.Region == "" || filter.Region == value.Region
		})
	case types.ShelfCertification:
		cert := input.Certification
		if fact.Field != "certification" || cert == nil {
			return false
		}
		return (standardKey == "" || value.Standard == standardKey) &&
			(cert.Progress == nil || value.Progress == *cert.Progress) &&
			(cert.Region == nil || value.Region == *cert.Region)
	}
	return fact.Origin == "self" && slices.ContainsFunc(input.Parameters, func(filter types.ShelfParameterFilter) bool {
		return filter.Field == fact.Field
	})
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
