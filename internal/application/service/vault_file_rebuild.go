package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync"
	"time"

	"reliquary/internal/types"
)

var ErrVaultRebuildRunning = errors.New("a vault file rebuild is already running")

// Counts cover database-owned Markdown files. Replica refresh errors are returned
// separately because the replica includes metadata and attachments as well.
type VaultFileRebuildResult struct {
	Rebuilt bool `json:"rebuilt"`
	Written int  `json:"written"`
	Skipped int  `json:"skipped"`
	Failed  int  `json:"failed"`
}

type VaultFileRebuildStatus struct {
	Status string                  `json:"status"`
	Result *VaultFileRebuildResult `json:"result,omitempty"`
	Error  string                  `json:"error,omitempty"`
}

// Hold a dedicated PostgreSQL session so CLI and HTTP callers, including other
// server processes, share the same nonblocking lock. Never return a locked
// session to the connection pool.
func (v *VaultService) lockFileRebuild(ctx context.Context) (func(), error) {
	pool, err := v.db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var locked bool
	const lockID int64 = 7265646266696
	if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&locked); err != nil || !locked {
		if err != nil {
			conn.Raw(func(any) error { return driver.ErrBadConn })
			conn.Close()
			return nil, err
		}
		conn.Close()
		return nil, ErrVaultRebuildRunning
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := conn.ExecContext(releaseCtx, "SELECT pg_advisory_unlock($1)", lockID); err != nil {
				// A bad session must be discarded rather than pooled with its lock held.
				conn.Raw(func(any) error { return driver.ErrBadConn })
			}
			conn.Close()
		})
	}, nil
}

// RebuildFiles keeps the existing service contract for maintenance callers.
func (v *VaultService) RebuildFiles(ctx context.Context) error {
	_, err := v.RebuildFilesWithResult(ctx)
	return err
}

func (v *VaultService) RebuildFilesWithResult(ctx context.Context) (*VaultFileRebuildResult, error) {
	unlock, err := v.lockFileRebuild(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return v.rebuildFiles(ctx)
}

func (v *VaultService) rebuildFiles(ctx context.Context) (*VaultFileRebuildResult, error) {
	result := &VaultFileRebuildResult{}
	if err := ensureVaultDirectories(v.db.WithContext(ctx), v.root); err != nil {
		return result, err
	}
	var entries []types.Entry
	if err := v.db.WithContext(ctx).Where("vault_path<>'' AND parse_status<>'deleting'").Order("id").Find(&entries).Error; err != nil {
		return result, err
	}
	for _, entry := range entries {
		written, err := v.refreshHeader(ctx, entry.ID)
		if err != nil {
			result.Failed++
			return result, err
		}
		if written {
			result.Written++
		} else {
			result.Skipped++
		}
	}
	if v.replica != nil {
		if err := v.replica.Flush(ctx); err != nil {
			return result, err
		}
	}
	result.Rebuilt = true
	return result, nil
}

func (v *VaultService) FileRebuildStatus() VaultFileRebuildStatus {
	v.rebuildMu.Lock()
	defer v.rebuildMu.Unlock()
	state := v.rebuild
	if state.Status == "" {
		state.Status = "idle"
	}
	return state
}

// Acquire before accepting so simultaneous requests get a conflict immediately.
// The bounded background context survives the initiating HTTP connection.
func (v *VaultService) StartFileRebuild(ctx context.Context) (VaultFileRebuildStatus, error) {
	v.rebuildMu.Lock()
	defer v.rebuildMu.Unlock()
	if v.rebuildClosed {
		return VaultFileRebuildStatus{}, errors.New("vault file rebuild service is closed")
	}
	if v.rebuild.Status == "running" {
		return VaultFileRebuildStatus{}, ErrVaultRebuildRunning
	}
	unlock, err := v.lockFileRebuild(ctx)
	if err != nil {
		return VaultFileRebuildStatus{}, err
	}
	v.rebuild = VaultFileRebuildStatus{Status: "running"}
	workCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	v.rebuildCancel = cancel
	done := make(chan struct{})
	v.rebuildDone = done
	go func() {
		defer close(done)
		defer unlock()
		defer cancel()
		result, err := v.rebuildFiles(workCtx)
		unlock()
		v.rebuildMu.Lock()
		defer v.rebuildMu.Unlock()
		v.rebuild = VaultFileRebuildStatus{Status: "success", Result: result}
		if err != nil {
			v.rebuild.Status = "failed"
			v.rebuild.Error = err.Error()
		}
	}()
	return v.rebuild, nil
}

// Stop accepting work and wait before the header writer and DB pool are closed.
func (v *VaultService) closeFileRebuild() {
	v.rebuildMu.Lock()
	v.rebuildClosed = true
	if v.rebuildCancel != nil {
		v.rebuildCancel()
	}
	done := v.rebuildDone
	v.rebuildMu.Unlock()
	if done != nil {
		<-done
	}
}
