package modelcalls

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

type storedContent struct {
	ID            string `gorm:"primaryKey"`
	RequestGzip   []byte
	ResponseGzip  []byte
	RequestBytes  int64
	ResponseBytes int64
	ResponseType  string
	HTTPStatus    int
	Complete      bool
}

func (storedContent) TableName() string { return "model_call_contents" }
func compressContent(value string) ([]byte, error) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := io.WriteString(w, value); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func expandContent(value []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(value))
	if err != nil {
		return "", err
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	return string(b), err
}
func (s *Store) writeContent(ctx context.Context, call *types.ModelCall) error {
	c := call.Content
	if c == nil {
		return nil
	}
	request, err := compressContent(c.Request)
	if err != nil {
		return err
	}
	response, err := compressContent(c.Response)
	if err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if call.EntryID != nil {
			key := fmt.Sprintf("%d:%s", call.SpaceID, *call.EntryID)
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,24))", key).Error; err != nil {
				return err
			}
			var deleted int64
			if err := tx.Table("model_call_content_deletions").Where("space_id=? AND entry_id=?", call.SpaceID, *call.EntryID).Count(&deleted).Error; err != nil {
				return err
			}
			if deleted > 0 {
				return nil
			}
		}
		return tx.Create(&storedContent{ID: call.ID, RequestGzip: request, ResponseGzip: response, RequestBytes: int64(len(c.Request)), ResponseBytes: int64(len(c.Response)), ResponseType: c.ResponseType, HTTPStatus: c.HTTPStatus, Complete: c.Complete}).Error
	})
}
func (s *Store) Content(ctx context.Context, space uint64, id string) (*types.ModelCallContent, error) {
	var row storedContent
	err := s.DB.WithContext(ctx).Table("model_call_contents AS content").Select("content.*").Joins("JOIN model_calls AS call ON call.id=content.id").Where("call.space_id=? AND call.id=?", space, id).Take(&row).Error
	if err != nil {
		return nil, err
	}
	request, err := expandContent(row.RequestGzip)
	if err != nil {
		return nil, err
	}
	response, err := expandContent(row.ResponseGzip)
	if err != nil {
		return nil, err
	}
	return &types.ModelCallContent{ID: id, Request: request, Response: response, RequestBytes: row.RequestBytes, ResponseBytes: row.ResponseBytes, ResponseType: row.ResponseType, HTTPStatus: row.HTTPStatus, Complete: row.Complete}, nil
}

// One bounded worker keeps storage off the HTTP response path. Saturation is logged.
type contentJob struct {
	ctx  context.Context
	db   *gorm.DB
	call *types.ModelCall
	done chan struct{}
}

var contentQueue = make(chan contentJob, 64)
var contentWorker sync.Once

func startContentWorker() {
	contentWorker.Do(func() {
		go func() {
			for job := range contentQueue {
				if job.done != nil {
					close(job.done)
					continue
				}
				ctx, cancel := context.WithTimeout(job.ctx, 10*time.Second)
				if err := NewStore(job.db).Write(ctx, job.call); err != nil {
					logger.Warnf(ctx, "model call persistence failed: %v", err)
				}
				cancel()
			}
		}()
	})
}
func (s *Store) Enqueue(ctx context.Context, call *types.ModelCall) error {
	startContentWorker()
	select {
	case contentQueue <- contentJob{ctx: context.WithoutCancel(ctx), db: s.DB, call: call}:
		return nil
	default:
		return fmt.Errorf("model call storage queue is full")
	}
}

// FlushWrites allows orderly shutdown and deterministic integration checks.
func FlushWrites(ctx context.Context) error {
	startContentWorker()
	done := make(chan struct{})
	select {
	case contentQueue <- contentJob{done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func retentionNumber(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 36500 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return n, nil
}
func (s *Store) PruneContents(ctx context.Context) error {
	days, err := retentionNumber("RELIQUARY_MODEL_CALL_CONTENT_RETENTION_DAYS")
	if err != nil {
		return err
	}
	attempts, err := retentionNumber("RELIQUARY_MODEL_CALL_CONTENT_RETENTION_ATTEMPTS")
	if err != nil {
		return err
	}
	if days > 0 {
		if err := s.DB.WithContext(ctx).Exec("DELETE FROM model_call_contents WHERE id IN (SELECT id FROM model_calls WHERE requested_at < ?)", time.Now().UTC().AddDate(0, 0, -days)).Error; err != nil {
			return err
		}
	}
	if attempts > 0 {
		return s.DB.WithContext(ctx).Exec(`DELETE FROM model_call_contents WHERE id IN (
   SELECT id FROM (SELECT id, dense_rank() OVER (PARTITION BY space_id,entry_id ORDER BY processing_attempt DESC) AS rank
    FROM model_calls WHERE entry_id IS NOT NULL) ranked WHERE rank > ?)`, attempts).Error
	}
	return nil
}
