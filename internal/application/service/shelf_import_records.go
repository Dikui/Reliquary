package service

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *EntityService) validateRecordImportSheet(ctx context.Context, library *types.Library, kind string, sheet types.ShelfImportSheet) error {
	if kind != "atq" && !slices.Contains([]string{"model", "part", "product", "platform"}, sheet.OwnerKind) {
		return apperrors.NewBadRequestError("record worksheet requires an explicit owner_kind")
	}
	if sheet.ModuleType != "" && !shelfOptionExists(library.ShelfConfig.ModuleTypes, sheet.ModuleType) {
		return apperrors.NewBadRequestError("worksheet module_type must be configured")
	}
	if kind == "certification_report" && strings.TrimSpace(sheet.Progress) == "" {
		return apperrors.NewBadRequestError("report worksheet requires a confirmed completed progress spelling")
	}
	if kind == "atq" {
		if (sheet.ProductLineID == "") == (sheet.ModuleType == "") {
			return apperrors.NewBadRequestError("ATQ worksheet requires exactly one product line or module binding")
		}

	}
	if sheet.ProductLineID != "" {
		line, err := s.shelfEntity(ctx, library.ID, sheet.ProductLineID)
		if err != nil {
			return err
		}
		if line.Kind != "product_line" || line.ArchivedAt != nil {
			return apperrors.NewBadRequestError("worksheet requires an active product line")
		}
	}

	for raw := range sheet.CheckValues {
		if strings.TrimSpace(raw) == "" || kind != "overseas_sales" {
			return apperrors.NewBadRequestError("check_values require nonblank overseas flags")
		}
	}
	fields, columns := map[string]bool{}, map[string]bool{}
	for _, c := range sheet.Columns {
		if !shelfImportColumn.MatchString(c.Column) || importColumnNumber(c.Column) > 16384 || columns[c.Column] || fields[c.Field+":"+c.Key] {
			return apperrors.NewBadRequestError("import columns and mapped fields must be distinct")
		}
		columns[c.Column], fields[c.Field+":"+c.Key] = true, true
		if kind == "overseas_sales" && c.Field == "certification" {
			standard, ok := library.ShelfConfig.Standard(c.Key)
			if !ok || standard.Key != c.Key || strings.TrimSpace(sheet.Progress) == "" || len(sheet.CheckValues) == 0 {
				return apperrors.NewBadRequestError("certification checks require a configured standard key, completed progress spelling and check_values")
			}
			continue
		}
		if (kind == "overseas_sales" && c.Field == "sale_status") || (kind == "atq" && c.Field == "market") {
			if !shelfOptionExists(library.ShelfConfig.Regions, c.Key) {
				return apperrors.NewBadRequestError("sales check requires a configured region")
			}
			continue
		}
		if kind == "atq" {
			if c.Key != "" || !slices.Contains([]string{"name", "name_zh", "name_en", "file_category", "stage_label", "due_stages", "owner", "priority", "source_list", "source_version"}, c.Field) {
				return apperrors.NewBadRequestError("unknown ATQ import field")
			}
			continue
		}
		allowed := []string{"name", "owner", "standard", "year", "certification_object", "cell_part", "region", "agency", "progress", "start_date", "obtained_date", "issued_date", "validity", "number", "cloud_url"}
		if kind == "certification_report" {
			allowed = append(allowed, "model", "pack_model", "cluster_model", "container_model")
		}
		if kind == "overseas_sales" {
			allowed = []string{"owner", "standard", "certification_object", "cell_part", "region", "agency", "progress", "validity", "number", "cloud_url"}
		}
		if c.Key != "" || !slices.Contains(allowed, c.Field) {
			return apperrors.NewBadRequestError("unknown field for this import file_kind")
		}
	}
	required := []string{"owner:", "standard:", "certification_object:", "region:"}
	if kind == "overseas_sales" {
		required = []string{"owner:"}
	}
	if kind == "atq" {
		required = []string{"name:", "due_stages:"}
		if !fields["name:"] {
			if fields["name_zh:"] {
				required[0] = "name_zh:"
			} else if fields["name_en:"] {
				required[0] = "name_en:"
			}
		}
	}
	for _, field := range required {
		if !fields[field] {
			return apperrors.NewBadRequestError("worksheet is missing required field " + strings.TrimSuffix(field, ":"))
		}
	}
	if kind == "overseas_sales" {
		hasCheck := fields["standard:"] || slices.ContainsFunc(sheet.Columns, func(c types.ShelfImportColumn) bool { return c.Field == "sale_status" || c.Field == "certification" })
		if !hasCheck {
			return apperrors.NewBadRequestError("overseas worksheet needs a status or certification check")
		}
		metadata := slices.ContainsFunc(sheet.Columns, func(c types.ShelfImportColumn) bool {
			return slices.Contains([]string{"agency", "progress", "number", "validity", "cloud_url", "cell_part"}, c.Field)
		})
		if metadata && !fields["standard:"] {
			return apperrors.NewBadRequestError("certification metadata checks require a standard column")
		}
	}

	return nil
}

