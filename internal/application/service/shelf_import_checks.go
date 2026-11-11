package service

import (
	"fmt"
	"slices"

	"reliquary/internal/types"
)

// Notices are persisted review evidence with no catalog write operation.
func (b *importMapBuilder) overseasRow(mapping types.ShelfImportSheet, cells map[string]importCell) error {
	if err := b.validateRecordCells(cells); err != nil {
		return err
	}
	region, err := b.recordRegion(cells["region:"].Value)
	if err != nil {
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
			matches := [][]types.ShelfCondition{}
			for _, view := range detail.Certifications {
				record := view.Certification
				if record.Standard != column.Key || record.Progress != mapping.Progress {
					continue
				}
				if cell, ok := cells["certification_object:"]; ok && record.CertificationObject != cell.Value {
					continue
				}
				if _, ok := cells["region:"]; ok && record.Region != region {
					continue
				}
				actual = true
				matches = append(matches, shelfCertificationConditions(record, detail.Composition))
			}
			if expected != actual {
				add("certification:"+column.Key, expected, actual)
			} else if expected && shelfMatchesPartial(matches, detail.Composition) {
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
			if _, ok := cells["region:"]; ok && record.Region != region {
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
