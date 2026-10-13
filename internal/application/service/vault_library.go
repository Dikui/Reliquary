package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

// Library directories are stable identities, independent of display names.
func vaultLibraryDirectory(db *gorm.DB, id string) (string, error) {
	var library types.Library
	if err := db.Unscoped().First(&library, "id=?", id).Error; err != nil {
		return "", err
	}
	directory := library.Directory
	if directory == "" || directory == "." || directory == ".." || strings.ContainsAny(directory, `/\\`) || strings.HasPrefix(directory, ".") || directory == "attachments" {
		return "", fmt.Errorf("invalid library directory %q", directory)
	}
	return directory, nil
}

func ensureVaultDirectories(db *gorm.DB, root string) error {
	// Historical SQL backup inspection must not invent library directories.
	if !db.Migrator().HasColumn("libraries", "directory") {
		return os.MkdirAll(filepath.Join(root, "attachments"), 0755)
	}
	var libraries []types.Library
	if err := db.Find(&libraries).Error; err != nil {
		return err
	}
	directories := []string{"attachments"}
	for _, library := range libraries {
		directory, err := vaultLibraryDirectory(db, library.ID)
		if err != nil {
			return err
		}
		directories = append(directories, directory)
	}
	for _, directory := range directories {
		path, err := vaultAbsolute(root, directory)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
	}
	return nil
}

func vaultEntryPath(directory, title, id string) string {
	return filepath.ToSlash(filepath.Join(directory, vaultSlug(title)+"-"+id[:min(8, len(id))]+".md"))
}

// RebuildFiles renders all database-owned Markdown without reading edited headers.
// Image bytes remain in attachments or must be restored from a physical backup.
func (v *VaultService) RebuildFiles(ctx context.Context) error {
	if err := ensureVaultDirectories(v.db.WithContext(ctx), v.root); err != nil {
		return err
	}
	var entries []types.Entry
	if err := v.db.WithContext(ctx).Where("vault_path<>'' AND parse_status<>'deleting'").Find(&entries).Error; err != nil {
		return err
	}
	for _, entry := range entries {
		if err := v.RefreshHeader(ctx, entry.ID); err != nil {
			return err
		}
	}
	if v.replica != nil {
		return v.replica.Flush(ctx)
	}
	return nil
}
