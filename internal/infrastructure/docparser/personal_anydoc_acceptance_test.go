//go:build anydoc

package docparser

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"reliquary/internal/infrastructure/docparser/anydoc"
	"reliquary/internal/types"
)

func TestPersonalAnydocAcceptance(t *testing.T) {
	if !anydoc.Available() {
		t.Fatal("acceptance requires the linked anydoc library")
	}
	paragraphs := []string{
		"Personal parser sample",
		"Reliquary keeps personal documents on the local filesystem.",
		"PostgreSQL holds the searchable knowledge for one user.",
	}
	for _, sample := range []struct {
		name, fileType string
		data           []byte
		want           []string
		table          bool
		scanned        bool
	}{
		{name: "text-layer.pdf", fileType: "pdf", data: personalSamplePDF(), want: paragraphs},
		{name: "paragraphs.docx", fileType: "docx", data: personalSampleDOCX(t), want: paragraphs},
		{name: "table.docx", fileType: "docx", data: personalSampleTableDOCX(t), want: []string{"Table sample inventory", "Item", "Count", "Apples", "12", "Oranges", "7"}, table: true},
		{name: "scanned.pdf", fileType: "pdf", data: personalSampleScannedPDF(), scanned: true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			reader, err := NewReader(t.Context(), "", sample.fileType, false, ReaderDeps{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := reader.Read(context.Background(), &types.ReadRequest{FileName: sample.name, FileType: sample.fileType, FileContent: sample.data})
			record := map[string]any{"file": sample.name, "sha256": fmt.Sprintf("%x", sha256.Sum256(sample.data)), "expected_fragments": sample.want, "mineru_configured": false}
			if sample.scanned {
				if err == nil {
					t.Fatalf("image-only PDF must require OCR without MinerU: %+v", result)
				}
				record["error"] = err.Error()
				writePersonalAnydocEvidence(t, sample.name, sample.data, "", record)
				if !strings.Contains(strings.ToLower(err.Error()), "ocr") || !strings.Contains(err.Error(), "MinerU") {
					t.Fatalf("scanned PDF needs an explicit OCR/MinerU configuration hint: %v", err)
				}
				t.Logf("scanned PDF rejected clearly: %v", err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || strings.TrimSpace(result.MarkdownContent) == "" {
				t.Fatal("conversion returned no body")
			}
			body := strings.Join(strings.Fields(result.MarkdownContent), " ")
			record["metadata"] = result.Metadata
			record["body"] = result.MarkdownContent
			writePersonalAnydocEvidence(t, sample.name, sample.data, result.MarkdownContent, record)
			if result.Metadata["parser"] != "anydoc" || result.Metadata["anydoc_version"] != anydoc.Version() {
				t.Fatalf("conversion did not use linked anydoc: %v", result.Metadata)
			}
			for _, fragment := range sample.want {
				if !strings.Contains(body, fragment) {
					t.Errorf("missing original fragment %q in body:\n%s", fragment, result.MarkdownContent)
				}
			}
			if sample.table {
				for _, row := range []string{`(?m)\|\s*Apples\s*\|\s*12\s*\|`, `(?m)\|\s*Oranges\s*\|\s*7\s*\|`} {
					if !regexp.MustCompile(row).MatchString(result.MarkdownContent) {
						t.Errorf("table row lost its column relationship, pattern %q:\n%s", row, result.MarkdownContent)
					}
				}
			}
			t.Logf("parser=%s version=%s body=%q", result.Metadata["parser"], result.Metadata["anydoc_version"], result.MarkdownContent)
		})
	}
}

func writePersonalAnydocEvidence(t *testing.T, name string, data []byte, body string, record map[string]any) {
	t.Helper()
	dir := os.Getenv("RELIQUARY_ANYDOC_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for file, contents := range map[string][]byte{name: data, name + ".md": []byte(body), name + ".json": append(encoded, '\n')} {
		if err := os.WriteFile(filepath.Join(dir, file), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func personalSampleTableDOCX(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Table sample inventory</w:t></w:r></w:p><w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid><w:gridCol w:w="3000"/><w:gridCol w:w="3000"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>Item</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Count</w:t></w:r></w:p></w:tc></w:tr><w:tr><w:tc><w:p><w:r><w:t>Apples</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>12</w:t></w:r></w:p></w:tc></w:tr><w:tr><w:tc><w:p><w:r><w:t>Oranges</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>7</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`},
	}
	for _, part := range parts {
		f, err := w.Create(part.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(part.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// An image-only page contains a raster XObject and no PDF text operators.
func personalSampleScannedPDF() []byte {
	imageData := bytes.Repeat([]byte{0, 255, 255, 0}, 16)
	pageContent := "q 400 0 0 400 50 200 cm /Scan Do Q\n"
	objects := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Scan 5 0 R >> >> >>"),
		[]byte(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(pageContent), pageContent)),
		append([]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length %d >>\nstream\n", len(imageData))), append(imageData, []byte("\nendstream")...)...),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n", i+1)
		pdf.Write(object)
		pdf.WriteString("\nendobj\n")
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return pdf.Bytes()
}
