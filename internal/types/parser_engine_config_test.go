package types

import "testing"

func TestParserEngineConfigResolveChatParserEngine(t *testing.T) {
	config := &ParserEngineConfig{ChatParserEngineRules: []ParserEngineRule{
		{FileTypes: []string{"pdf", ".docx"}, Engine: "mineru"},
		{FileTypes: []string{"xlsx"}, Engine: "anydoc"},
	}}
	for input, expected := range map[string]string{
		"PDF": "mineru", ".docx": "mineru", "xlsx": "anydoc",
		"txt": "simple", "pptx": "anydoc", ".PPT": "anydoc",
	} {
		if actual := config.ResolveChatParserEngine(input); actual != expected {
			t.Fatalf("ResolveChatParserEngine(%q) = %q, want %q", input, actual, expected)
		}
	}
	var nilConfig *ParserEngineConfig
	if actual := nilConfig.ResolveChatParserEngine("pdf"); actual != "anydoc" {
		t.Fatalf("nil config resolved %q", actual)
	}
	if actual := nilConfig.ResolveChatParserEngine("pptx"); actual != "anydoc" {
		t.Fatalf("nil config pptx resolved %q, want anydoc", actual)
	}
}
