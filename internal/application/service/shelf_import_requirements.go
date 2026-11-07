package service

import (
	"fmt"
	"slices"
	"strings"

	"reliquary/internal/types"
)

func (b *importMapBuilder) requirementRow(mapping types.ShelfImportSheet, cells map[string]importCell) error {
	if err := b.validateRecordCells(cells); err != nil {
		return err
	}
	name := importRequirementName(cells)
	if strings.TrimSpace(name) == "" || slices.Contains(b.plan.Definition.MissingValues, name) {
		return fmt.Errorf("uncertain: requirement name is missing or a placeholder")
	}
	identity := normalizeEntityName(name)
	if b.seenModelNames[identity] {
		return fmt.Errorf("uncertain: requirement appears in multiple rows; submitter must decide whether to merge")
	}
	draft, final, err := b.importDueStages(cells["due_stages:"].Value)
	if err != nil {
		return err
	}
	existing, err := b.recordEntity("document_requirement", name)
	if err != nil && existing.ID != "" {
		return err
	}
	if existing.ID != "" && b.seenModelIDs[existing.ID] {
		return fmt.Errorf("uncertain: multiple rows resolve to the same requirement")
	}
	requirement := types.ShelfRequirement{}
	if raw, ok := existing.Attributes["requirement"]; ok {
		if err := types.DecodeShelfValue(raw, &requirement); err != nil {
			return err
		}
	}
	requirement.DraftDueStage, requirement.FinalDueStage = draft, final
	requirement.DueText = cells["due_stages:"].Value
	for field, target := range map[string]*string{"name_zh": &requirement.NameZH, "name_en": &requirement.NameEN, "file_category": &requirement.FileCategory, "stage_label": &requirement.StageLabel, "owner": &requirement.Owner, "priority": &requirement.Priority, "source_list": &requirement.SourceList, "source_version": &requirement.SourceVersion} {
		if cell, ok := cells[field+":"]; ok {
			*target = cell.Value
		}
	}
	markets := map[string]string{}
	for k, v := range requirement.Markets {
		markets[k] = v
	}
	for _, column := range mapping.Columns {
		if column.Field != "market" {
			continue
		}
		raw := cells["market:"+column.Key].Value
		value := raw
		switch raw {
		case "必需":
			value = "required"
		case "可选":
			value = "optional"
		case "不需要":
			value = "not_needed"
		}
		if !slices.Contains([]string{"required", "optional", "not_needed"}, value) {
			return fmt.Errorf("uncertain: market necessity %q has no explicit meaning", raw)
		}
		markets[column.Key] = value
	}
	requirement.Markets = markets
	attrs := map[string]any{}
	for k, v := range existing.Attributes {
		attrs[k] = v
	}
	attrs["requirement"] = requirement
	id := existing.ID
	if id == "" {
		id = b.append(types.ShelfOperation{Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "document_requirement", Name: name, Attributes: attrs}})
	} else {
		b.append(types.ShelfOperation{Type: "update_entity", EntityID: id, Patch: &types.ManagedEntityPatch{Revision: existing.Revision, Attributes: &attrs}})
	}
	target := mapping.ProductLineID
	bindingAttrs := map[string]any{}
	if mapping.ModuleType != "" {
		target = id
		bindingAttrs["module_types"] = []string{mapping.ModuleType}
	}
	var bindings []types.Relation
	if err := b.service.db.WithContext(b.ctx).Where("library_id=? AND space_id=? AND subject_id=? AND object_id=? AND relation_type='applies_to' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", b.library.ID, b.library.SpaceID, id, target, b.at, b.at).Find(&bindings).Error; err != nil {
		return err
	}
	if len(bindings) == 0 {
		b.append(types.ShelfOperation{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: id, ObjectID: target, RelationType: "applies_to", Attributes: bindingAttrs}})
	} else if mapping.ModuleType != "" {
		var modules []string
		if err := types.DecodeShelfValue(bindings[0].Attributes["module_types"], &modules); err != nil {
			return err
		}
		if !slices.Contains(modules, mapping.ModuleType) {
			return fmt.Errorf("uncertain: requirement module binding differs from the shelf")
		}
	}
	b.seenModelNames[identity] = true
	b.seenModelIDs[id] = true
	return nil
}

