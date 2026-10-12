package vaultpath

import (
	"path/filepath"
	"strings"
)

// UserMetadata identifies editor configuration, trash and desktop metadata.
// Operational files under the vault's root .reliquary directory stay verified.
// This is a content filter, not a replacement for validating asset paths.
func UserMetadata(relative string) bool {
	if filepath.IsAbs(relative) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}
	if parts[0] == ".reliquary" {
		return false
	}
	for _, part := range parts {
		switch part {
		case ".obsidian", ".trash", ".DS_Store", ".Spotlight-V100", ".Trashes", "Thumbs.db", "desktop.ini":
			return true
		}
		if strings.HasPrefix(part, "._") {
			return true
		}
	}
	return false
}
