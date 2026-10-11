package service

import (
	"sort"
	"strconv"

	"gorm.io/gorm"
)

// Maintenance mutations take the exclusive barrier. Ordinary persistence takes
// the shared barrier, allowing concurrent messages while excluding merge/prune.
// Every model call happens before either transaction starts.
func lockEntitySpace(tx *gorm.DB, tenantID uint64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "reliquary:entities:team:"+strconv.FormatUint(tenantID, 10)).Error
}

func lockEntitySpaceShared(tx *gorm.DB, tenantID uint64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended(?, 0))", "reliquary:entities:team:"+strconv.FormatUint(tenantID, 10)).Error
}

// Sort the actual lock IDs, including hash collisions, so overlapping identity
// sets cannot acquire advisory locks in opposite orders.
func lockEntityIdentities(tx *gorm.DB, tenantID uint64, keys []string) error {
	ids := make([]int64, 0, len(keys))
	seen := make(map[int64]bool, len(keys))
	for _, key := range keys {
		var id int64
		if err := tx.Raw("SELECT hashtextextended(?, 0)", "reliquary:entities:identity:"+strconv.FormatUint(tenantID, 10)+":"+key).Scan(&id).Error; err != nil {
			return err
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", id).Error; err != nil {
			return err
		}
	}
	return nil
}
