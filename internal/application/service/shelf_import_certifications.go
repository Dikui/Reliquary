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
	owner, err := b.recordOwner(mapping, cells["owner:"].Value)
	if err != nil {
		return err
	}
	standard, ok := b.library.ShelfConfig.Standard(cells["standard:"].Value)
	if !ok {
		return fmt.Errorf("uncertain: certification standard has no configured name or alias")
	}
	cellPartName := ""
	record := types.ShelfCertification{OwnerID: owner.ID, Standard: standard.Key, CertificationObject: cells["certification_object:"].Value, Region: cells["region:"].Value}
	if cell, ok := cells["cell_part:"]; ok && cell.Value != "" {
		part, err := b.recordEntity("part", cell.Value)
		if err != nil {
			return err
		}
		record.CellPartID = part.ID
		cellPartName = part.CanonicalName
	}
	key := fmt.Sprintf("cert:%q/%q/%q/%q/%q", record.OwnerID, record.Standard, record.CertificationObject, record.CellPartID, record.Region)
	if b.seenRecordKeys[key] {
		return fmt.Errorf("uncertain: certification key appears in multiple rows; submitter must decide whether to merge")
	}

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
		record.Year = 0
		if cell.Value != "" {
			year, err := strconv.Atoi(cell.Value)
			if err != nil {
				return fmt.Errorf("invalid certification year %q", cell.Value)
			}
			record.Year = year
		}
	}
	b.seenRecordKeys[key] = true
	opType, name := "update_certification", cells["name:"].Value
	if len(matches) == 0 {
		opType = "create_certification"
		if name == "" {
			names := []string{owner.CanonicalName, standard.Name, record.CertificationObject}
			if cellPartName != "" {
				names = append(names, cellPartName)
			}
			if record.Region != "" {
				regionName := record.Region
				for _, region := range b.library.ShelfConfig.Regions {
					if region.Key == record.Region {
						regionName = region.Name
						break
					}
				}
				names = append(names, regionName)
			}
			name = strings.Join(names, " / ")
		}
	}
	op := types.ShelfOperation{Type: opType, Revision: revision, Certification: &types.ShelfCertificationInput{Name: name, ShelfCertification: record}}
	if len(matches) > 1 {
		op.Issues = []types.ShelfConflict{{Type: "uncertain", Message: "certification key has multiple candidates; submit a corrected complete record", Details: map[string]any{"candidates": matches, "match_count": len(matches)}}}
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
	for _, field := range []string{"pack_model", "cluster_model", "container_model"} {
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
		return nil
	}
	for _, modelName := range b.importModelNames(modelName) {
		if err := b.reportComposition(mapping, part.ID, modelName); err != nil {
			return err
		}
	}
	return nil
}

func (b *importMapBuilder) reportComposition(mapping types.ShelfImportSheet, partID, modelName string) error {
	model, err := b.recordEntity("model", modelName)
	if err != nil {
		return err
	}
	for _, op := range b.ops {
		if op.Relation != nil && op.Relation.RelationType == "composed_of" && op.Relation.SubjectID == model.ID && op.Relation.ObjectID == partID {
			return nil
		}
	}
	var relations []types.Relation
	if err := b.service.db.WithContext(b.ctx).Where("library_id=? AND space_id=? AND subject_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", b.library.ID, b.library.SpaceID, model.ID, partID, b.at, b.at).Find(&relations).Error; err != nil {
		return err
	}
	for _, relation := range relations {
		if relation.Attributes["module_type"] != mapping.ModuleType {
			return fmt.Errorf("uncertain: report composition module differs from the shelf")
		}
	}
	if len(relations) == 0 {
		b.append(types.ShelfOperation{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: model.ID, ObjectID: partID, RelationType: "composed_of", Attributes: map[string]any{"module_type": mapping.ModuleType}}})
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
