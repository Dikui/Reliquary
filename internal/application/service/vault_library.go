package service

import (
	"errors"
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
	if err := migrateVaultLibraryDirectories(db, root, libraries); err != nil {
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

// Move only directories whose archived database paths have already migrated.
func migrateVaultLibraryDirectories(db *gorm.DB, root string, libraries []types.Library) error {
	previous := map[string]string{"official": "institutional", "media": "opinions", "personal": "mine", "ai-output": "ai-reports"}
	type move struct{ from, to string }
	var moves []move
	for _, library := range libraries {
		old, exists := previous[library.SystemRole]
		if !exists || library.Directory != library.SystemRole {
			continue
		}
		var archived int64
		if err := db.Unscoped().Model(&types.Entry{}).Where("library_id=? AND vault_path LIKE ?", library.ID, library.Directory+"/%").Count(&archived).Error; err != nil {
			return err
		}
		if archived == 0 {
			continue
		}
		from, err := vaultAbsolute(root, old)
		if err != nil {
			return err
		}
		info, err := os.Stat(from)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("old library path is not a directory: %s", from)
		}
		to, err := vaultAbsolute(root, library.Directory)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(to)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("library directory migration conflict: both %s and %s contain files", from, to)
		}
		moves = append(moves, move{from, to})
	}
	for _, item := range moves {
		if info, err := os.Lstat(item.to); err == nil {
			if !info.IsDir() {
				return fmt.Errorf("library migration target is not a directory: %s", item.to)
			}
			// Remove accepts only an empty directory; files added since preflight stop the move.
			if err := os.Remove(item.to); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(item.from, item.to); err != nil {
			return fmt.Errorf("move library directory %s to %s: %w", item.from, item.to, err)
		}
	}
	return nil
}
