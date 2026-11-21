package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The connector supplies PostgreSQL's capacity query in memory. GORM and the
// database/sql pool still open, hold and release real driver.Conn instances.
type slotConnector struct{ slots int64 }

func (c slotConnector) Driver() driver.Driver                        { return c }
func (c slotConnector) Open(string) (driver.Conn, error)             { return slotConn{slots: c.slots}, nil }
func (c slotConnector) Connect(context.Context) (driver.Conn, error) { return c.Open("") }

type slotConn struct{ slots int64 }

func (c slotConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c slotConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (c slotConn) Close() error                        { return nil }
func (c slotConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &slotRows{slots: c.slots}, nil
}

type slotRows struct{ slots int64 }

func (r *slotRows) Columns() []string { return []string{"slots"} }
func (r *slotRows) Close() error      { return nil }
func (r *slotRows) Next(row []driver.Value) error {
	if r.slots < 0 {
		return io.EOF
	}
	row[0], r.slots = r.slots, -1
	return nil
}

func testDialector(t *testing.T, slots int64) gorm.Dialector {
	t.Helper()
	pool := sql.OpenDB(slotConnector{slots})
	t.Cleanup(func() { pool.Close() })
	return postgres.New(postgres.Config{Conn: pool})
}

// Regression for SQLSTATE 53300 during bulk transactional enqueue.
func TestPostgresPoolWaitsAtConfiguredLimit(t *testing.T) {
	for _, tc := range []struct {
		name             string
		role             PoolRole
		app, maintenance string
		want             int
	}{
		{"app default", ApplicationPool, "", "", 40},
		{"maintenance default", MaintenancePool, "", "", 4},
		{"app override", ApplicationPool, "6", "3", 6},
		{"maintenance override", MaintenancePool, "6", "3", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_MAX_OPEN_CONNS", tc.app)
			t.Setenv("DB_MAINTENANCE_MAX_OPEN_CONNS", tc.maintenance)
			db, err := tc.role.Open(testDialector(t, 97), &gorm.Config{})
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			require.Equal(t, tc.want, pool.Stats().MaxOpenConnections)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var held *sql.Conn
			for range tc.want {
				conn, err := pool.Conn(ctx)
				require.NoError(t, err)
				defer conn.Close()
				held = conn
			}
			done := make(chan error, 1)
			go func() {
				conn, err := pool.Conn(ctx)
				if err == nil {
					err = conn.Close()
				}
				done <- err
			}()
			require.Eventually(t, func() bool { return pool.Stats().WaitCount == 1 }, time.Second, time.Millisecond)
			require.Equal(t, tc.want, pool.Stats().OpenConnections)
			require.NoError(t, held.Close())
			require.NoError(t, <-done)
		})
	}
}

func TestPostgresPoolRequiresConnectionHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name, app, maintenance, wantError string
	}{
		{"unbounded app", "0", "4", "DB_MAX_OPEN_CONNS"},
		{"maintenance replay needs three slots", "40", "2", "DB_MAINTENANCE_MAX_OPEN_CONNS"},
		{"invalid limit", "many", "4", "DB_MAX_OPEN_CONNS"},
		{"uses last ordinary slot", "40", "4", "budget 48 must be below 48"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DB_MAX_OPEN_CONNS", tc.app)
			t.Setenv("DB_MAINTENANCE_MAX_OPEN_CONNS", tc.maintenance)
			_, err := ApplicationPool.Open(testDialector(t, 48), &gorm.Config{})
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}
