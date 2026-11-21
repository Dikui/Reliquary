package database

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type PoolRole int

const (
	ApplicationPool PoolRole = iota
	MaintenancePool
)

// Open configures the shared SQL pool before startup work or workers can use it.
func (role PoolRole) Open(dialector gorm.Dialector, config *gorm.Config) (*gorm.DB, error) {
	app, err := poolLimit("DB_MAX_OPEN_CONNS", 40)
	if err != nil {
		return nil, err
	}
	maintenance, err := poolLimit("DB_MAINTENANCE_MAX_OPEN_CONNS", 4)
	if err != nil {
		return nil, err
	}
	// The capacity query below also verifies connectivity, after the cap is set.
	config.DisableAutomaticPing = true
	db, err := gorm.Open(dialector, config)
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	limit := app
	if role == MaintenancePool {
		limit = maintenance
	}
	pool.SetMaxOpenConns(limit)
	pool.SetMaxIdleConns(min(10, limit))
	pool.SetConnMaxLifetime(10 * time.Minute)

	// One migration session per process, plus pg_dump and pg_restore during a clone.
	const extraSessions = 2 + 2
	budget := int64(app) + int64(maintenance) + extraSessions
	var available int
	err = pool.QueryRow(`SELECT current_setting('max_connections')::int
		- current_setting('superuser_reserved_connections')::int
		- COALESCE(current_setting('reserved_connections', true)::int, 0)`).Scan(&available)
	if err == nil && budget >= int64(available) {
		err = fmt.Errorf("PostgreSQL connection budget %d must be below %d non-reserved slots (DB_MAX_OPEN_CONNS=%d, DB_MAINTENANCE_MAX_OPEN_CONNS=%d, migration and clone sessions=%d)", budget, available, app, maintenance, extraSessions)
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return db, nil
}

func poolLimit(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	limit, err := strconv.ParseInt(raw, 10, 32)
	// Replay can hold the executor lock and a transaction while issuing a query.
	if err != nil || limit < 3 {
		return 0, fmt.Errorf("%s must be an integer of at least 3", key)
	}
	return int(limit), nil
}
