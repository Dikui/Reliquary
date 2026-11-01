package service

import (
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	"reliquary/internal/testutil/redistest"
	"reliquary/internal/types"
	"testing"
)

// Exercise the production resource decorator: an in-memory file driver alone
// cannot detect resource resolution through a transaction that has ended.
func TestIngestResourceDuplicateCleanupWithinTransaction(t *testing.T) {
	const body = "星河研究院地热研究团队在玄武岩山区钻取岩芯，逐层记录裂隙密度、矿物组成与热导率。钻孔测温结果揭示浅层地下水循环对温度梯度的影响，实验室公开岩石孔隙率、渗透系数和同位素分析方法。这次研究提供测井原始材料与实地采集记录。"
	for _, method := range []string{"exact_retry"} {
		t.Run(method, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			h.db = h.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Error)})
			h.service.db = h.db
			root := t.TempDir()
			h.service.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(h.db))
			h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), h.service.resourceCatalog)
			h.service.task = redistest.NewClient(t, h.db)
			existing, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", URL: "https://example.org/ui02/existing", Title: "已有待补全文章", Content: body, Acquisition: "extracted", Publisher: "星河观测通讯"})
			require.NoError(t, err)
			require.Equal(t, "accepted", existing.Status)

			beforeFiles := transactionStoredFiles(t, root)
			var beforeResources int64
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&beforeResources).Error)
			beforeJobs := transactionJobs(t, h.db)
			request := types.IngestRequest{Library: "official", URL: "https://example.org/ui02/existing", Title: "已有待补全文章", Content: body, Acquisition: "extracted", Publisher: "星河观测通讯"}
			result, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{request}})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			t.Logf("batch result after duplicate lookup: %+v", result.Items[0])
			require.Equal(t, "exists", result.Items[0].Status)
			require.Equal(t, existing.EntryID, result.Items[0].EntryID)
			require.Equal(t, beforeFiles, transactionStoredFiles(t, root), "duplicate delivery must remove only its unused physical files")
			var activeResources int64
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&activeResources).Error)
			require.Equal(t, beforeResources, activeResources, "unused duplicate resources cannot remain active")
			assertSamePendingOperations(t, beforeJobs, transactionJobs(t, h.db))
		})
	}
}
