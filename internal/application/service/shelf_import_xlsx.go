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
	Strike          bool
	FontColor       string // set only when it differs from the workbook default font
	FormattedNumber bool
	DateSerial      bool
	Date1904        bool
}
type importWorksheet struct {
	Cells                 map[string]importCell
	Rows                  []int
	Merges                []importMerge
	ConditionalFormatting bool
	// The fields below are inventory only. Parsers never read them; the
	// coverage ledger reports them so no source signal disappears silently.
	Hidden     bool
	HiddenRows map[int]bool
	HiddenCols [][2]int
	Comments   map[string]string
	Hyperlinks map[string]string
	Drawing    bool
	// Origin maps a virtual reference to its source cell after range or
	// transpose normalization; nil for an unchanged worksheet.
	Origin map[string]string
}

func (sheet *importWorksheet) hiddenAt(col, row int) bool {
	if sheet.HiddenRows[row] {
		return true
	}
	for _, span := range sheet.HiddenCols {
		if col >= span[0] && col <= span[1] {
			return true
		}
	}
	return false
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
		Properties struct {
			Date1904 bool `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name  string `xml:"name,attr"`
			ID    string `xml:"id,attr"`
			State string `xml:"state,attr"`
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
		Fonts []struct {
			Strike *struct {
				Val string `xml:"val,attr"`
			} `xml:"strike"`
			Color *importColor `xml:"color"`
		} `xml:"fonts>font"`
		Formats []struct {
			Fill         int `xml:"fillId,attr"`
			Font         int `xml:"fontId,attr"`
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
	strikes := make([]bool, len(styles.Formats))
	fontColors := make([]string, len(styles.Formats))
	defaultFont := ""
	if len(styles.Fonts) > 0 {
		defaultFont = styles.Fonts[0].Color.key()
	}
	for i, format := range styles.Formats {
		if format.Font > 0 && format.Font < len(styles.Fonts) {
			font := styles.Fonts[format.Font]
			strikes[i] = font.Strike != nil && font.Strike.Val != "0" && font.Strike.Val != "false"
			if color := font.Color.key(); color != defaultFont {
				fontColors[i] = color
			}
		}
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
				Hidden bool         `xml:"hidden,attr"`
				Cells  []importCell `xml:"c"`
			} `xml:"sheetData>row"`
			Columns []struct {
				Min    int  `xml:"min,attr"`
				Max    int  `xml:"max,attr"`
				Style  int  `xml:"style,attr"`
				Hidden bool `xml:"hidden,attr"`
			} `xml:"cols>col"`
			Hyperlinks []struct {
				Ref      string `xml:"ref,attr"`
				ID       string `xml:"id,attr"`
				Location string `xml:"location,attr"`
			} `xml:"hyperlinks>hyperlink"`
			Drawings           []struct{} `xml:"drawing"`
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
		sheet := &importWorksheet{Cells: map[string]importCell{}, ConditionalFormatting: len(raw.ConditionalFormats) > 0, Hidden: item.State == "hidden" || item.State == "veryHidden", HiddenRows: map[int]bool{}, Comments: map[string]string{}, Hyperlinks: map[string]string{}, Drawing: len(raw.Drawings) > 0}
		for _, c := range raw.Columns {
			if c.Hidden {
				sheet.HiddenCols = append(sheet.HiddenCols, [2]int{c.Min, c.Max})
			}
		}
		sheetRels, err := readImportSheetRels(files, name, decode)
		if err != nil {
			return nil, err
		}
		for _, link := range raw.Hyperlinks {
			first := strings.Split(link.Ref, ":")[0]
			if _, _, err := importCellCoordinates(first); err != nil {
				return nil, fmt.Errorf("invalid hyperlink reference")
			}
			target := sheetRels.target(link.ID)
			if link.Location != "" {
				target = strings.TrimPrefix(target+"#"+link.Location, "#")
			}
			sheet.Hyperlinks[first] = target
		}
		if comments := sheetRels.comments; comments != "" {
			var list struct {
				Items []struct {
					Ref  string         `xml:"ref,attr"`
					Text importRichText `xml:"text"`
				} `xml:"commentList>comment"`
			}
			if err := decode(comments, &list, true); err != nil {
				return nil, err
			}
			for _, comment := range list.Items {
				if _, _, err := importCellCoordinates(comment.Ref); err != nil {
					return nil, fmt.Errorf("invalid comment reference")
				}
				sheet.Comments[comment.Ref] = comment.Text.text()
			}
		}
		lastRow := 0
		for _, row := range raw.Rows {
			if row.Number <= lastRow || row.Number > 1048576 {
				return nil, fmt.Errorf("worksheet rows must have increasing explicit numbers")
			}
			lastRow = row.Number
			sheet.Rows = append(sheet.Rows, row.Number)
			if row.Hidden {
				sheet.HiddenRows[row.Number] = true
			}
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
					cell.Strike = strikes[style]
					cell.FontColor = fontColors[style]
					cell.FormattedNumber = styles.Formats[style].NumberFormat != 0 && (cell.Type == "" || cell.Type == "n") && cell.Value != ""
					format := styles.Formats[style].NumberFormat
					cell.DateSerial = cell.FormattedNumber && (format >= 14 && format <= 17 || format == 22)
					cell.Date1904 = workbook.Properties.Date1904
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
			if len(ends) == 1 {
				// Excel writes and opens single-cell merges such as ref="A17".
				ends = append(ends, ends[0])
			}
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

type importColor struct {
	RGB     string `xml:"rgb,attr"`
	Indexed *int   `xml:"indexed,attr"`
	Theme   *int   `xml:"theme,attr"`
	Tint    string `xml:"tint,attr"`
	Auto    bool   `xml:"auto,attr"`
}

// key uses the same raw identity as fills; automatic or absent color is "".
func (c *importColor) key() string {
	switch {
	case c == nil || c.Auto:
		return ""
	case c.Theme != nil:
		return fmt.Sprintf("theme:%d:%s", *c.Theme, c.Tint)
	case c.Indexed != nil:
		return fmt.Sprintf("indexed:%d", *c.Indexed)
	}
	return strings.ToUpper(c.RGB)
}

type importSheetRels struct {
	targets  map[string]string
	comments string
}

func (r importSheetRels) target(id string) string { return r.targets[id] }

// Worksheet relationships name hyperlink targets and the legacy comment part.
// Modern threaded comments also write that part, so it covers both.
func readImportSheetRels(files map[string]*zip.File, sheetPath string, decode func(string, any, bool) error) (importSheetRels, error) {
	result := importSheetRels{targets: map[string]string{}}
	relsPath := path.Join(path.Dir(sheetPath), "_rels", path.Base(sheetPath)+".rels")
	if _, ok := files[relsPath]; !ok {
		return result, nil
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := decode(relsPath, &rels, false); err != nil {
		return result, err
	}
	for _, rel := range rels.Items {
		result.targets[rel.ID] = rel.Target
		if strings.HasSuffix(rel.Type, "/comments") && rel.Mode != "External" {
			name := path.Clean(path.Join(path.Dir(sheetPath), rel.Target))
			if strings.HasPrefix(rel.Target, "/") {
				name = path.Clean(strings.TrimPrefix(rel.Target, "/"))
			}
			if !strings.HasPrefix(name, "xl/") {
				return result, fmt.Errorf("invalid comments path")
			}
			result.comments = name
		}
	}
	return result, nil
}