// Resolve only exact names, codes and confirmed aliases within the chosen kind.
func (b *importMapBuilder) recordEntity(kind, name string) (types.Entity, error) {
	var found types.Entity
	for _, op := range b.ops {
		if op.Type == "create_entity" && op.Entity != nil && op.Entity.Kind == kind && normalizeEntityName(op.Entity.Name) == normalizeEntityName(name) {
			found = types.Entity{ID: "$" + op.ID, Kind: kind, CanonicalName: op.Entity.Name, Attributes: op.Entity.Attributes, Revision: 1}
		}
	}
	if strings.TrimSpace(name) == "" || slices.Contains(b.plan.Definition.MissingValues, name) {
		return found, fmt.Errorf("uncertain: missing %s identity", kind)
	}
	for _, entity := range b.entities {
		if entity.Kind != kind {
			continue
		}
		for _, candidate := range append([]string{entity.ID, entity.CanonicalName, entity.Code}, entity.Aliases...) {
			if candidate != "" && normalizeEntityName(candidate) == normalizeEntityName(name) {
				if found.ID != "" && found.ID != entity.ID {
					return found, fmt.Errorf("uncertain: %s has multiple candidates", kind)
				}
				found = entity
				break
			}
		}
	}
	if found.ID == "" {
		return found, fmt.Errorf("uncertain: %s identity %q cannot be identified", kind, name)
	}
	for _, op := range b.ops {
		if op.Type == "update_entity" && op.EntityID == found.ID && op.Patch.Attributes != nil {
			found.Attributes = *op.Patch.Attributes
			found.Revision = op.Patch.Revision + 1
		}
	}
	return found, nil
}

