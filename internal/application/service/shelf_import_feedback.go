package service

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"reliquary/internal/types"
)

// Retain mapped header text in batch metadata because the workbook is not kept.
func shelfImportHeaders(definition types.ShelfImportDefinition, workbook *importWorkbookData) map[string]map[string]string {
	result := map[string]map[string]string{}
	for _, mapping := range definition.Sheets {
		sheet := workbook.Sheets[mapping.Worksheet]
		if mapping.Ignore || sheet == nil {
			continue
		}
		result[mapping.Worksheet] = map[string]string{}
		for _, column := range mapping.Columns {
			headers, seen := []string{}, map[string]bool{}
			for row := 1; row <= mapping.HeaderRows; row++ {
				cell := sheet.cell(column.Column + strconv.Itoa(row))
				if cell.Value != "" && !seen[cell.Ref] {
					headers = append(headers, cell.Value)
					seen[cell.Ref] = true
				}
			}
			result[mapping.Worksheet][column.Column] = strings.Join(headers, "\n")
		}
	}
	return result
}

// Scan physical cells so a merged value is reported once at its source column,
// even when mapped rows cannot be parsed. Never evaluate source formulas.
func (b *importMapBuilder) unmappedColumns(workbook *importWorkbookData) {
	for _, mapping := range b.plan.Definition.Sheets {
		sheet := workbook.Sheets[mapping.Worksheet]
		if mapping.Ignore || sheet == nil {
			continue
		}
		mapped := map[string]bool{}
		for _, column := range mapping.Columns {
			mapped[column.Column] = true
		}
		byColumn := map[string][]importCell{}
		virtualRow := map[string]int{}
		for ref, cell := range sheet.Cells {
			_, row, _ := importCellCoordinates(ref)
			column := strings.TrimRight(ref, "0123456789")
			if row <= mapping.HeaderRows || mapped[column] || cell.Value == "" && cell.Formula == nil {
				continue
			}
			byColumn[column] = append(byColumn[column], cell)
			virtualRow[cell.Ref] = row
		}
		columns := make([]string, 0, len(byColumn))
		for column := range byColumn {
			columns = append(columns, column)
		}
		slices.SortFunc(columns, func(a, c string) int { return cmp.Compare(importColumnNumber(a), importColumnNumber(c)) })
		for _, column := range columns {
			cells := byColumn[column]
			slices.SortFunc(cells, func(a, c importCell) int {
				return cmp.Compare(virtualRow[a.Ref], virtualRow[c.Ref])
			})
			headers, seen := []string{}, map[string]bool{}
			for row := 1; row <= mapping.HeaderRows; row++ {
				cell := sheet.cell(column + strconv.Itoa(row))
				if cell.Value != "" && !seen[cell.Ref] {
					headers = append(headers, cell.Value)
					seen[cell.Ref] = true
				}
			}
			feedback := types.ShelfImportColumnFeedback{Worksheet: mapping.Worksheet, Column: column, Header: strings.Join(headers, "\n"), NonemptyRows: len(cells), Examples: []string{}}
			b.location = &types.ShelfImportLocation{Worksheet: mapping.Worksheet, Cells: []string{}, Values: map[string]string{}}
			for _, cell := range cells[:min(3, len(cells))] {
				value := cell.Value
				if cell.Formula != nil {
					value = "=" + *cell.Formula
				}
				feedback.Examples = append(feedback.Examples, value)
				b.location.Cells = append(b.location.Cells, cell.Ref)
				b.location.Values[cell.Ref] = value
			}
			b.append(types.ShelfOperation{Type: "import_notice", Issues: []types.ShelfConflict{{Type: "unmapped_column", Details: feedback}}})
		}
	}
}