// Only configured stage names/keys and explicit draft/final markers have meaning.
// Other wording can be handled by a confirmed exact-text deadline mapping.
func (b *importMapBuilder) importDueStages(raw string) (string, string, error) {
	if mapped, ok := b.plan.Definition.Deadlines[raw]; ok {
		return mapped.DraftStage, mapped.FinalStage, nil
	}
	stage := func(raw string) string {
		raw = strings.TrimSpace(raw)
		found := ""
		for _, option := range b.library.ShelfConfig.Stages {
			if option.Name == raw || option.Key == raw {
				if found != "" && found != option.Key {
					return ""
				}
				found = option.Key
			}
		}
		return found
	}
	if final := stage(raw); final != "" {
		return "", final, nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '；' || r == '\n' })
	draft, final := "", ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		label := ""
		for _, candidate := range []string{"初稿", "终稿"} {
			for _, separator := range []string{":", "："} {
				if text, ok := strings.CutPrefix(part, candidate+separator); ok {
					label = candidate
					part = text
				}
			}
			for _, brackets := range [][2]string{{"（", "）"}, {"(", ")"}} {
				if text, ok := strings.CutSuffix(part, brackets[0]+candidate+brackets[1]); ok {
					label = candidate
					part = text
				}
			}
		}
		value := stage(part)
		if label == "" || value == "" {
			return "", "", fmt.Errorf("uncertain: R&D deadline %q needs a confirmed stage mapping", raw)
		}
		if label == "初稿" {
			if draft != "" {
				return "", "", fmt.Errorf("uncertain: duplicate draft deadline")
			}
			draft = value
		} else {
			if final != "" {
				return "", "", fmt.Errorf("uncertain: duplicate final deadline")
			}
			final = value
		}
	}
	if final == "" {
		return "", "", fmt.Errorf("uncertain: R&D deadline %q has no identifiable final stage", raw)
	}
	return draft, final, nil
}

// Active requirements from earlier confirmed editions remain tracked even when
// a later edition contains only rejected rows or ignores their worksheet.
func (b *importMapBuilder) disappearedRequirements() error {
	var parts []types.ShelfChangeset
	if err := b.service.db.WithContext(b.ctx).Where("space_id=? AND library_id=? AND status='confirmed' AND source->>'import_plan_key'=?", b.library.SpaceID, b.library.ID, b.plan.Key).Order("created_at DESC,id DESC").Find(&parts).Error; err != nil {
		return err
	}
	entities := map[string]types.Entity{}
	for _, entity := range b.entities {
		if entity.Kind == "document_requirement" {
			entities[entity.ID] = entity
		}
	}
	seen := map[string]bool{}
	for _, part := range parts {
		for _, item := range part.Items {
			if item.Decision == nil {
				continue
			}
			id := shelfReference(&part, item.EntityID)
			if item.Decision.Action == "same_entity" {
				id = item.Decision.TargetEntityID
			}
			entity, ok := entities[id]
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			location := item.Operation.ImportLocation
			if location != nil && b.skippedSheets[location.Worksheet] {
				continue
			}
			present := false
			for _, name := range append([]string{entity.CanonicalName, entity.Code}, entity.Aliases...) {
				present = present || b.present[normalizeEntityName(name)]
			}
			if !present {
				b.location = location
				b.append(types.ShelfOperation{Type: "missing_entity", EntityID: id, Revision: entity.Revision})
			}
		}
	}
	return nil
}

func importRequirementName(cells map[string]importCell) string {
	for _, field := range []string{"name:", "name_zh:", "name_en:"} {
		if cell, ok := cells[field]; ok {
			return cell.Value
		}
	}
	return ""
}
