package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

const policyProbeLock int64 = 714150016

// CaptureChatGPTPolicy returns the full safe policy rows in PostgreSQL's JSON
// format. It includes neither accounts nor their authorization credentials.
func CaptureChatGPTPolicy(ctx context.Context, db *gorm.DB) ([]map[string]any, error) {
	rows, err := db.WithContext(ctx).Raw(`SELECT to_jsonb(p)::text FROM chatgpt_policy p ORDER BY id`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var row map[string]any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&row); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// AppendChatGPTPolicyProbe appends an exact before/after policy transition only
// when a real probe changed it. The unchanged quota and the call's consumption
// remain in the live row; audit rows never reverse or bypass that accounting.
func AppendChatGPTPolicyProbe(ctx context.Context, db *gorm.DB, modelID string, before, after []map[string]any) error {
	previous, err := json.Marshal(before)
	if err != nil {
		return err
	}
	next, err := json.Marshal(after)
	if err != nil || bytes.Equal(previous, next) {
		return err
	}
	var when time.Time
	if err := db.WithContext(ctx).Raw(`SELECT GREATEST(clock_timestamp(), COALESCE(MAX(created_at)+interval '1 microsecond',clock_timestamp())) FROM operation_logs WHERE operation_type='maintenance_model_probe'`).Scan(&when).Error; err != nil {
		return err
	}
	return db.WithContext(ctx).Create(&types.OperationLog{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, Actor: "maintenance",
		OperationType: "maintenance_model_probe", ObjectID: modelID, Before: previous, After: next, CreatedAt: when}).Error
}

// WithChatGPTPolicyProbeAudit serializes maintenance probes across processes.
// The advisory lock uses its own connection, leaving ordinary Reserve's row
// transaction available to the real adapter. Failed calls are audited too.
func WithChatGPTPolicyProbeAudit(ctx context.Context, db *gorm.DB, modelID string, probe func() error) error {
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) (resultErr error) {
		if err := conn.Exec("SELECT pg_advisory_lock(?)", policyProbeLock).Error; err != nil {
			return err
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, conn.WithContext(cleanup).Exec("SELECT pg_advisory_unlock(?)", policyProbeLock).Error)
		}()
		before, err := CaptureChatGPTPolicy(ctx, conn)
		if err != nil {
			return err
		}
		probeErr := probe()
		// A timed-out HTTP probe may already have reserved quota. Preserve its
		// transition after cancellation instead of leaving an unaudited change.
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		after, err := CaptureChatGPTPolicy(auditCtx, conn)
		if err != nil {
			return errors.Join(probeErr, err)
		}
		return errors.Join(probeErr, AppendChatGPTPolicyProbe(auditCtx, conn, modelID, before, after))
	})
}