func (b *importMapBuilder) parseRecords(workbook *importWorkbookData) error {
	mapped := map[string]bool{}
	for _, mapping := range b.plan.Definition.Sheets {
		mapped[mapping.Worksheet] = true
		if mapping.Ignore {
			b.skippedSheets[mapping.Worksheet] = true
			continue
		}
		b.location = &types.ShelfImportLocation{Worksheet: mapping.Worksheet, Cells: []string{}}
		sheet, ok := workbook.Sheets[mapping.Worksheet]
		if !ok || sheet.ConditionalFormatting {
			b.skippedSheets[mapping.Worksheet] = true
			b.issue("unparseable", "worksheet missing or uses conditional formatting", nil)
			continue
		}
		for _, row := range sheet.Rows {
			if row <= mapping.HeaderRows {
				continue
			}
			cells := map[string]importCell{}
			raw := map[string]string{}
			refs := []string{}
			nonempty := false
			for _, column := range mapping.Columns {
				ref := column.Column + strconv.Itoa(row)
				cell := sheet.cell(ref)
				cells[column.Field+":"+column.Key] = cell
				raw[ref] = cell.Value
				if cell.Formula != nil {
					raw[ref+".formula"] = *cell.Formula
				}
				refs = append(refs, ref)
				nonempty = nonempty || cell.Value != "" || cell.Formula != nil
			}
			if !nonempty {
				continue
			}
			b.location = &types.ShelfImportLocation{Worksheet: mapping.Worksheet, Cells: refs, Values: raw}
			variants := []map[string]importCell{cells}
			if mapping.OwnerKind == "model" && b.plan.FileKind != "atq" {
				names := []string{cells["owner:"].Value}
				for _, separator := range b.plan.Definition.ModelSeparators {
					split := []string{}
					for _, name := range names {
						split = append(split, strings.Split(name, separator)...)
					}
					names = split
				}
				variants = nil
				for _, name := range names {
					copy := map[string]importCell{}
					for key, cell := range cells {
						copy[key] = cell
					}
					owner := copy["owner:"]
					owner.Value = name
					copy["owner:"] = owner
					variants = append(variants, copy)
				}
			}
			for _, variant := range variants {
				start := len(b.ops)
				var err error
				switch b.plan.FileKind {
				case "atq":
					b.present[normalizeEntityName(importRequirementName(variant))] = true
					err = b.requirementRow(mapping, variant)
				case "overseas_sales":
					err = b.overseasRow(mapping, variant)
				default:
					err = b.certificationRow(mapping, variant)
				}
				if err != nil {
					b.ops = b.ops[:start]
					kind := "unparseable"
					if strings.HasPrefix(err.Error(), "uncertain:") {
						kind = "uncertain"
					}
					if b.plan.FileKind == "atq" && strings.TrimSpace(importRequirementName(variant)) == "" {
						b.skippedSheets[mapping.Worksheet] = true
					}
					b.issue(kind, err.Error(), raw)
				}
				if len(b.ops) > 500 {
					return apperrors.NewBadRequestError("import exceeds 500 changeset operations; split the workbook")
				}
			}
			if len(b.ops) > 500 {
				return apperrors.NewBadRequestError("import exceeds 500 changeset operations; split the workbook")
			}
		}
	}
	names := []string{}
	for name := range workbook.Sheets {
		if !mapped[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		b.location = &types.ShelfImportLocation{Worksheet: name, Cells: []string{}}
		b.issue("uncertain", "worksheet has no mapping; explicitly map or ignore it", nil)
	}
	return nil
}

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
	record := types.ShelfCertification{OwnerID: owner.ID, Standard: standard.Key, CertificationObject: cells["certification_object:"].Value, Region: cells["region:"].Value}
	if cell, ok := cells["cell_part:"]; ok && cell.Value != "" {
		part, err := b.recordEntity("part", cell.Value)
		if err != nil {
			return err
		}
		record.CellPartID = part.ID
	}
	key := fmt.Sprintf("cert:%q/%q/%q/%q/%q", record.OwnerID, record.Standard, record.CertificationObject, record.CellPartID, record.Region)
	if b.seenModelIDs[key] {
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
	b.seenModelIDs[key] = true
	op := types.ShelfOperation{Type: "update_certification", Revision: revision, Certification: &types.ShelfCertificationInput{Name: cells["name:"].Value, ShelfCertification: record}}
	if len(matches) > 1 {
		op.Issues = []types.ShelfConflict{{Type: "uncertain", Message: "certification key has multiple candidates; submit a corrected complete record", Details: map[string]any{"candidates": matches, "match_count": len(matches)}}}
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
	model, err := b.recordEntity("model", modelName)
	if err != nil {
		return err
	}
	for _, op := range b.ops {
		if op.Relation != nil && op.Relation.RelationType == "composed_of" && op.Relation.SubjectID == model.ID && op.Relation.ObjectID == part.ID {
			return nil
		}
	}
	var relations []types.Relation
	if err := b.service.db.WithContext(b.ctx).Where("library_id=? AND space_id=? AND subject_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", b.library.ID, b.library.SpaceID, model.ID, part.ID, b.at, b.at).Find(&relations).Error; err != nil {
		return err
	}
	for _, relation := range relations {
		if relation.Attributes["module_type"] != mapping.ModuleType {
			return fmt.Errorf("uncertain: report composition module differs from the shelf")
		}
	}
	if len(relations) == 0 {
		b.append(types.ShelfOperation{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: model.ID, ObjectID: part.ID, RelationType: "composed_of", Attributes: map[string]any{"module_type": mapping.ModuleType}}})
	}
	return nil
}

// Notices are persisted review evidence with no catalog write operation.
func (b *importMapBuilder) overseasRow(mapping types.ShelfImportSheet, cells map[string]importCell) error {
	if err := b.validateRecordCells(cells); err != nil {
		return err
	}
	owner, err := b.recordOwner(mapping, cells["owner:"].Value)
	if err != nil {
		return err
	}
	issues := []types.ShelfConflict{}
	add := func(field string, expected, actual any) {
		issues = append(issues, types.ShelfConflict{Type: "cross_table_contradiction", Message: "overseas list differs from the shelf; review without changing shelf data", Details: map[string]any{"owner_id": owner.ID, "field": field, "expected": expected, "actual": actual}})
	}
	statuses := []types.ShelfSaleStatus{}
	if raw, ok := owner.Attributes["sale_statuses"]; ok {
		if err := types.DecodeShelfValue(raw, &statuses); err != nil {
			return err
		}
	}
	for _, column := range mapping.Columns {
		if column.Field != "sale_status" {
			continue
		}
		raw := cells[column.Field+":"+column.Key].Value
		expected := raw
		if mapped, ok := b.plan.Definition.StatusValues[raw]; ok {
			expected = mapped.Status
		}
		if !slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, expected) {
			return fmt.Errorf("uncertain: overseas sale status is blank or has no mapping")
		}
		actual := []string{}
		for _, status := range statuses {
			if status.Region == column.Key {
				actual = append(actual, status.Status)
			}
		}
		if len(actual) != 1 || actual[0] != expected {
			var value any = actual
			if len(actual) == 1 {
				value = actual[0]
			}
			add("sale_status:"+column.Key, expected, value)
		}
	}
	hasMatrix := slices.ContainsFunc(mapping.Columns, func(c types.ShelfImportColumn) bool { return c.Field == "certification" })
	if hasMatrix {
		detail, err := b.service.shelfModelDetail(b.ctx, &owner, b.at, nil)
		if err != nil {
			return err
		}
		for _, column := range mapping.Columns {
			if column.Field != "certification" {
				continue
			}
			raw := cells["certification:"+column.Key].Value
			expected, ok := mapping.CheckValues[raw]
			if !ok {
				return fmt.Errorf("uncertain: overseas certification flag %q has no confirmed meaning", raw)
			}
			actual := false
			partial := false
			for _, view := range detail.Certifications {
				record := view.Certification
				if record.Standard != column.Key || record.Progress != mapping.Progress {
					continue
				}
				if cell, ok := cells["certification_object:"]; ok && record.CertificationObject != cell.Value {
					continue
				}
				if cell, ok := cells["region:"]; ok && record.Region != cell.Value {
					continue
				}
				actual = true
				partial = partial || shelfConditionsPartial(shelfCertificationConditions(record, detail.Composition), detail.Composition)
			}
			if expected != actual {
				add("certification:"+column.Key, expected, actual)
			} else if expected && partial {
				add("certification_partial_options:"+column.Key, "all combinations", "some combinations")
			}
		}
	}

	if standardCell, ok := cells["standard:"]; ok {
		standard, ok := b.library.ShelfConfig.Standard(standardCell.Value)
		if !ok {
			return fmt.Errorf("uncertain: overseas standard is not configured")
		}
		records := []types.ShelfCertification{}
		for _, entity := range b.entities {
			if entity.Kind != "certification" {
				continue
			}
			var record types.ShelfCertification
			if err := types.DecodeShelfValue(entity.Attributes["certification"], &record); err != nil {
				return err
			}
			if record.OwnerID != owner.ID || record.Standard != standard.Key {
				continue
			}
			if cell, ok := cells["cell_part:"]; ok {
				partID := ""
				if cell.Value != "" {
					part, err := b.recordEntity("part", cell.Value)
					if err != nil {
						return err
					}
					partID = part.ID
				}
				if record.CellPartID != partID {
					continue
				}
			}
			if cell, ok := cells["certification_object:"]; ok && record.CertificationObject != cell.Value {
				continue
			}
			if cell, ok := cells["region:"]; ok && record.Region != cell.Value {
				continue
			}
			records = append(records, record)
		}
		if len(records) != 1 {
			add("certification", standardCell.Value, records)
		} else {
			record := records[0]
			for _, pair := range []struct{ field, actual string }{{"progress", record.Progress}, {"number", record.Number}, {"validity", record.Validity}, {"agency", record.Agency}, {"cloud_url", record.CloudURL}} {
				field, actual := pair.field, pair.actual
				if cell, ok := cells[field+":"]; ok && cell.Value != actual {
					add(field, cell.Value, actual)
				}
			}
		}
	}
	b.append(types.ShelfOperation{Type: "import_notice", Issues: issues})
	return nil
}

