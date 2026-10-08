package docparser

import (
	"context"
	"testing"

	"reliquary/internal/infrastructure/docparser/anydoc"
)

func TestNewReaderRoutesSimpleFormatsAndURLsInProcess(t *testing.T) {
	for _, tc := range []struct {
		fileType string
		isURL    bool
	}{{"md", false}, {"csv", false}, {"html", true}} {
		reader, err := NewReader(context.Background(), "", tc.fileType, tc.isURL, ReaderDeps{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := reader.(*SimpleFormatReader); !ok {
			t.Fatalf("reader = %T, want simple", reader)
		}
	}
}

func TestNewReaderRejectsUnsupportedTypesAndRemovedEngines(t *testing.T) {
	for _, ft := range []string{"html", "htm", "mhtml", "xmind"} {
		if SupportsFileType(ft) {
			t.Fatalf("upload %q must be unsupported", ft)
		}
		if _, err := NewReader(context.Background(), "", ft, false, ReaderDeps{}); err == nil {
			t.Fatalf("unsupported upload %q succeeded", ft)
		}
	}
	for _, name := range []string{"builtin", "markitdown", "paddleocr_vl", "paddleocr_vl_cloud"} {
		if _, err := NewReader(context.Background(), name, "pdf", false, ReaderDeps{}); err == nil {
			t.Fatalf("removed engine %q succeeded", name)
		}
	}
}

// The anydoc engine is only linked into builds tagged `anydoc`; everywhere
// else it must be listed as unavailable and refuse to build a reader, so a
// knowledge base configured for it fails loudly instead of parsing with
// something else.
func TestAnydocEngineFollowsBuildAvailability(t *testing.T) {
	reader, err := NewReader(context.Background(), AnydocEngineName, "docx", false, ReaderDeps{})

	if anydoc.Available() {
		if err != nil {
			t.Fatalf("NewReader: %v", err)
		}
		if _, ok := reader.(*AnydocReader); !ok {
			t.Fatalf("reader = %T, want *AnydocReader", reader)
		}
		return
	}
	if err == nil {
		t.Fatal("NewReader succeeded without the converter linked in, want an error")
	}
}

func TestListAllEnginesIncludesAnydoc(t *testing.T) {
	for _, engine := range ListAllEngines(nil) {
		if engine.Name != AnydocEngineName {
			continue
		}
		if engine.Available != anydoc.Available() {
			t.Errorf("anydoc availability = %v, want %v", engine.Available, anydoc.Available())
		}
		if !engine.Available && engine.UnavailableReason == "" {
			t.Error("anydoc is unavailable without a reason to show")
		}
		if len(engine.FileTypes) == 0 {
			t.Error("anydoc lists no file types")
		}
		return
	}
	t.Fatal("anydoc engine not found in the engine list")
}
