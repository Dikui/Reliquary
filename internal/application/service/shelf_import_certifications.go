package service

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"reliquary/internal/types"
)

func (b *importMapBuilder) certificationRow(mapping types.ShelfImportSheet, cells map[string]importCell) error {
	if err := b.validateRecordCells(cells); err != nil {
		return err
	}
	if b.plan.FileKind == "certification_report" {
		if err := b.reportPart(mapping, cells); err != nil {
			return err
		}
	}
	owner, ownerErr := b.recordOwner(mapping, cells["owner:"].Value)
	if ownerErr != nil {
		owner = types.Entity{}
	}
	standard, year, standardErr := b.library.ShelfConfig.ResolveStandardYear(cells["standard:"].Value, 0)
	if standardErr != nil {
		year = 0
		standard.Key = cells["standard:"].Value
	}
	record := types.ShelfCertification{Year: year, OwnerID: owner.ID, Standard: standard.Key, CertificationObject: cells["certification_object:"].Value, Region: cells["region:"].Value}
	if cell, ok := cells["cell_part:"]; ok && cell.Value != "" {
		part, err := b.recordEntity("part", cell.Value)
		if err != nil {
			return err
		}
		record.CellPartID = part.ID
	}
	key := fmt.Sprintf("cert:%q/%q/%q/%q/%q", record.OwnerID, record.Standard, record.CertificationObject, record.CellPartID, record.Region)
	completeKey := owner.ID != "" && standardErr == nil && strings.TrimSpace(record.CertificationObject) != "" && strings.TrimSpace(record.Region) != ""
	duplicate := completeKey && b.seenRecordKeys[key]

	matches := []types.Entity{}
	for _, entity := range b.entities {
		if entity.Kind != "certification" {
			continue
		}
		var existing types.ShelfCertification
		if raw, ok := entity.Attributes["certification"]; ok {
			if err := types.DecodeShelfValue(raw, &existing); err != nil {
				return err
			}
			if shelfCertificationKeyEqual(existing, record) {
				matches = append(matches, entity)
			}
		}
	}
	revision := int64(0)
	if len(matches) == 1 {
		if err := types.DecodeShelfValue(matches[0].Attributes["certification"], &record); err != nil {
			return err
		}
		revision = matches[0].Revision
		if year != 0 {
			record.Year = year
		}
	}
	if b.plan.FileKind == "certification_report" {
		if cell, ok := cells["progress:"]; ok && cell.Value != mapping.Progress {
			return fmt.Errorf("uncertain: report progress differs from the confirmed completed spelling")
		}
		record.Progress = mapping.Progress
	}
	for field, target := range map[string]*string{"progress": &record.Progress, "agency": &record.Agency, "validity": &record.Validity, "number": &record.Number, "cloud_url": &record.CloudURL} {
		if cell, ok := cells[field+":"]; ok {
			*target = cell.Value
		}
	}
	for _, dateField := range []struct {
		field  string
		target *string
	}{{"start_date", &record.StartDate}, {"obtained_date", &record.ObtainedDate}, {"issued_date", &record.IssuedDate}} {
		field, target := dateField.field, dateField.target
		if cell, ok := cells[field+":"]; ok {
			*target = ""
			if cell.Value == "" {
				continue
			}
			value, err := importCertificationDate(cell)
			if err != nil {
				return fmt.Errorf("invalid %s date %q; retain original and ask submitter", field, cell.Value)
			}
			*target = value
		}
	}
	if cell, ok := cells["year:"]; ok {
		record.Year = year
		if cell.Value != "" {
			fieldYear, err := strconv.Atoi(cell.Value)
			if err != nil {
				return fmt.Errorf("invalid certification year %q", cell.Value)
			}
			record.Year = fieldYear
			if year != 0 && year != fieldYear {
				record.Standard = cells["standard:"].Value
			}
		}
	}

	if completeKey {
		b.seenRecordKeys[key] = true
	}
	mappedFields := make([]string, 0, len(mapping.Columns))
	for _, column := range mapping.Columns {
		mappedFields = append(mappedFields, column.Field)
	}
	name, hasNameColumn := cells["name:"]
	if !hasNameColumn {
		name = cells["standard:"]
	}
	op := types.ShelfOperation{Type: "update_certification", CertificationOwner: &types.ShelfCertificationOwner{MappedFields: mappedFields, Name: cells["owner:"].Value, Kind: mapping.OwnerKind, ProductLineID: mapping.ProductLineID}, Revision: revision, Certification: &types.ShelfCertificationInput{Name: name.Value, ShelfCertification: record}}
	if duplicate {
		op.Issues = append(op.Issues, types.ShelfConflict{Type: "uncertain", Message: "certification key appears in multiple rows; submitter must decide whether to merge"})
	}
	if len(matches) > 1 {
		op.Issues = append(op.Issues, types.ShelfConflict{Type: "uncertain", Message: "certification key has multiple candidates; submit a corrected complete record", Details: map[string]any{"candidates": matches, "match_count": len(matches)}})
	}
	if text := cells["sample_description:"].Value; text != "" {
		op.Issues = append(op.Issues, types.ShelfConflict{Type: "uncertain", Message: "sample description requires explicit part-condition review", Details: map[string]any{"kind": "conditions", "text": text}})
	}
	b.append(op)
	return nil
}

