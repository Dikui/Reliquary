package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// This reader supports data-only .xlsx imports. It never evaluates formulas or
// follows external relationships. Unsupported cell meaning is returned to review.
const shelfImportMaxBytes = 32 << 20

type importRichText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (r importRichText) text() string {
	text := r.Text
	for _, run := range r.Runs {
		text += run.Text
	}
	return text
}

type importCell struct {
	Ref             string         `xml:"r,attr"`
	Type            string         `xml:"t,attr"`
	Style           *int           `xml:"s,attr"`
	Value           string         `xml:"v"`
	Inline          importRichText `xml:"is"`
	Formula         *string        `xml:"f"`
	Fill            string
	FormattedNumber bool
}
type importWorksheet struct {
	Cells                 map[string]importCell
	Rows                  []int
	Merges                []importMerge
	ConditionalFormatting bool
}
type importMerge struct {
	Left, Top, Right, Bottom int
	Anchor                   string
}
type importWorkbookData struct{ Sheets map[string]*importWorksheet }

func importCellCoordinates(ref string) (int, int, error) {
	index := 0
	for index < len(ref) && ref[index] >= 'A' && ref[index] <= 'Z' {
		index++
	}
	if index == 0 || index > 3 {
		return 0, 0, fmt.Errorf("invalid cell reference %q", ref)
	}
	row, err := strconv.Atoi(ref[index:])
	col := importColumnNumber(ref[:index])
	if err != nil || row < 1 || row > 1048576 || col > 16384 {
		return 0, 0, fmt.Errorf("invalid cell reference %q", ref)
	}
	return col, row, nil
}

func (sheet *importWorksheet) cell(ref string) importCell {
	col, row, _ := importCellCoordinates(ref)
	for _, m := range sheet.Merges {
		if col >= m.Left && col <= m.Right && row >= m.Top && row <= m.Bottom {
			return sheet.Cells[m.Anchor]
		}
	}
	return sheet.Cells[ref]
}

