package service

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type ingestRollbackModel struct {
	*articleProcessingChat
	failures int
}

func (m *ingestRollbackModel) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	for _, message := range messages {
		if strings.Contains(message.Content, "rollbackmodelmarker") {
			m.failures++
			return nil, errors.New("fixture ingest model unavailable")
		}
	}
	return m.articleProcessingChat.Chat(ctx, messages, options)
}

func ingestRollbackCounts(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64)
	for _, table := range []string{"entries", "resources", "resource_bindings", "entry_repost_sources", "operation_logs", "task_pending_ops", "ingest_assets"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		counts[table] = count
	}
	return counts
}

func ingestRollbackBytes(t *testing.T, roots ...string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	for _, root := range roots {
		require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = body
			return nil
		}))
	}
	return files
}

type ingestRollbackDeleteFile struct {
	interfaces.FileService
	failNext bool
	failures int
}

func (s *ingestRollbackDeleteFile) DeleteFile(ctx context.Context, path string) error {
	if s.failNext {
		s.failNext = false
		s.failures++
		return errors.New("fixture physical file deletion unavailable")
	}
	return s.FileService.DeleteFile(ctx, path)
}
