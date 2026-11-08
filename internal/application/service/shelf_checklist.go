package service

import (
	"context"
	"slices"
	"sort"
	"time"

	"reliquary/internal/types"
)

// A read loads the library graph once. Checklist rows remain calculated views;
// neither the rows nor their summaries are persisted.
type shelfChecklistCacheKey struct{}
type shelfChecklistCache struct {
	libraryID string
	at        time.Time
	data      *shelfChecklistData
}

type shelfChecklistData struct {
	library      *types.Library
	nodes        map[string]types.Entity
	recorded     map[string]bool
	requirements []types.Entity
	edges        []types.Relation
	bindings     []types.Relation
	documents    []types.ShelfDocumentVersion
}

func (s *EntityService) loadShelfChecklist(ctx context.Context, l *types.Library, at time.Time) (*shelfChecklistData, error) {
	cache, _ := ctx.Value(shelfChecklistCacheKey{}).(*shelfChecklistCache)
	if cache != nil && cache.libraryID == l.ID && cache.at.Equal(at) {
		return cache.data, nil
	}
	d := &shelfChecklistData{library: l, nodes: map[string]types.Entity{}, recorded: map[string]bool{}}
	var nodes []types.Entity
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=?", l.ID, l.SpaceID).Order("canonical_name,id").Find(&nodes).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(nodes))
	for _, e := range nodes {
		ids = append(ids, e.ID)
	}
	var versions []types.EntityAttributeVersion
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Select("DISTINCT ON (entity_id) *").Where("entity_id IN ? AND effective_at<=?", ids, at).Order("entity_id,effective_at DESC,revision DESC").Find(&versions).Error; err != nil {
			return nil, err
		}
	}
	snapshots := map[string]map[string]any{}
	for _, v := range versions {
		snapshots[v.EntityID] = v.Attributes
		d.recorded[v.EntityID] = true
	}
	for _, e := range nodes {
		e.Attributes = snapshots[e.ID]
		d.nodes[e.ID] = e
	}
	// Requirements always use the latest effective list, even for older models.
	for i := range nodes {
		if nodes[i].Kind == "document_requirement" && nodes[i].ArchivedAt == nil && nodes[i].MergedIntoID == "" {
			d.requirements = append(d.requirements, nodes[i])
		}
	}
	pointers := []*types.Entity{}
	for i := range d.requirements {
		pointers = append(pointers, &d.requirements[i])
	}
	if err := hydrateManagedEntityAttributes(ctx, s.db, pointers, time.Now()); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=? AND relation_type IN ('belongs_to','composed_of') AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", l.ID, l.SpaceID, at, at).Find(&d.edges).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=? AND relation_type='applies_to' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", l.ID, l.SpaceID, now, now).Find(&d.bindings).Error; err != nil {
		return nil, err
	}
	// Explicit current selections take precedence until another version is added.
	// Otherwise ordinal determines replacement, not labels or file dates.
	// Selecting within the business time also supports historical checklist reads.
	if err := s.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (v.file_id) v.* FROM shelf_document_versions v
 JOIN entities e ON e.id=v.file_id AND e.archived_at IS NULL AND e.merged_into_id=''
 WHERE v.library_id=? AND v.space_id=? AND v.occurred_at<=? ORDER BY v.file_id,
 CASE WHEN v.id=(SELECT selection.version_id FROM shelf_document_selections selection
 WHERE selection.file_id=v.file_id AND selection.selected_at<=?
 AND NOT EXISTS (SELECT 1 FROM shelf_document_versions newer WHERE newer.file_id=v.file_id AND newer.created_at>selection.selected_at AND newer.occurred_at<=?)
 ORDER BY selection.selected_at DESC,selection.id DESC LIMIT 1) THEN 0 ELSE 1 END,v.ordinal DESC`, l.ID, l.SpaceID, at, at, at).Scan(&d.documents).Error; err != nil {
		return nil, err
	}
	if cache != nil {
		cache.libraryID = l.ID
		cache.at = at
		cache.data = d
	}
	return d, nil
}

func (d *shelfChecklistData) ancestors(id string) []types.Entity {
	result := []types.Entity{}
	for depth := 0; depth < 3; depth++ {
		parent := ""
		for _, edge := range d.edges {
			if edge.SubjectID == id && edge.RelationType == "belongs_to" {
				parent = edge.ObjectID
				break
			}
		}
		if parent == "" {
			break
		}
		result = append(result, d.nodes[parent])
		id = parent
	}
	return result
}
func (d *shelfChecklistData) model(id string, combination map[string]string) (string, string, []types.ShelfChecklistItem) {
	model := d.nodes[id]
	ancestors := d.ancestors(id)
	stage, _ := model.Attributes["current_stage"].(string)
	stageOwner := ""
	if stage != "" {
		stageOwner = id
	}
	owners := map[string]int{id: 0}
	line := ""
	for _, e := range ancestors {
		switch e.Kind {
		case "product":
			owners[e.ID] = 2
			if stage == "" {
				stage, _ = e.Attributes["current_stage"].(string)
				if stage != "" {
					stageOwner = e.ID
				}
			}
		case "platform":
			owners[e.ID] = 3
		case "product_line":
			line = e.ID
		}
	}
	for _, edge := range d.edges {
		if edge.SubjectID == id && edge.RelationType == "composed_of" {
			owners[edge.ObjectID] = 1
		}
	}
	rows := []types.ShelfChecklistItem{}
	for _, r := range d.requirements {
		applies := false
		for _, b := range d.bindings {
			if b.SubjectID != r.ID {
				continue
			}
			if b.ObjectID == line {
				applies = true
			}
			if b.ObjectID == r.ID {
				var modules []string
				_ = types.DecodeShelfValue(b.Attributes["module_types"], &modules)
				for _, edge := range d.edges {
					if edge.SubjectID == id && edge.RelationType == "composed_of" {
						for _, module := range modules {
							if edge.Attributes["module_type"] == module {
								applies = true
							}
						}
					}
				}
			}
		}
		if !applies {
			continue
		}
		var record types.ShelfRequirement
		if _, ok := r.Attributes["requirement"]; !ok {
			continue
		}
		if types.DecodeShelfValue(r.Attributes["requirement"], &record) != nil {
			continue
		}

		universe := d.domain(id)
		domain := cloneShelfDomains(universe)
		if combination != nil {
			for module, part := range combination {
				domain[module] = []string{part}
			}
		}
		pending := []map[string][]string{domain}
		for rank := 0; rank < 4; rank++ {
			covered := []map[string][]string{}
			for i := range d.documents {
				v := &d.documents[i]
				if documentIDValue(v.RequirementID) != r.ID {
					continue
				}
				level, ok := owners[documentIDValue(v.OwnerID)]
				if !ok || level != rank {
					continue
				}
				admitted := cloneShelfDomains(domain)
				valid := true
				conditions := append([]types.ShelfCondition{}, v.Conditions...)
				if rank == 1 {
					for _, edge := range d.edges {
						if edge.SubjectID == id && edge.ObjectID == documentIDValue(v.OwnerID) && edge.RelationType == "composed_of" {
							module, _ := edge.Attributes["module_type"].(string)
							conditions = append(conditions, types.ShelfCondition{ModuleType: module, PartIDs: []string{documentIDValue(v.OwnerID)}})
						}
					}
				}
				for _, c := range conditions {
					admitted[c.ModuleType] = shelfPartIntersection(admitted[c.ModuleType], c.PartIDs)
					if len(admitted[c.ModuleType]) == 0 {
						valid = false
					}
				}
				if !valid {
					continue
				}
				for _, box := range pending {
					intersection, ok := shelfBoxIntersection(box, admitted)
					if !ok {
						continue
					}
					row := d.row(r, record, stage, v, rank, intersection, universe, combination)
					rows = append(rows, row)
					covered = append(covered, intersection)
				}
			}
			for _, box := range covered {
				pending = shelfSubtractBoxes(pending, box)
			}
		}
		for _, box := range pending {
			rows = append(rows, d.row(r, record, stage, nil, 4, box, universe, combination))
		}

	}
	// Preserve conflicting same-layer candidates without making up a tie-break.
	for i := range rows {
		for j := 0; j < i; j++ {
			if rows[i].Requirement.ID == rows[j].Requirement.ID && rows[i].Document != nil && rows[j].Document != nil && shelfConditionsOverlap(rows[i].Conditions, rows[j].Conditions) {
				rows[i].Ambiguous = true
				rows[j].Ambiguous = true
			}
		}
	}
	return stage, stageOwner, rows
}
func (d *shelfChecklistData) status(stage string, r types.ShelfRequirement, v *types.ShelfDocumentVersion) (string, string) {
	if stage == "" {
		if v != nil {
			return "has_file", "有文件"
		}
		return "no_file", "无文件"
	}
	order := map[string]int{}
	for i, s := range d.library.ShelfConfig.Stages {
		order[s.Key] = i
	}
	due := func(key string) bool {
		p, ok := order[key]
		current, known := order[stage]
		return key != "" && ok && known && current >= p
	}
	if v != nil && v.Stage == "final" {
		return "delivered", "已交"
	}
	if due(r.FinalDueStage) {
		return "missing", "缺"
	}
	if v != nil && v.Stage == "draft" {
		return "draft_only", "只有初稿"
	}
	if due(r.DraftDueStage) {
		return "missing", "缺"
	}
	return "not_due", "未到期"
}
func (d *shelfChecklistData) apply(entity *types.Entity, result *types.ShelfModelDetail, combination map[string]string) {
	if entity.Kind == "model" {
		result.CurrentStage, result.StageOwnerID, result.Checklist = d.model(entity.ID, combination)
		result.OtherDocuments = d.otherDocuments(entity.ID, result.Composition, combination)
		return
	}
	result.Documents = []types.ShelfNodeDocument{}
	for _, document := range d.documents {
		if documentIDValue(document.OwnerID) != entity.ID {
			continue
		}
		row := types.ShelfNodeDocument{Entity: d.nodes[document.FileID], Document: document}
		if requirement, ok := d.nodes[documentIDValue(document.RequirementID)]; ok {
			row.Requirement = &requirement
		}
		result.Documents = append(result.Documents, row)
	}
	sort.Slice(result.Documents, func(i, j int) bool {
		left, right := result.Documents[i].Entity, result.Documents[j].Entity
		if left.CanonicalName == right.CanonicalName {
			return left.ID < right.ID
		}
		return left.CanonicalName < right.CanonicalName
	})
	if entity.Kind == "part" {
		result.Checklist = d.partChecklist(entity)
		return
	}
	summary := &types.ShelfChecklistSummary{Counts: map[string]int{}, Models: []types.ShelfChecklistModelSummary{}}
	ids := []string{}
	for id, e := range d.nodes {
		if e.Kind != "model" || e.ArchivedAt != nil || e.MergedIntoID != "" || !d.recorded[id] {
			continue
		}
		for _, a := range d.ancestors(id) {
			if a.ID == entity.ID {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		stage, _, rows := d.model(id, nil)
		m := types.ShelfChecklistModelSummary{ModelID: id, CurrentStage: stage, Counts: map[string]int{}}
		counted := map[string]bool{}
		for _, r := range rows {
			key := r.Requirement.ID + ":" + r.Status
			if !counted[key] {
				m.Counts[r.Status]++
				summary.Counts[r.Status]++
				counted[key] = true
			}
			if r.Status == "missing" {
				m.HasGaps = true
			}
		}
		summary.Models = append(summary.Models, m)
		summary.ModelCount++
		if m.HasGaps {
			summary.ModelsWithGaps++
		}
	}
	result.ChecklistSummary = summary
}

// Parts have module requirements, but no stage or sale status of their own.
func (d *shelfChecklistData) partChecklist(entity *types.Entity) []types.ShelfChecklistItem {
	rows := []types.ShelfChecklistItem{}
	module, _ := entity.Attributes["module_type"].(string)
	for _, requirement := range d.requirements {
		applies := false
		for _, binding := range d.bindings {
			if binding.SubjectID != requirement.ID || binding.ObjectID != requirement.ID {
				continue
			}
			var modules []string
			_ = types.DecodeShelfValue(binding.Attributes["module_types"], &modules)
			applies = applies || slices.Contains(modules, module)
		}
		if !applies {
			continue
		}
		var record types.ShelfRequirement
		if types.DecodeShelfValue(requirement.Attributes["requirement"], &record) != nil {
			continue
		}
		found := false
		for i := range d.documents {
			document := &d.documents[i]
			if documentIDValue(document.OwnerID) != entity.ID || documentIDValue(document.RequirementID) != requirement.ID {
				continue
			}
			row := d.row(requirement, record, "", document, 0, nil, nil, nil)
			row.Conditions = append([]types.ShelfCondition{}, document.Conditions...)
			rows = append(rows, row)
			found = true
		}
		if !found {
			rows = append(rows, d.row(requirement, record, "", nil, 0, nil, nil, nil))
		}
	}
	for i := range rows {
		for j := 0; j < i; j++ {
			if rows[i].Requirement.ID == rows[j].Requirement.ID && rows[i].Document != nil && rows[j].Document != nil && shelfConditionsOverlap(rows[i].Conditions, rows[j].Conditions) {
				rows[i].Ambiguous, rows[j].Ambiguous = true, true
			}
		}
	}
	return rows
}

// Other files use the same current-version snapshot as the checklist, but have
// no requirement. No precedence hides independently named supporting files.
func (d *shelfChecklistData) otherDocuments(id string, composition []types.ShelfComposition, combination map[string]string) []types.ShelfOtherDocument {
	owners := map[string]string{id: "self"}
	for _, ancestor := range d.ancestors(id) {
		if ancestor.Kind == "product" || ancestor.Kind == "platform" {
			owners[ancestor.ID] = "ancestor"
		}
	}
	for _, group := range composition {
		for _, part := range group.Parts {
			owners[part.ID] = "component"
		}
	}
	rows := []types.ShelfOtherDocument{}
	for _, document := range d.documents {
		origin, ok := owners[documentIDValue(document.OwnerID)]
		if !ok || documentIDValue(document.RequirementID) != "" {
			continue
		}
		conditions := append([]types.ShelfCondition{}, document.Conditions...)
		if origin == "component" {
			for _, group := range composition {
				if slices.ContainsFunc(group.Parts, func(part types.Entity) bool { return part.ID == documentIDValue(document.OwnerID) }) {
					conditions = append(conditions, types.ShelfCondition{ModuleType: group.ModuleType, PartIDs: []string{documentIDValue(document.OwnerID)}})
				}
			}
		}
		if shelfConditionsMatch(conditions, combination) {
			rows = append(rows, types.ShelfOtherDocument{Entity: d.nodes[document.FileID], Document: document, Owner: d.nodes[documentIDValue(document.OwnerID)], Origin: origin, Conditions: conditions})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Entity.CanonicalName == rows[j].Entity.CanonicalName {
			return rows[i].Entity.ID < rows[j].Entity.ID
		}
		return rows[i].Entity.CanonicalName < rows[j].Entity.CanonicalName
	})
	return rows
}

func (s *EntityService) addShelfChecklist(ctx context.Context, l *types.Library, e *types.Entity, at time.Time, combination map[string]string, result *types.ShelfModelDetail) error {
	if e.Kind != "model" && e.Kind != "product" && e.Kind != "platform" && e.Kind != "product_line" && e.Kind != "part" {
		return nil
	}
	data, err := s.loadShelfChecklist(ctx, l, at)
	if err != nil {
		return err
	}
	data.apply(e, result, combination)
	return nil
}

func (d *shelfChecklistData) domain(id string) map[string][]string {
	domain := map[string][]string{}
	for _, edge := range d.edges {
		if edge.SubjectID == id && edge.RelationType == "composed_of" {
			module, _ := edge.Attributes["module_type"].(string)
			domain[module] = append(domain[module], edge.ObjectID)
		}
	}
	return domain
}
func (d *shelfChecklistData) row(r types.Entity, record types.ShelfRequirement, stage string, v *types.ShelfDocumentVersion, rank int, box, universe map[string][]string, combination map[string]string) types.ShelfChecklistItem {
	row := types.ShelfChecklistItem{Requirement: r, Conditions: []types.ShelfCondition{}, Document: v}
	keys := []string{}
	for key := range box {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, module := range keys {
		if len(box[module]) < len(universe[module]) {
			parts := slices.Clone(box[module])
			sort.Strings(parts)
			row.Conditions = append(row.Conditions, types.ShelfCondition{ModuleType: module, PartIDs: parts})
			row.PartialOptions = combination == nil
		}
	}
	if v != nil {
		row.HasFile = true
		row.DocumentName = d.nodes[v.FileID].CanonicalName
		row.OwnerID = documentIDValue(v.OwnerID)
		row.Origin, row.OriginLabel = "self", "自身"
		if rank == 1 {
			row.Origin, row.OriginLabel = "component", "来自部件"
		}
		if rank > 1 {
			row.Origin, row.OriginLabel = "ancestor", "来自上层"
		}
	}
	row.Status, row.StatusLabel = d.status(stage, record, v)
	return row
}

// Intersect and subtract rectangular part domains without enumerating complete
// configurations. Empty maps represent the single configuration with no parts.
func shelfBoxIntersection(left, right map[string][]string) (map[string][]string, bool) {
	result := map[string][]string{}
	for module, parts := range left {
		result[module] = shelfPartIntersection(parts, right[module])
		if len(result[module]) == 0 {
			return nil, false
		}
	}
	return result, true
}
func shelfSubtractBoxes(boxes []map[string][]string, covered map[string][]string) []map[string][]string {
	result := []map[string][]string{}
	for _, box := range boxes {
		intersection, ok := shelfBoxIntersection(box, covered)
		if !ok {
			result = append(result, box)
			continue
		}
		keys := []string{}
		for key := range box {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		prefix := cloneShelfDomains(box)
		for _, module := range keys {
			outside := []string{}
			for _, id := range prefix[module] {
				if !slices.Contains(intersection[module], id) {
					outside = append(outside, id)
				}
			}
			if len(outside) > 0 {
				piece := cloneShelfDomains(prefix)
				piece[module] = outside
				result = append(result, piece)
			}
			prefix[module] = intersection[module]
		}
	}
	return result
}
