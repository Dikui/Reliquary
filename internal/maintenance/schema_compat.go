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
	var legacy bool
	if err := db.Raw("SELECT to_regclass('public.tenants') IS NOT NULL").Scan(&legacy).Error; err != nil {
		return err
	}
	if !legacy {
		return run(db)
	}
	read := func(direction string) (string, error) {
		directory, err := os.Getwd()
		if err != nil {
			return "", err
		}
		var data []byte
		for {
			data, err = os.ReadFile(filepath.Join(directory, "migrations/versioned/000002_space_naming."+direction+".sql"))
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
	up, err := read("up")
	if err != nil {
		return err
	}
	down, err := read("down")
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(up).Error; err != nil {
			return err
		}
		if err := run(tx); err != nil {
			return err
		}
		return tx.Exec(down).Error
	})
}
