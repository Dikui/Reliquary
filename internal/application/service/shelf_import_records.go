package service

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// Resolve only exact names, codes and confirmed aliases within the chosen kind.
func (b *importMapBuilder) recordEntity(kind, name string) (types.Entity, error) {
	var found types.Entity
	for _, op := range b.ops {
		if op.Type == "create_entity" && op.Entity != nil && op.Entity.Kind == kind && normalizeEntityName(op.Entity.Name) == normalizeEntityName(name) {
			if kind == "document_requirement" && requirementListName(op.Entity.Attributes) != b.requirementListName(b.location.Worksheet) {
				continue
			}
			found = types.Entity{ID: "$" + op.ID, Kind: kind, CanonicalName: op.Entity.Name, Attributes: op.Entity.Attributes, Revision: 1}
		}
	}
	if strings.TrimSpace(name) == "" || slices.Contains(b.plan.Definition.MissingValues, name) {
		return found, fmt.Errorf("uncertain: missing %s identity", kind)
	}
	for _, entity := range b.entities {
		if entity.Kind != kind || kind == "document_requirement" && requirementListName(entity.Attributes) != b.requirementListName(b.location.Worksheet) {
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
		if !ok {
			b.skippedSheets[mapping.Worksheet] = true
			b.issue("unparseable", "configured worksheet is missing", nil)
			continue
		}
		if sheet.ConditionalFormatting {
			b.skippedSheets[mapping.Worksheet] = true
			b.issue("uncertain", "conditional formatting requires explicit values and fills", nil)
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
				names := b.importModelNames(cells["owner:"].Value)
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
					b.present[b.requirementPresenceKey(mapping.Worksheet, importRequirementName(variant))] = true
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
					if b.plan.FileKind == "atq" {
						// A rejected row may be an unresolved old requirement or
						// a group heading. Neither proves absence on this sheet.
						b.skippedSheets[mapping.Worksheet] = true
					}
					b.issue(kind, err.Error(), raw)
				}
				if len(b.ops) > 500 {
					return apperrors.NewBadRequestError("import exceeds 500 changeset operations; split the workbook")
				}
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

func (b *importMapBuilder) importModelNames(raw string) []string {
	names := []string{raw}
	for _, separator := range b.plan.Definition.ModelSeparators {
		split := []string{}
		for _, name := range names {
			split = append(split, strings.Split(name, separator)...)
		}
		names = split
	}
	return names
}
