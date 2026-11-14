package service

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"reliquary/internal/types"
)

// Coverage ledger: every source cell with content or a marking must be read by
// a mapping, listed as an unmapped column, or reported here. Reported content
// blocks "apply"; the submitter discards it (left out on purpose) or defers it
// (a question for the customer). Signals are reported, never interpreted.

var importCoverageReasons = []string{"ignored_sheet", "hidden_sheet", "drawing", "outside_range", "header_area", "format_only", "strike", "font_color", "comment", "hyperlink", "hidden"}

const importCoverageCellLimit = 200

type importCoverageHit struct{ ref, example string }

func importCellText(cell importCell) string {
	if cell.Formula != nil {
		return "=" + *cell.Formula
	}
	return cell.Value
}

func importHasContent(cell importCell) bool { return cell.Value != "" || cell.Formula != nil }

func (b *importMapBuilder) coverage(workbook, layout *importWorkbookData) {
	for _, mapping := range b.plan.Definition.Sheets {
		sheet := workbook.Sheets[mapping.Worksheet]
		if sheet == nil {
			continue
		}
		groups := map[string][]importCoverageHit{}
		add := func(reason, ref, example string) {
			groups[reason] = append(groups[reason], importCoverageHit{ref, example})
		}
		refs := importSheetRefs(sheet)
		if mapping.Ignore {
			for _, ref := range refs {
				cell := sheet.Cells[ref]
				switch {
				case importHasContent(cell):
					add("ignored_sheet", ref, importCellText(cell))
				case sheet.Comments[ref] != "":
					add("ignored_sheet", ref, sheet.Comments[ref])
				case sheet.Hyperlinks[ref] != "":
					add("ignored_sheet", ref, sheet.Hyperlinks[ref])
				}
			}
			b.appendCoverage(mapping.Worksheet, groups)
			continue
		}
		if sheet.ConditionalFormatting {
			continue // the whole sheet is already a blocking conflict
		}
		if sheet.Hidden {
			add("hidden_sheet", "", "")
		}
		if sheet.Drawing {
			add("drawing", "", "")
		}
		virtual := layout.Sheets[mapping.Worksheet]
		area := importSheetArea(mapping, sheet)
		mapped := map[string]bool{}
		for _, column := range mapping.Columns {
			mapped[column.Column] = true
		}
		// Unmapped columns with data are already listed with their header.
		dataColumns, processed := map[string]bool{}, map[int]bool{}
		for ref, cell := range virtual.Cells {
			_, row, _ := importCellCoordinates(ref)
			column := strings.TrimRight(ref, "0123456789")
			if row > mapping.HeaderRows && !mapped[column] && importHasContent(cell) {
				dataColumns[column] = true
			}
		}
		for _, row := range virtual.Rows {
			for _, column := range mapping.Columns {
				if row > mapping.HeaderRows && importHasContent(virtual.cell(column.Column+strconv.Itoa(row))) {
					processed[row] = true
				}
			}
		}
		for _, ref := range refs {
			cell := sheet.Cells[ref]
			col, row, _ := importCellCoordinates(ref)
			content := importHasContent(cell)
			vc, vr, inside := importVirtualPosition(mapping, area, col, row)
			if !inside {
				if content || cell.Strike {
					add("outside_range", ref, importCellText(cell))
				}
			} else {
				column, header := importColumnLetters(vc), vr <= mapping.HeaderRows
				ignoredFill := cell.Fill == "" || b.plan.Definition.FillMeanings[cell.Fill] == "ignore"
				switch {
				case content && header && !mapped[column] && !dataColumns[column]:
					add("header_area", ref, importCellText(cell))
				case !content && !header && !ignoredFill && !(mapped[column] && processed[vr]):
					add("format_only", ref, cell.Fill)
				}
				if content && cell.Strike {
					add("strike", ref, importCellText(cell))
				}
				if content && !header && cell.FontColor != "" {
					add("font_color", ref, cell.FontColor+" "+importCellText(cell))
				}
				if content && sheet.hiddenAt(col, row) {
					add("hidden", ref, importCellText(cell))
				}
			}
			if text := sheet.Comments[ref]; text != "" {
				add("comment", ref, text)
			}
			if target := sheet.Hyperlinks[ref]; target != "" {
				add("hyperlink", ref, target)
			}
		}
		b.appendCoverage(mapping.Worksheet, groups)
	}
}

// importSheetRefs lists stored cells plus cells known only by a comment or link.
func importSheetRefs(sheet *importWorksheet) []string {
	seen := map[string]bool{}
	refs := []string{}
	for _, set := range []map[string]string{sheet.Comments, sheet.Hyperlinks} {
		for ref := range set {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	for ref := range sheet.Cells {
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	slices.SortFunc(refs, func(a, c string) int {
		ac, ar, _ := importCellCoordinates(a)
		cc, cr, _ := importCellCoordinates(c)
		return cmp.Or(cmp.Compare(ar, cr), cmp.Compare(ac, cc))
	})
	return refs
}

func (b *importMapBuilder) appendCoverage(worksheet string, groups map[string][]importCoverageHit) {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, c string) int {
		return cmp.Compare(slices.Index(importCoverageReasons, a), slices.Index(importCoverageReasons, c))
	})
	for _, reason := range keys {
		hits := groups[reason]
		feedback := types.ShelfImportCoverageFeedback{Worksheet: worksheet, Reason: reason, Count: len(hits), Examples: []string{}}
		location := &types.ShelfImportLocation{Worksheet: worksheet, Cells: []string{}, Values: map[string]string{}}
		for i, hit := range hits {
			if hit.ref == "" {
				continue
			}
			if i < importCoverageCellLimit {
				location.Cells = append(location.Cells, hit.ref)
			}
			if len(feedback.Examples) < 3 {
				feedback.Examples = append(feedback.Examples, hit.example)
				location.Values[hit.ref] = hit.example
			}
		}
		if reason == "hidden_sheet" || reason == "drawing" {
			feedback.Count = 1
		}
		b.location = location
		b.append(types.ShelfOperation{Type: "import_notice", Issues: []types.ShelfConflict{{Type: "uncovered_content", Message: "source content is not covered by the import plan; discard to leave it out or defer it as a customer question", Details: feedback}}})
	}
}
