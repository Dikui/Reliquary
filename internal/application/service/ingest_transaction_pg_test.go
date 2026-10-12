package service

import (
	"context"
	"encoding/base64"
	"math/bits"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

// Exercise the production resource decorator: an in-memory file driver alone
// cannot detect resource resolution through a transaction that has ended.
func TestIngestResourceDuplicateCleanupWithinTransaction(t *testing.T) {
	const body = "星河研究院地热研究团队在玄武岩山区钻取岩芯，逐层记录裂隙密度、矿物组成与热导率。钻孔测温结果揭示浅层地下水循环对温度梯度的影响，实验室公开岩石孔隙率、渗透系数和同位素分析方法。这次研究提供测井原始材料与实地采集记录。"
	for _, method := range []string{"content_hash", "simhash"} {
		t.Run(method, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			h.db = h.db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Error)})
			h.service.db = h.db
			root := t.TempDir()
			h.service.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(h.db))
			h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), h.service.resourceCatalog)
			h.service.task = taskqueue.NewClient(h.db)
			t.Setenv("RELIQUARY_DUPLICATE_SIMHASH_DISTANCE", "3")

			missing := []types.IngestMissing{{Type: "image", SourceURL: "https://example.org/ui02/materials/existing.png", Reason: "外部工具未取得原图"}}
			existing, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/ui02/existing", Title: "已有待补全文章", Content: body, Acquisition: "extracted", Publisher: "星河观测通讯", Missing: missing})
			require.NoError(t, err)
			require.Equal(t, "accepted", existing.Status)
			require.True(t, h.article(t, existing.EntryID).Incomplete)
			pending, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: "https://example.org/ui02/missing-content", Acquisition: "model_written", Missing: []types.IngestMissing{{Type: "content", Reason: "正文需要登录且未取得全文"}}})
			require.NoError(t, err)
			require.Equal(t, "pending_review", pending.Status)

			incoming := body
			if method == "simhash" {
				incoming += "。"
				originalHash, originalSimHash, _ := messageBodyFingerprint(body)
				incomingHash, incomingSimHash, _ := messageBodyFingerprint(incoming)
				require.NotEqual(t, originalHash, incomingHash)
				require.LessOrEqual(t, bits.OnesCount64(uint64(originalSimHash)^uint64(incomingSimHash)), messageSimHashThreshold())
			}
			beforeFiles := transactionStoredFiles(t, root)
			var beforeResources int64
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&beforeResources).Error)
			beforeJobs := transactionJobs(t, h.db)
			request := types.IngestRequest{URL: "https://example.org/ui02/follow-main", Title: "关注动态待补全 星河研究院新观测", Content: incoming, Acquisition: "model_written", Publisher: "星河观测通讯", Author: "页面检查编辑", PublishedAt: "2026-10-10T09:30:00+08:00", Missing: []types.IngestMissing{{Type: "image", SourceURL: "https://example.org/ui02/materials/follow-main.png", Reason: "外部工具未取得原图"}}}
			result, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{request}})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			t.Logf("batch result after duplicate lookup: %+v", result.Items[0])
			var repost types.EntryRepostSource
			require.NoError(t, h.db.Where("normalized_url=?", normalizeMessageURL(request.URL)).First(&repost).Error)
			t.Logf("persisted repost method=%s article_id=%s", repost.MatchMethod, repost.EntryID)
			require.Equal(t, "exists", result.Items[0].Status)
			require.Equal(t, existing.EntryID, result.Items[0].EntryID)
			require.Equal(t, method, repost.MatchMethod)
			require.Equal(t, beforeFiles, transactionStoredFiles(t, root), "duplicate delivery must remove only its unused physical files")
			var activeResources int64
			require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&activeResources).Error)
			require.Equal(t, beforeResources, activeResources, "unused duplicate resources cannot remain active")
			require.Equal(t, beforeJobs, transactionJobs(t, h.db), "duplicates do not schedule processing")
		})
	}
}

type ingestAliasImageReader struct {
	data []byte
}

func (r ingestAliasImageReader) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	body := strings.Repeat("The geological research team records rock composition, thermal conductivity, borehole temperatures and calibration data from independently collected samples. ", 8)
	return &types.ReadResult{MarkdownContent: body + "\n\n![first](images/chart.png)\n\n![second](./images/chart.png)", ImageRefs: []types.ImageRef{
		{OriginalRef: "images/chart.png", Filename: "chart.png", ImageData: r.data},
		{OriginalRef: "./images/chart.png", Filename: "chart.png", ImageData: r.data},
	}}, nil
}

func TestIngestResourceDuplicateParsedImageAliasesCleanedOnce(t *testing.T) {
	h, _ := newIngestHarness(t)
	root := t.TempDir()
	h.service.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(h.db))
	h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), h.service.resourceCatalog)
	h.service.task = taskqueue.NewClient(h.db)
	image, err := decodeIngestInline(articleImageDataURI(t, 91))
	require.NoError(t, err)
	h.service.ingestDocReader = ingestAliasImageReader{data: image}
	pdf := sourcedMetadataPDF("Original geological observations.", "/Title (Geological study)")
	request := &types.IngestRequest{URL: "https://example.org/alias-original", Acquisition: "extracted", Publisher: "Acme", File: &types.IngestFile{Name: "original.pdf", Data: base64.StdEncoding.EncodeToString(pdf)}}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", first.Status)
	intake, err := h.article(t, first.EntryID).IntakeMetadata()
	require.NoError(t, err)
	require.Len(t, intake.Images, 2)
	require.Equal(t, intake.Images[0], intake.Images[1], "different parser references reuse one physical image")
	beforeFiles := transactionStoredFiles(t, root)
	request.URL = "https://example.org/alias-repost"
	second, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "exists", second.Status)
	require.Equal(t, first.EntryID, second.EntryID)
	require.Equal(t, beforeFiles, transactionStoredFiles(t, root))
}

func TestIngestResourceDiscardedExtractedURLCleansUnusedMaterials(t *testing.T) {
	h, _ := newIngestHarness(t)
	root := t.TempDir()
	h.service.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(h.db))
	h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), h.service.resourceCatalog)
	h.service.task = taskqueue.NewClient(h.db)
	const source = "https://example.org/discarded-extracted-origin"
	first, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: source, Acquisition: "model_written", Missing: []types.IngestMissing{{Type: "content", Reason: "暂无正文"}}})
	require.NoError(t, err)
	require.NoError(t, h.service.DiscardIngest(h.ctx, first.EntryID, "用户不再保留"))
	beforeFiles := transactionStoredFiles(t, root)
	request := &types.IngestRequest{URL: "", Acquisition: "model_written", Content: "Source: " + source + "\n\nThe complete original record describes measurements and instrument calibration.", Images: []types.IngestImage{{Data: articleImageDataURI(t, 92)}}}
	result, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "discarded", result.Status)
	require.Equal(t, first.EntryID, result.EntryID)
	require.Equal(t, beforeFiles, transactionStoredFiles(t, root))
	var resources, entries int64
	require.NoError(t, h.db.Model(&types.StoredResource{}).Count(&resources).Error)
	require.Zero(t, resources)
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&entries).Error)
	require.Zero(t, entries)
}
