package service

import (
	"strconv"

	"gorm.io/gorm"
)

// Directory edits take the exclusive barrier. Association and follow updates
// share it so they cannot attach a reference to an entity being deleted.
func lockEntitySpace(tx *gorm.DB, spaceID uint64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "reliquary:entities:team:"+strconv.FormatUint(spaceID, 10)).Error
}

func lockEntitySpaceShared(tx *gorm.DB, spaceID uint64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended(?, 0))", "reliquary:entities:team:"+strconv.FormatUint(spaceID, 10)).Error
}
