package tools

import "testing"

func TestAvailableToolsExcludeRemovedDatabaseQuery(t *testing.T) {
	for _, tool := range AvailableToolDefinitions() {
		if tool.Name == "database_query" {
			t.Fatal("removed database tool is advertised")
		}
	}
	hasDataAnalysis := false
	for _, name := range DefaultAllowedTools() {
		if name == "database_query" {
			t.Fatal("removed database tool is allowed")
		}
		if name == ToolDataAnalysis {
			hasDataAnalysis = true
		}
	}
	if !hasDataAnalysis {
		t.Fatal("data analysis must remain available")
	}
}
