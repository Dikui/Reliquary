package service

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"reliquary/internal/types"
)

// The layout layer turns a configured worksheet region into a virtual sheet
// whose records are rows, so every file-kind parser stays row oriented. Plan
// columns and header_rows always refer to that virtual sheet; locations are
// translated back to source cells before the changeset is saved.

type importArea struct{ Left, Top, Right, Bottom int }

func parseImportRange(value string) (importArea, error) {
	ends := strings.Split(value, ":")
	if len(ends) != 2 {
		return importArea{}, fmt.Errorf("range must look like A1:F200")
	}
	left, top, e1 := importCellCoordinates(ends[0])
	right, bottom, e2 := importCellCoordinates(ends[1])
	if e1 != nil || e2 != nil || left > right || top > bottom {
		return importArea{}, fmt.Errorf("range must look like A1:F200")
	}
	return importArea{left, top, right, bottom}, nil
}

func importColumnLetters(number int) string {
	letters := ""
	for number > 0 {
		number--
		letters = string(rune('A'+number%26)) + letters
		number /= 26
	}
	return letters
}

func importRef(col, row int) string { return importColumnLetters(col) + strconv.Itoa(row) }

// sheetArea is the configured range, or every stored cell when none is given.
func importSheetArea(mapping types.ShelfImportSheet, sheet *importWorksheet) importArea {
	if mapping.Range != "" {
		area, _ := parseImportRange(mapping.Range)
		return area
	}
	area := importArea{1, 1, 1, 1}
	for ref := range sheet.Cells {
		col, row, _ := importCellCoordinates(ref)
		area.Right, area.Bottom = max(area.Right, col), max(area.Bottom, row)
	}
	for _, m := range sheet.Merges {
		area.Right, area.Bottom = max(area.Right, m.Right), max(area.Bottom, m.Bottom)
	}
	return area
}

// virtualPosition maps a source cell into the virtual sheet; ok is false
// outside the configured area.
func importVirtualPosition(mapping types.ShelfImportSheet, area importArea, col, row int) (int, int, bool) {
	if col < area.Left || col > area.Right || row < area.Top || row > area.Bottom {
		return 0, 0, false
	}
	if mapping.Transpose {
		return row - area.Top + 1, col - area.Left + 1, true
	}
	return col - area.Left + 1, row - area.Top + 1, true
}

func normalizeImportWorkbook(definition types.ShelfImportDefinition, workbook *importWorkbookData) *importWorkbookData {
	result := &importWorkbookData{Sheets: map[string]*importWorksheet{}}
	for name, sheet := range workbook.Sheets {
		result.Sheets[name] = sheet
	}
	for _, mapping := range definition.Sheets {
		sheet := workbook.Sheets[mapping.Worksheet]
		if mapping.Ignore || sheet == nil || mapping.Range == "" && !mapping.Transpose {
			continue
		}
		area := importSheetArea(mapping, sheet)
		virtual := &importWorksheet{Cells: map[string]importCell{}, ConditionalFormatting: sheet.ConditionalFormatting, Origin: map[string]string{}}
		rows := map[int]bool{}
		for ref, cell := range sheet.Cells {
			col, row, _ := importCellCoordinates(ref)
			vc, vr, ok := importVirtualPosition(mapping, area, col, row)
			if !ok {
				continue
			}
			key := importRef(vc, vr)
			virtual.Cells[key] = cell
			virtual.Origin[key] = ref
			rows[vr] = true
		}
		for _, m := range sheet.Merges {
			clipped := importArea{max(m.Left, area.Left), max(m.Top, area.Top), min(m.Right, area.Right), min(m.Bottom, area.Bottom)}
			if clipped.Left > clipped.Right || clipped.Top > clipped.Bottom {
				continue
			}
			l, t, _ := importVirtualPosition(mapping, area, clipped.Left, clipped.Top)
			r, b, _ := importVirtualPosition(mapping, area, clipped.Right, clipped.Bottom)
			anchor := importRef(l, t)
			if col, row, _ := importCellCoordinates(m.Anchor); col >= area.Left && row >= area.Top {
				ac, ar, _ := importVirtualPosition(mapping, area, col, row)
				anchor = importRef(ac, ar)
			}
			virtual.Merges = append(virtual.Merges, importMerge{l, t, r, b, anchor})
		}
		for row := range rows {
			virtual.Rows = append(virtual.Rows, row)
		}
		slices.Sort(virtual.Rows)
		result.Sheets[mapping.Worksheet] = virtual
	}
	return result
}

// translateImportLocations rewrites virtual references to source cells. Maps
// are replaced rather than edited because a row's value map is shared between
// its location and issue details.
func translateImportLocations(ops []types.ShelfOperation, layout *importWorkbookData) {
	source := func(worksheet, ref string) string {
		sheet := layout.Sheets[worksheet]
		if sheet == nil || sheet.Origin == nil {
			return ref
		}
		base, suffix, _ := strings.Cut(ref, ".")
		if origin, ok := sheet.Origin[base]; ok {
			if suffix != "" {
				return origin + "." + suffix
			}
			return origin
		}
		return ref
	}
	values := func(worksheet string, raw map[string]string) map[string]string {
		translated := make(map[string]string, len(raw))
		for key, value := range raw {
			translated[source(worksheet, key)] = value
		}
		return translated
	}
	done := map[*types.ShelfImportLocation]bool{}
	for i := range ops {
		location := ops[i].ImportLocation
		if location == nil || layout.Sheets[location.Worksheet] == nil || layout.Sheets[location.Worksheet].Origin == nil {
			continue
		}
		if !done[location] {
			done[location] = true
			cells := make([]string, len(location.Cells))
			for j, ref := range location.Cells {
				cells[j] = source(location.Worksheet, ref)
			}
			location.Cells = cells
			if location.Values != nil {
				location.Values = values(location.Worksheet, location.Values)
			}
		}
		for j := range ops[i].Issues {
			switch details := ops[i].Issues[j].Details.(type) {
			case map[string]string:
				ops[i].Issues[j].Details = values(location.Worksheet, details)
			case map[string]any:
				copied := map[string]any{}
				for key, value := range details {
					copied[key] = value
				}
				if raw, ok := details["values"].(map[string]string); ok {
					copied["values"] = values(location.Worksheet, raw)
				}
				ops[i].Issues[j].Details = copied
			}
		}
	}
}