func readImportWorkbook(data []byte) (*importWorkbookData, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid xlsx archive")
	}
	files := map[string]*zip.File{}
	var total uint64
	if len(z.File) > 2000 {
		return nil, fmt.Errorf("xlsx has too many archive entries")
	}
	for _, f := range z.File {
		total += f.UncompressedSize64
		if f.UncompressedSize64 > shelfImportMaxBytes || total > shelfImportMaxBytes {
			return nil, fmt.Errorf("xlsx expanded size exceeds 32 MiB")
		}
		if _, exists := files[f.Name]; exists {
			return nil, fmt.Errorf("xlsx contains duplicate archive entries")
		}
		files[f.Name] = f
	}
	decode := func(name string, target any, optional bool) error {
		f, exists := files[name]
		if !exists {
			if optional {
				return nil
			}
			return fmt.Errorf("xlsx missing %s", name)
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		return xml.NewDecoder(io.LimitReader(r, shelfImportMaxBytes+1)).Decode(target)
	}
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	var shared struct {
		Items []importRichText `xml:"si"`
	}
	var styles struct {
		Fills []struct {
			Gradient *struct{} `xml:"gradientFill"`
			Pattern  struct {
				Type  string `xml:"patternType,attr"`
				Color struct {
					RGB     string `xml:"rgb,attr"`
					Indexed *int   `xml:"indexed,attr"`
					Theme   *int   `xml:"theme,attr"`
					Tint    string `xml:"tint,attr"`
				} `xml:"fgColor"`
			} `xml:"patternFill"`
		} `xml:"fills>fill"`
		Formats []struct {
			Fill         int `xml:"fillId,attr"`
			NumberFormat int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	for _, call := range []struct {
		name     string
		target   any
		optional bool
	}{{"xl/workbook.xml", &workbook, false}, {"xl/_rels/workbook.xml.rels", &rels, false}, {"xl/sharedStrings.xml", &shared, true}, {"xl/styles.xml", &styles, true}} {
		if err := decode(call.name, call.target, call.optional); err != nil {
			return nil, err
		}
	}
	colors := make([]string, len(styles.Formats))
	for i, format := range styles.Formats {
		if format.Fill < 0 || format.Fill >= len(styles.Fills) {
			return nil, fmt.Errorf("invalid xlsx fill index")
		}
		if styles.Fills[format.Fill].Gradient != nil {
			colors[i] = "unsupported-fill"
			continue
		}
		p := styles.Fills[format.Fill].Pattern
		if p.Type == "" || p.Type == "none" || p.Type == "gray125" {
			continue
		}
		color := strings.ToUpper(p.Color.RGB)
		if p.Color.Indexed != nil {
			color = fmt.Sprintf("indexed:%d", *p.Color.Indexed)
		}
		if p.Color.Theme != nil {
			color = fmt.Sprintf("theme:%d:%s", *p.Color.Theme, p.Color.Tint)
		}
		if color == "" || p.Type != "solid" {
			color = "unsupported-fill"
		}
		colors[i] = color
	}
	result := &importWorkbookData{Sheets: map[string]*importWorksheet{}}
	for _, item := range workbook.Sheets {
		if _, exists := result.Sheets[item.Name]; exists {
			return nil, fmt.Errorf("duplicate worksheet name")
		}
		target := ""
		for _, rel := range rels.Items {
			if rel.ID == item.ID && rel.Mode != "External" {
				target = rel.Target
			}
		}
		if target == "" {
			return nil, fmt.Errorf("worksheet relationship is missing or external")
		}
		name := path.Clean(path.Join("xl", target))
		if strings.HasPrefix(target, "/") {
			name = path.Clean(strings.TrimPrefix(target, "/"))
		}
		if !strings.HasPrefix(name, "xl/") {
			return nil, fmt.Errorf("invalid worksheet path")
		}
		var raw struct {
			Rows []struct {
				Number int          `xml:"r,attr"`
				Style  *int         `xml:"s,attr"`
				Cells  []importCell `xml:"c"`
			} `xml:"sheetData>row"`
			Columns []struct {
				Min   int `xml:"min,attr"`
				Max   int `xml:"max,attr"`
				Style int `xml:"style,attr"`
			} `xml:"cols>col"`
			ConditionalFormats []struct{} `xml:"conditionalFormatting"`
			Merges             []struct {
				Ref string `xml:"ref,attr"`
			} `xml:"mergeCells>mergeCell"`
		}
		if err := decode(name, &raw, false); err != nil {
			return nil, err
		}
		if len(raw.Rows) > 10000 || len(raw.Merges) > 10000 {
			return nil, fmt.Errorf("worksheet exceeds import row or merge limit")
		}
		sheet := &importWorksheet{Cells: map[string]importCell{}, ConditionalFormatting: len(raw.ConditionalFormats) > 0}
		lastRow := 0
		for _, row := range raw.Rows {
			if row.Number <= lastRow || row.Number > 1048576 {
				return nil, fmt.Errorf("worksheet rows must have increasing explicit numbers")
			}
			lastRow = row.Number
			sheet.Rows = append(sheet.Rows, row.Number)
			for _, cell := range row.Cells {
				col, r, err := importCellCoordinates(cell.Ref)
				if err != nil || r != row.Number {
					return nil, fmt.Errorf("invalid worksheet cell position")
				}
				if _, exists := sheet.Cells[cell.Ref]; exists {
					return nil, fmt.Errorf("duplicate worksheet cell")
				}
				style := 0
				for _, c := range raw.Columns {
					if col >= c.Min && col <= c.Max {
						style = c.Style
					}
				}
				if row.Style != nil {
					style = *row.Style
				}
				if cell.Style != nil {
					style = *cell.Style
				}

				if style < 0 || (style >= len(colors) && style != 0) {
					return nil, fmt.Errorf("invalid cell style")
				}
				if style < len(colors) {
					cell.Fill = colors[style]
					cell.FormattedNumber = styles.Formats[style].NumberFormat != 0 && (cell.Type == "" || cell.Type == "n") && cell.Value != ""
				}
				switch cell.Type {
				case "inlineStr":
					cell.Value = cell.Inline.text()
				case "s":
					index, err := strconv.Atoi(cell.Value)
					if err != nil || index < 0 || index >= len(shared.Items) {
						return nil, fmt.Errorf("invalid shared string")
					}
					cell.Value = shared.Items[index].text()
				}
				sheet.Cells[cell.Ref] = cell
				if len(sheet.Cells) > 100000 {
					return nil, fmt.Errorf("worksheet exceeds cell limit")
				}
			}
		}
		for _, merge := range raw.Merges {
			ends := strings.Split(merge.Ref, ":")
			if len(ends) != 2 {
				return nil, fmt.Errorf("invalid merged range")
			}
			left, top, e1 := importCellCoordinates(ends[0])
			right, bottom, e2 := importCellCoordinates(ends[1])
			if e1 != nil || e2 != nil || left > right || top > bottom {
				return nil, fmt.Errorf("invalid merged range")
			}
			m := importMerge{left, top, right, bottom, ends[0]}
			for _, old := range sheet.Merges {
				if left <= old.Right && right >= old.Left && top <= old.Bottom && bottom >= old.Top {
					return nil, fmt.Errorf("overlapping merged ranges")
				}
			}
			sheet.Merges = append(sheet.Merges, m)
		}
		result.Sheets[item.Name] = sheet
	}
	return result, nil
}
