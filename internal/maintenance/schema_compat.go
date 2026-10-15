package maintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

// WithCurrentNames lets the offline helper inspect and scrub a pre-naming
// database without upgrading its persisted schema or migration history. DDL and
// any scrub changes share one transaction; failures roll everything back.
// The exact old application remains responsible for running the old deployment.
func WithCurrentNames(db *gorm.DB, run func(*gorm.DB) error) error {
	var legacy, oldEntries, oldLibrary, oldSources bool
	if err := db.Raw("SELECT to_regclass('public.tenants') IS NOT NULL").Scan(&legacy).Error; err != nil {
		return err
	}
	if err := db.Raw("SELECT to_regclass('public.knowledges') IS NOT NULL").Scan(&oldEntries).Error; err != nil {
		return err
	}
	if err := db.Raw("SELECT to_regclass('public.knowledge_bases') IS NOT NULL").Scan(&oldLibrary).Error; err != nil {
		return err
	}
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='data_sources' AND column_name='knowledge_base_id')").Scan(&oldSources).Error; err != nil {
		return err
	}
	if !legacy && !oldEntries && !oldLibrary {
		return run(db)
	}
	read := func(version, direction string) (string, error) {
		directory, err := os.Getwd()
		if err != nil {
			return "", err
		}
		var data []byte
		for {
			data, err = os.ReadFile(filepath.Join(directory, "migrations/versioned/"+version+"."+direction+".sql"))
			if err == nil || !os.IsNotExist(err) || filepath.Dir(directory) == directory {
				break
			}
			directory = filepath.Dir(directory)
		}
		if err != nil {
			return "", fmt.Errorf("read offline naming adapter: %w", err)
		}
		return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(data)), "BEGIN;"), "COMMIT;"), nil
	}
	versions := []string{}
	if legacy {
		versions = append(versions, "000002_space_naming")
	}
	if oldEntries {
		versions = append(versions, "000003_entry_naming")
	}
	if oldLibrary {
		versions = append(versions, "000007_library_naming")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, version := range versions {
			up, err := read(version, "up")
			if err != nil {
				return err
			}
			if err = tx.Exec(up).Error; err != nil {
				return err
			}
		}
		if oldSources {
			if err := tx.Exec("ALTER TABLE public.data_sources RENAME COLUMN knowledge_base_id TO library_id").Error; err != nil {
				return err
			}
		}
		if err := run(tx); err != nil {
			return err
		}
		if oldSources {
			if err := tx.Exec("ALTER TABLE public.data_sources RENAME COLUMN library_id TO knowledge_base_id").Error; err != nil {
				return err
			}
		}
		for i := len(versions) - 1; i >= 0; i-- {
			down, err := read(versions[i], "down")
			if err != nil {
				return err
			}
			if err = tx.Exec(down).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
