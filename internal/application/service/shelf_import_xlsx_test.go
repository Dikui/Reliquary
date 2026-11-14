package service

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func singleSheetWorkbook(t *testing.T, sheetXML string) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for _, file := range []struct{ name, content string }{
		{"xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="List" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/styles.xml", `<styleSheet><fills><fill><patternFill patternType="none"/></fill></fills><cellXfs><xf fillId="0"/></cellXfs></styleSheet>`},
		{"xl/worksheets/sheet1.xml", sheetXML},
	} {
		w, err := z.Create(file.name)
		require.NoError(t, err)
		_, err = w.Write([]byte(file.content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return data.Bytes()
}

// Excel writes and opens single-cell merge entries such as ref="A17"; a
// customer list containing one must not be rejected as a malformed workbook.
func TestReadImportWorkbookAcceptsSingleCellMerge(t *testing.T) {
	data := singleSheetWorkbook(t, `<worksheet><sheetData><row r="17"><c r="A17" t="inlineStr"><is><t>Item</t></is></c></row></sheetData><mergeCells><mergeCell ref="A17"/><mergeCell ref="B2:B3"/></mergeCells></worksheet>`)
	book, err := readImportWorkbook(data)
	require.NoError(t, err)
	merges := book.Sheets["List"].Merges
	require.Len(t, merges, 2)
	require.Equal(t, importMerge{Left: 1, Top: 17, Right: 1, Bottom: 17, Anchor: "A17"}, merges[0])

	_, err = readImportWorkbook(singleSheetWorkbook(t, `<worksheet><sheetData/><mergeCells><mergeCell ref="A1:B2:C3"/></mergeCells></worksheet>`))
	require.ErrorContains(t, err, "invalid merged range")
}

// ADR 0025: the inventory keeps signals parsers never read, so the coverage
// ledger can report them instead of dropping them silently.
func TestReadImportWorkbookKeepsInventorySignals(t *testing.T) {
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for _, file := range []struct{ name, content string }{
		{"xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="List" r:id="rId1" state="hidden"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/styles.xml", `<styleSheet><fonts><font><color theme="1"/></font><font><strike/><color theme="1"/></font><font><color rgb="ffff0000"/></font></fonts><fills><fill><patternFill patternType="none"/></fill></fills><cellXfs><xf fillId="0"/><xf fillId="0" fontId="1"/><xf fillId="0" fontId="2"/></cellXfs></styleSheet>`},
		{"xl/worksheets/sheet1.xml", `<worksheet xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><cols><col min="2" max="3" hidden="1"/></cols><sheetData><row r="1" hidden="1"><c r="A1" s="1" t="inlineStr"><is><t>Old</t></is></c><c r="B1" s="2" t="inlineStr"><is><t>Red</t></is></c></row></sheetData><hyperlinks><hyperlink ref="A1:B1" r:id="rId1"/><hyperlink ref="C2" location="Other!A1"/></hyperlinks><drawing r:id="rId3"/></worksheet>`},
		{"xl/worksheets/_rels/sheet1.xml.rels", `<Relationships><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/a" TargetMode="External"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="/xl/comments9.xml"/></Relationships>`},
		{"xl/comments9.xml", `<comments><commentList><comment ref="D4"><text><t>Only text</t></text></comment></commentList></comments>`},
	} {
		w, err := z.Create(file.name)
		require.NoError(t, err)
		_, err = w.Write([]byte(file.content))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	book, err := readImportWorkbook(data.Bytes())
	require.NoError(t, err)
	sheet := book.Sheets["List"]
	require.True(t, sheet.Hidden)
	require.True(t, sheet.Drawing)
	require.True(t, sheet.Cells["A1"].Strike)
	require.Empty(t, sheet.Cells["A1"].FontColor, "a strike font with the default color is not a color signal")
	require.Equal(t, "FFFF0000", sheet.Cells["B1"].FontColor)
	require.True(t, sheet.hiddenAt(1, 1))
	require.True(t, sheet.hiddenAt(3, 9))
	require.False(t, sheet.hiddenAt(4, 2))
	require.Equal(t, map[string]string{"A1": "https://example.com/a", "C2": "Other!A1"}, sheet.Hyperlinks)
	require.Equal(t, map[string]string{"D4": "Only text"}, sheet.Comments)
}
