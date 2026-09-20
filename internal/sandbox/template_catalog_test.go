package sandbox

import "testing"

func TestIsStandardTemplateRecognizesProviderScopedName(t *testing.T) {
	for _, name := range []string{"reliquary", "team/reliquary", "project-b89e/Reliquary"} {
		if !isStandardTemplate(name) {
			t.Fatalf("expected %q to identify the Reliquary standard template", name)
		}
	}
	if isStandardTemplate("reliquary-custom") {
		t.Fatal("custom template must not be treated as the standard template")
	}
}
