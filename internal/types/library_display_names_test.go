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
	keys := map[string]string{"official": "systemOfficial", "media": "systemMedia", "references": "systemReferences", "personal": "systemPersonal", "ai-output": "systemAIOutput"}
	expectedEnglish := map[string]string{"official": "Official information", "media": "Media information", "references": "Reference documents", "personal": "Personal output", "ai-output": "AI output"}
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

func TestSystemLibraryDefaultTextUsesCurrentTerminology(t *testing.T) {
	retired := regexp.MustCompile(`(?i)文章|用户文档|整理结果|本人文档|知识库|\b(articles?|user documents?|personal documents?|organized results?|knowledge base)\b`)
	for _, library := range SystemLibraryDefaults() {
		values := []string{library.Name, library.Description, library.ReviewCriteria, library.DisplayName("en-US"), library.DisplayName("zh-CN")}
		for _, row := range library.ContentTypes {
			values = append(values, row.Name)
		}
		for _, text := range values {
			require.NotRegexp(t, retired, text, "system role %s", library.SystemRole)
		}
	}
}
