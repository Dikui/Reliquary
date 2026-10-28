package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Explicit role queries are allowed: they select a system role, never a first library.
func TestNoSpaceSingletonLibraryAccess(t *testing.T) {
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			source := string(data)
			for _, legacy := range []string{"GetLibraryBySpaceID(", "EnsureSingleLibrary(", ".GetLibrary(ctx)", ".GetSystemLibrary(ctx)"} {
				if strings.Contains(source, legacy) {
					t.Errorf("%s contains singleton accessor %s", path, legacy)
				}
			}
			for _, line := range strings.Split(source, "\n") {
				if strings.Contains(line, ".First(&library)") || strings.Contains(line, ".First(library)") {
					if strings.Contains(line, `Where("space_id`) && !strings.Contains(line, "system_role") && !strings.Contains(line, "AND id=?") {
						t.Errorf("%s contains first-library query: %s", path, line)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
