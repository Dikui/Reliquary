package docparser

import (
	"reliquary/internal/types"
	"testing"
)

func TestListAllEnginesRetainsOnlyPersonalParsers(t *testing.T) {
	names := map[string]bool{}
	for _, engine := range ListAllEngines(nil) {
		names[engine.Name] = true
	}
	if len(names) != 4 {
		t.Fatalf("engines = %v, want four retained parsers", names)
	}
	for _, name := range []string{"simple", "anydoc", "mineru", "mineru_cloud"} {
		if !names[name] {
			t.Errorf("missing parser %q", name)
		}
	}
}

func TestDefaultParserEngineUsesAnydocRegardlessOfBuild(t *testing.T) {
	for _, ft := range []string{"pptx", "ppt", "pdf", "docx"} {
		if got := types.DefaultParserEngine(ft); got != AnydocEngineName {
			t.Errorf("DefaultParserEngine(%s) = %q, want anydoc", ft, got)
		}
	}
	if got := types.DefaultParserEngine("csv"); got != SimpleEngineName {
		t.Errorf("csv default = %q, want simple", got)
	}
}