func (b *importMapBuilder) reportPart(mapping types.ShelfImportSheet, cells map[string]importCell) error {
	name := cells["cell_part:"].Value
	if name == "" {
		for _, field := range []string{"pack_model:", "cluster_model:", "container_model:"} {
			if cells[field].Value != "" {
				return fmt.Errorf("uncertain: part model attributes require an identified cell part")
			}
		}
		return nil
	}
	if slices.Contains(b.plan.Definition.MissingValues, name) {
		return fmt.Errorf("uncertain: cell part is a configured placeholder")
	}
	if mapping.ModuleType == "" {
		return fmt.Errorf("uncertain: report cell part requires a configured module_type")
	}
	part, err := b.recordEntity("part", name)
	if err != nil && part.ID != "" {
		return err
	}
	// A missing exact identity can be proposed as a new part, never a guessed alias.
	attrs := map[string]any{}
	for k, v := range part.Attributes {
		attrs[k] = v
	}
	if module, _ := attrs["module_type"].(string); module != "" && module != mapping.ModuleType {
		return fmt.Errorf("uncertain: part module differs from the report mapping")
	}
	attrs["module_type"] = mapping.ModuleType
	for _, field := range []string{"pack_model"} {
		if cell, ok := cells[field+":"]; ok {
			attrs[field] = cell.Value
		}
	}
	if part.ID == "" {
		part.ID = b.append(types.ShelfOperation{Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "part", Name: name, Attributes: attrs}})
	} else if !reflect.DeepEqual(attrs, part.Attributes) {
		for _, op := range b.ops {
			if op.EntityID == part.ID && op.Type == "update_entity" || "$"+op.ID == part.ID {
				return fmt.Errorf("uncertain: repeated cell part rows propose different attributes")
			}
		}
		b.append(types.ShelfOperation{Type: "update_entity", EntityID: part.ID, Patch: &types.ManagedEntityPatch{Revision: part.Revision, Attributes: &attrs}})
	}
	modelName := cells["model:"].Value
	if modelName == "" && mapping.OwnerKind == "model" {
		modelName = cells["owner:"].Value
	}
	if modelName == "" {
		if cells["cluster_model:"].Value != "" || cells["container_model:"].Value != "" {
			return fmt.Errorf("uncertain: cluster and container models require an identified composition model")
		}
		return nil
	}
	compositionAttrs := map[string]any{"module_type": mapping.ModuleType}
	for _, field := range []string{"cluster_model", "container_model"} {
		if cell, ok := cells[field+":"]; ok {
			compositionAttrs[field] = cell.Value
		}
	}
	for _, modelName := range b.importModelNames(modelName) {
		if err := b.reportComposition(mapping, part.ID, modelName, compositionAttrs); err != nil {
			return err
		}
	}
	return nil
}

func (b *importMapBuilder) reportComposition(mapping types.ShelfImportSheet, partID, modelName string, attrs map[string]any) error {
	model, err := b.recordEntity("model", modelName)
	if err != nil {
		// Keep the declared composition as a separate, correctable proposal. An
		// unknown model does not erase the report's certification or part fields.
		b.append(types.ShelfOperation{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: "", ObjectID: partID, RelationType: "composed_of", Attributes: attrs}, Issues: []types.ShelfConflict{{Type: "uncertain", Message: "report composition model needs an explicit identity", Details: map[string]any{"owner_name": modelName, "owner_kind": "model"}}}})
		return nil
	}
	key := model.ID + "/" + partID
	if previous, ok := b.compositionRows[key]; ok {
		if !reflect.DeepEqual(previous, attrs) {
			return fmt.Errorf("uncertain: repeated model and cell composition rows propose different attributes")
		}
		return nil
	}
	b.compositionRows[key] = attrs
	var relations []types.Relation
	if err := b.service.db.WithContext(b.ctx).Where("library_id=? AND space_id=? AND subject_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", b.library.ID, b.library.SpaceID, model.ID, partID, b.at, b.at).Find(&relations).Error; err != nil {
		return err
	}
	for _, relation := range relations {
		if relation.Attributes["module_type"] != mapping.ModuleType {
			return fmt.Errorf("uncertain: report composition module differs from the shelf")
		}
		next := map[string]any{}
		for key, value := range relation.Attributes {
			next[key] = value
		}
		for key, value := range attrs {
			next[key] = value
		}
		if !reflect.DeepEqual(next, relation.Attributes) {
			b.append(types.ShelfOperation{Type: "save_relation", RelationID: relation.ID, Revision: relation.Version, Relation: &types.ManagedRelationInput{SubjectID: model.ID, ObjectID: partID, RelationType: "composed_of", Version: relation.Version, Attributes: next}})
		}
	}
	if len(relations) == 0 {
		b.append(types.ShelfOperation{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: model.ID, ObjectID: partID, RelationType: "composed_of", Attributes: attrs}})
	}
	return nil
}

func importCertificationDate(cell importCell) (string, error) {
	if cell.DateSerial {
		serial, err := strconv.Atoi(cell.Value)
		if err != nil || serial < 0 || serial > 2958465 || !cell.Date1904 && (serial == 0 || serial == 60) {
			return "", fmt.Errorf("invalid Excel date serial")
		}
		base := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
		if cell.Date1904 {
			base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
		} else if serial > 60 {
			serial--
		}
		date := base.AddDate(0, 0, serial)
		if date.Year() > 9999 {
			return "", fmt.Errorf("Excel date is out of range")
		}
		return date.Format("2006-01-02"), nil
	}
	for _, layout := range []string{"2006-01-02", "2006/1/2", "2006.1.2", "2006年1月2日"} {
		if date, err := time.Parse(layout, cell.Value); err == nil {
			return date.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("date cannot be parsed")
}
