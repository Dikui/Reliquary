package vaultpath

import "testing"

func TestUserMetadataOnlyFiltersExplicitUserPaths(t *testing.T) {
	for _, relative := range []string{
		".obsidian/app.json", ".trash/x.md", ".DS_Store", "personal/.obsidian/plugins/plugin/data.json",
		"personal/nested/.trash/x.md", "reports/.DS_Store", "articles/2026-10/._article.md",
		"attachments/.Spotlight-V100/index", "personal/.Trashes/x.md", "reports/Thumbs.db", "personal/desktop.ini",
	} {
		if !UserMetadata(relative) {
			t.Errorf("user metadata path was not filtered: %s", relative)
		}
	}
	for _, relative := range []string{
		"articles/2026-10/Obsidian-config-12345678.md", "personal/obsidian.md", "personal/.NET.md",
		".reliquary/manifest.json", ".reliquary/resources/.DS_Store", ".reliquary/data/.obsidian/entries.json",
		".git/config", "personal/.custom/note.md", "../.obsidian/app.json", "/.obsidian/app.json", ".obsidian/../article.md",
	} {
		if UserMetadata(relative) {
			t.Errorf("managed or invalid path was filtered: %s", relative)
		}
	}
}
