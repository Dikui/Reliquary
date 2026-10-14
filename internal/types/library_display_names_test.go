package types

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSystemLibraryDisplayNamesMatchFrontendLocales(t *testing.T) {
	source, err := os.ReadFile("../../frontend/src/i18n/locales/ikb18.ts")
	require.NoError(t, err)
	keys := map[string]string{"institutional": "systemInstitutional", "opinions": "systemOpinions", "references": "systemReferences", "mine": "systemMine", "ai-reports": "systemReports"}
	expectedEnglish := map[string]string{"institutional": "Institutional releases", "opinions": "Personal opinions", "references": "References", "mine": "My materials", "ai-reports": "AI reports"}
	require.Len(t, SystemLibraryDefaults(), len(keys))
	for _, library := range SystemLibraryDefaults() {
		t.Run(library.SystemRole, func(t *testing.T) {
			names := regexp.MustCompile(regexp.QuoteMeta(keys[library.SystemRole])+`:\s*'([^']+)'`).FindAllStringSubmatch(string(source), -1)
			require.Len(t, names, 2, "Chinese and English UI role names must be present")
			require.Equal(t, library.Name, names[0][1])
			require.Equal(t, expectedEnglish[library.SystemRole], names[1][1])
			for _, language := range []string{"zh", "zh-CN", "zh-HK"} {
				require.Equal(t, names[0][1], library.DisplayName(language))
			}
			for _, language := range []string{"en", "en-US", "en-GB"} {
				require.Equal(t, names[1][1], library.DisplayName(language))
			}
			renamed := *library
			renamed.Name = "Custom name"
			require.Equal(t, "Custom name", renamed.DisplayName("en-US"))
			require.Equal(t, "Custom name", renamed.DisplayName("zh-CN"))
		})
	}
	custom := Library{Name: "My custom materials"}
	require.Equal(t, custom.Name, custom.DisplayName("en-US"))
	require.Equal(t, custom.Name, custom.DisplayName("zh-CN"))
}
