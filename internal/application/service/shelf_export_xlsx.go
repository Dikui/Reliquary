package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	apperrors "reliquary/internal/errors"
)

type shelfExportCell struct {
	Text       string
	PlannedEOM bool
}

type shelfExportSheet struct {
	Name string
	Rows [][]shelfExportCell
}

func shelfExportXML(text string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(text))
	return out.String()
}

func shelfExportColumn(col int) string {
	name := ""
	for col > 0 {
		col--
		name = string(rune('A'+col%26)) + name
		col /= 26
	}
	return name
}

func shelfExportName(name string, used map[string]bool) string {
	base := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	base = strings.Trim(strings.TrimSpace(base), "'")
	if base == "" {
		base = "Sheet"
	}
	if strings.EqualFold(base, "History") {
		base += "_"
	}
	for i := 0; ; i++ {
		suffix := ""
		if i > 0 {
			suffix = fmt.Sprintf(" (%d)", i+1)
		}
		name := strings.TrimRight(shelfExportTruncate(base, 31-len(suffix)), "'") + suffix
		if !used[strings.ToLower(name)] {
			used[strings.ToLower(name)] = true
			return name
		}
	}
}

func shelfExportTruncate(text string, limit int) string {
	units := 0
	for i, r := range text {
		units += utf16.RuneLen(r)
		if units > limit {
			return text[:i]
		}
	}
	return text
}

// Data-only Office Open XML: every customer value is an explicit inline string.
func writeShelfWorkbook(sheets []shelfExportSheet) ([]byte, error) {
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	write := func(name, content string) error {
		file, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = file.Write([]byte(xml.Header + content))
		return err
	}
	const mainNS = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	const relationNS = `http://schemas.openxmlformats.org/officeDocument/2006/relationships`
	contentTypes := `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	workbook := `<workbook ` + mainNS + ` xmlns:r="` + relationNS + `"><sheets>`
	rels := `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	names := map[string]bool{}
	for i, sheet := range sheets {
		number := strconv.Itoa(i + 1)
		workbook += `<sheet name="` + shelfExportXML(shelfExportName(sheet.Name, names)) + `" sheetId="` + number + `" r:id="rId` + number + `"/>`
		rels += `<Relationship Id="rId` + number + `" Type="` + relationNS + `/worksheet" Target="worksheets/sheet` + number + `.xml"/>`
		contentTypes += `<Override PartName="/xl/worksheets/sheet` + number + `.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`
		var data strings.Builder
		data.WriteString(`<worksheet ` + mainNS + `><sheetViews><sheetView workbookViewId="0"><pane xSplit="4" ySplit="2" topLeftCell="E3" activePane="bottomRight" state="frozen"/></sheetView></sheetViews><sheetFormatPr defaultRowHeight="30"/><cols><col min="1" max="4" width="28" customWidth="1"/><col min="5" max="` + strconv.Itoa(max(5, len(sheet.Rows[1]))) + `" width="34" customWidth="1"/></cols><sheetData>`)
		for r, row := range sheet.Rows {
			fmt.Fprintf(&data, `<row r="%d">`, r+1)
			for c, cell := range row {
				if shelfExportTruncate(cell.Text, 32767) != cell.Text {
					return nil, apperrors.NewBadRequestError("Excel cell exceeds 32767 characters; narrow the export or shorten the recorded notes")
				}
				style := 0
				if r < 2 {
					style = 1
				} else if cell.PlannedEOM {
					style = 2
				}
				fmt.Fprintf(&data, `<c r="%s%d" t="inlineStr" s="%d"><is><t xml:space="preserve">%s</t></is></c>`, shelfExportColumn(c+1), r+1, style, shelfExportXML(cell.Text))
			}
			data.WriteString(`</row>`)
		}
		fmt.Fprintf(&data, `</sheetData><autoFilter ref="A2:%s%d"/></worksheet>`, shelfExportColumn(len(sheet.Rows[1])), len(sheet.Rows))
		if err := write("xl/worksheets/sheet"+number+".xml", data.String()); err != nil {
			return nil, err
		}
	}
	rels += `<Relationship Id="styles" Type="` + relationNS + `/styles" Target="styles.xml"/></Relationships>`
	styles := `<styleSheet ` + mainNS + `><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FFFFFF00"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/><xf numFmtId="0" fontId="0" fillId="2" borderId="0" xfId="0" applyFill="1" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
	for _, file := range []struct{ name, content string }{
		{"[Content_Types].xml", contentTypes + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="workbook" Type="` + relationNS + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", workbook + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", rels},
		{"xl/styles.xml", styles},
	} {
		if err := write(file.name, file.content); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
