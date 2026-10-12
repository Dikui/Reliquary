package docparser

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"

	"strings"
	"testing"

	"reliquary/internal/infrastructure/docparser/anydoc"
	"reliquary/internal/types"
)

func TestPersonalParserSamples(t *testing.T) {
	for _, sample := range []struct {
		name, fileType, want string
		data                 []byte
	}{
		{"Markdown", "md", "Personal parser sample", []byte("# Personal parser sample\n\nRetained Markdown body.\n")},
		{"image", "png", "![sample.png](images/sample.png)", personalSamplePNG(t)},
		{"PDF", "pdf", "Personal parser sample", personalSamplePDF()},
		{"DOCX", "docx", "Personal parser sample", personalSampleDOCX(t)},
	} {
		t.Run(sample.name, func(t *testing.T) {
			reader, err := NewReader(context.Background(), "", sample.fileType, false, ReaderDeps{})
			if !anydoc.Available() && anydoc.Supports(sample.fileType, "") {
				if err == nil || !strings.Contains(err.Error(), "anydoc") || !strings.Contains(err.Error(), "-tags anydoc") {
					t.Fatalf("missing binding must explain how to enable anydoc: %v", err)
				}
				t.Logf("document conversion unavailable in this build: %v", err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := reader.Read(context.Background(), &types.ReadRequest{FileName: "sample." + sample.fileType, FileType: sample.fileType, FileContent: sample.data})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.MarkdownContent, sample.want) {
				t.Fatalf("body = %q, want %q", result.MarkdownContent, sample.want)
			}
			if sample.fileType == "png" && (len(result.ImageRefs) != 1 || !result.ImageRefs[0].IsOriginal || !bytes.Equal(result.ImageRefs[0].ImageData, sample.data)) {
				t.Fatalf("uploaded image bytes or original reference lost: %+v", result.ImageRefs)
			}
			t.Logf("parser=%s body=%q", types.DefaultParserEngine(sample.fileType), result.MarkdownContent)
		})
	}
}

func personalSampleDOCX(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Personal parser sample</w:t></w:r></w:p><w:p><w:r><w:t>Reliquary keeps personal documents on the local filesystem.</w:t></w:r></w:p><w:p><w:r><w:t>PostgreSQL holds the searchable entry for one user.</w:t></w:r></w:p></w:body></w:document>`,
	}
	for name, content := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func personalSamplePDF() []byte {
	content := "BT /F1 12 Tf 50 730 Td (Personal parser sample) Tj 0 -28 Td (Reliquary keeps personal documents on the local filesystem.) Tj 0 -28 Td (PostgreSQL holds the searchable entry for one user.) Tj ET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return pdf.Bytes()
}

func personalSamplePNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