func (b *importMapBuilder) validateRecordCells(cells map[string]importCell) error {
	for field, cell := range cells {
		dateField := slices.Contains([]string{"start_date:", "obtained_date:", "issued_date:"}, field)
		if cell.Formula != nil || cell.Type == "e" || cell.FormattedNumber && !(dateField && cell.DateSerial) {
			return fmt.Errorf("formula, error or formatted numeric cells require explicit values")
		}
		if cell.Fill != "" && b.plan.Definition.FillMeanings[cell.Fill] != "ignore" {
			return fmt.Errorf("uncertain: record cell fill requires an explicit interpretation")
		}
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

func (b *importMapBuilder) recordOwner(mapping types.ShelfImportSheet, name string) (types.Entity, error) {
	owner, err := b.recordEntity(mapping.OwnerKind, name)
	if err != nil || mapping.ProductLineID == "" {
		return owner, err
	}
	ids := []string{owner.ID}
	if owner.Kind == "part" {
		ids = nil
		if err := b.service.db.WithContext(b.ctx).Model(&types.Relation{}).Where("library_id=? AND space_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", b.library.ID, b.library.SpaceID, owner.ID, b.at, b.at).Pluck("subject_id", &ids).Error; err != nil {
			return owner, err
		}
		for _, op := range b.ops {
			if op.Relation != nil && op.Relation.RelationType == "composed_of" && op.Relation.ObjectID == owner.ID {
				ids = append(ids, op.Relation.SubjectID)
			}
		}
	}
	for _, id := range ids {
		var count int64
		err := b.service.db.WithContext(b.ctx).Raw(`WITH RECURSIVE ancestors(id) AS (
   SELECT ?::varchar UNION SELECT r.object_id FROM relations r JOIN ancestors a ON r.subject_id=a.id
   WHERE r.library_id=? AND r.space_id=? AND r.relation_type='belongs_to' AND r.valid_from<=? AND (r.valid_to IS NULL OR r.valid_to>?))
   SELECT count(*) FROM ancestors WHERE id=?`, id, b.library.ID, b.library.SpaceID, b.at, b.at, mapping.ProductLineID).Scan(&count).Error
		if err != nil {
			return owner, err
		}
		if count > 0 {
			return owner, nil
		}
	}
	return owner, fmt.Errorf("uncertain: owner does not match the worksheet product line at the file date")
}
