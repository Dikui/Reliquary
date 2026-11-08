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
