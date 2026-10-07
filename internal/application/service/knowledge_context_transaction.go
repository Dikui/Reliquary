package service

import (
	"strconv"

	"gorm.io/gorm"
)

// Every graph mutation obtains this lock before reading or locking graph rows.
// PostgreSQL releases it when the transaction ends. Model calls stay outside
// the transaction, so they never hold the team persistence lock.
func lockContextTeam(tx *gorm.DB, tenantID uint64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "reliquary:knowledge-context:team:"+strconv.FormatUint(tenantID, 10)).Error
}
