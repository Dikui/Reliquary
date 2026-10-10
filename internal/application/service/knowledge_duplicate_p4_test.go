package service

import (
	"context"
	"math/bits"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestP4MessageURLNormalization(t *testing.T) {
	for _, raw := range []string{
		"https://EXAMPLE.COM/Article/", "https://example.com/article", "https://example.com/article/?utm_source=rss&utm_campaign=daily#section",
		"https://example.com:443/ARTICLE?fbclid=one&gclid=two&mc_cid=three",
	} {
		require.Equal(t, "https://example.com/article", normalizeMessageURL(raw), raw)
	}
	require.Equal(t, "https://example.com/article?id=ABC", normalizeMessageURL("https://example.com/article/?utm_medium=social&id=ABC"))
	require.NotEqual(t, normalizeMessageURL("https://example.com/article?id=1"), normalizeMessageURL("https://example.com/article?id=2"))
	require.Empty(t, normalizeMessageURL("not a URL"))
}

func TestP4RepostNamePreservesFullWidthCharacters(t *testing.T) {
	require.Equal(t, "acme news", normalizeRepostName("  ACME\t News  "))
	require.Equal(t, "ａｃｍｅ", normalizeRepostName(" ＡＣＭＥ "))
	require.NotEqual(t, normalizeRepostName("ＡＣＭＥ"), normalizeRepostName("ACME"))
}

func TestP4BodyFingerprintAndNearDuplicateBoundary(t *testing.T) {
	body := "Acme released the Orion model with improved reasoning, fast inference, multimodal input, and a context window of one million tokens. The report includes benchmark results, evaluation methodology, pricing, and deployment requirements."
	changed := strings.Replace(body, "one million", "two million", 1)
	hash, simhash, count := messageBodyFingerprint(body)
	otherHash, otherSimhash, otherCount := messageBodyFingerprint(changed)
	require.NotEqual(t, hash, otherHash)
	distance := bits.OnesCount64(uint64(simhash) ^ uint64(otherSimhash))
	require.Positive(t, distance)
	require.LessOrEqual(t, distance, 3)
	require.GreaterOrEqual(t, count, minimumSimHashRunes)
	require.InDelta(t, count, otherCount, 10)
	whitespaceHash, _, _ := messageBodyFingerprint(" \n" + strings.ReplaceAll(body, " ", "  \n") + "\n")
	require.Equal(t, hash, whitespaceHash)
	db := pgtest.Open(t)
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", Source: "manual", ParseStatus: "completed", ContentHash: hash, ContentSimHash: simhash, ContentRuneCount: count}
	require.NoError(t, db.Create(k).Error)
	incoming := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", ContentHash: otherHash, ContentSimHash: otherSimhash, ContentRuneCount: otherCount}
	t.Setenv("RELIQUARY_DUPLICATE_SIMHASH_DISTANCE", "0")
	match, _, _, err := findMessageDuplicate(db, incoming)
	require.NoError(t, err)
	require.Nil(t, match, "changes beyond the configured bit distance are separate messages")
	t.Setenv("RELIQUARY_DUPLICATE_SIMHASH_DISTANCE", "3")
	match, method, actualDistance, err := findMessageDuplicate(db, incoming)
	require.NoError(t, err)
	require.Equal(t, k.ID, match.ID)
	require.Equal(t, "simhash", method)
	require.Equal(t, distance, actualDistance)
	_, unrelated, unrelatedCount := messageBodyFingerprint("Farmers harvested apples and pears throughout the valley. Rainfall, soil composition, irrigation, seasonal temperatures, crop yields, and market transport determined agricultural outcomes in the countryside.")
	incoming.ContentSimHash, incoming.ContentRuneCount = unrelated, unrelatedCount
	match, _, _, err = findMessageDuplicate(db, incoming)
	require.NoError(t, err)
	require.Nil(t, match)
	incoming.ContentSimHash, incoming.ContentRuneCount = simhash, 20
	match, _, _, err = findMessageDuplicate(db, incoming)
	require.NoError(t, err)
	require.Nil(t, match, "short messages do not use approximate deduplication")
}

func TestP4DuplicateIntakeReturnsCurrentMatchMethod(t *testing.T) {
	db := pgtest.Open(t)
	body := "Acme released the Orion model with improved reasoning, fast inference, multimodal input, and a context window of one million tokens. The report includes benchmark results, evaluation methodology, pricing, and deployment requirements."
	owner := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", Source: "manual", ParseStatus: "completed", EnableStatus: "enabled"}
	setMessageFingerprint(owner, body)
	require.NoError(t, db.Create(owner).Error)
	files := &createKnowledgeFileServiceStub{}
	queue := &createKnowledgeTaskEnqueuerStub{}
	svc := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db), kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb"}}, fileSvc: files, task: queue}
	for _, input := range []struct {
		body, url, method string
	}{
		{strings.Replace(body, "one million", "two million", 1), "https://news.example/near", "simhash"},
		{body, "https://news.example/exact", "content_hash"},
		{body, "https://news.example/exact?utm_source=feed", "url"},
	} {
		item, err := svc.CreateKnowledgeFromManual(newCreateKnowledgeFileContext(), "kb", &types.ManualKnowledgePayload{Title: "Reprint", Content: input.body, Status: "publish", SourceInfo: types.SourceInfo{Publisher: "News", OriginalURL: input.url}}, types.ChannelWeb)
		var duplicate *types.DuplicateKnowledgeError
		require.ErrorAs(t, err, &duplicate)
		require.Equal(t, owner.ID, item.ID)
		require.Equal(t, input.method, item.DuplicateMatchMethod)
	}
	require.Zero(t, queue.calls)
	require.Zero(t, files.saveCalls)
	detail, err := svc.GetKnowledgeByID(newCreateKnowledgeFileContext(), owner.ID)
	require.NoError(t, err)
	require.Empty(t, detail.DuplicateMatchMethod)
	require.Len(t, detail.RepostSources, 2)
}

func TestP4DuplicateIntakeDoesNotSaveOrEnqueueAndPreservesReposts(t *testing.T) {
	db := pgtest.Open(t)
	body := "Acme released its new research report with a complete description of the methodology and findings."
	source := types.SourceInfo{Publisher: "Acme", OriginalURL: "https://acme.example/report", PublishedAt: time.Now().UTC().Format(time.RFC3339)}
	owner := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", Source: "manual", ParseStatus: "completed", EnableStatus: "enabled"}
	require.NoError(t, owner.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: source}))
	setMessageFingerprint(owner, body)
	require.NoError(t, db.Create(owner).Error)
	files := &createKnowledgeFileServiceStub{}
	queue := &createKnowledgeTaskEnqueuerStub{}
	svc := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db), kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb"}}, fileSvc: files, task: queue}
	for index, input := range []types.SourceInfo{
		{Publisher: "News One", OriginalURL: "https://news-one.example/story/", PublishedAt: source.PublishedAt},
		{Publisher: "News One", OriginalURL: "https://news-one.example/story/?utm_source=rss", PublishedAt: source.PublishedAt},
		{Publisher: "News Two", OriginalURL: "https://news-two.example/story"},
	} {
		item, err := svc.CreateKnowledgeFromManual(newCreateKnowledgeFileContext(), "kb", &types.ManualKnowledgePayload{Title: "Reprint", Content: body, Status: "publish", SourceInfo: input}, types.ChannelRSS)
		var duplicate *types.DuplicateKnowledgeError
		require.ErrorAs(t, err, &duplicate)
		require.Equal(t, owner.ID, item.ID)
		expectedMethod := "content_hash"
		if index == 1 {
			expectedMethod = "url"
		}
		require.Equal(t, expectedMethod, item.DuplicateMatchMethod)
	}
	require.Zero(t, queue.calls, "there is no queued processing for duplicate articles")
	require.Zero(t, files.saveCalls, "duplicate bodies are not saved again")
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	reposts, err := loadKnowledgeRepostSources(db, 1, []string{owner.ID}, nil)
	require.NoError(t, err)
	require.Len(t, reposts, 2, "canonical URL makes repeat deliveries idempotent")
	require.Equal(t, "content_hash", reposts[0].MatchMethod)
	require.True(t, reposts[1].PublishedAtMissing)
	require.Empty(t, reposts[1].PublishedAt, "collection time is never substituted")
	detail, err := svc.GetKnowledgeByID(newCreateKnowledgeFileContext(), owner.ID)
	require.NoError(t, err)
	require.Len(t, detail.RepostSources, 2)
	require.Empty(t, detail.DuplicateMatchMethod, "the result of a previous intake is not stored on the message")
}

func TestP4ParsedDuplicateStopsBeforeEnrichment(t *testing.T) {
	db := pgtest.Open(t)
	body := "The original article describes a sourced announcement."
	owner := types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", ParseStatus: "completed", EnableStatus: "enabled"}
	setMessageFingerprint(&owner, body)
	require.NoError(t, db.Create(&owner).Error)
	incoming := types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", ParseStatus: "pending"}
	require.NoError(t, incoming.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{Publisher: "Reprint", OriginalURL: "https://news.example/copy"}}))
	require.NoError(t, db.Create(&incoming).Error)
	svc := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db)}
	svc.triggerManualProcessing(context.Background(), &types.KnowledgeBase{ID: "kb"}, &incoming, body, true)
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", incoming.ID).Count(&count).Error)
	require.Zero(t, count, "a duplicate parser result does not remain a separate message")
}

func TestP4URLDuplicatesAndConcurrentIntakeHaveOneOwner(t *testing.T) {
	db := pgtest.Open(t)
	svc := &knowledgeService{db: db}
	owner := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", ParseStatus: "completed", NormalizedURL: "https://example.com/article", CreatedAt: time.Now()}
	require.NoError(t, db.Create(owner).Error)
	incoming := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", ParseStatus: "pending"}
	require.NoError(t, incoming.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{SourceInfo: types.SourceInfo{OriginalURL: "https://EXAMPLE.COM/ARTICLE/?utm_source=one", Publisher: "News"}}))
	existing, err := svc.createMessageWithDeduplication(context.Background(), incoming, "A revised title in a duplicate URL delivery")
	require.NoError(t, err)
	require.Equal(t, owner.ID, existing.ID)
	sources, err := loadKnowledgeRepostSources(db, 1, []string{owner.ID}, nil)
	require.NoError(t, err)
	require.Len(t, sources, 1)
	require.Equal(t, "url", sources[0].MatchMethod)

	var wait sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			candidate := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "concurrent", Type: "manual", ParseStatus: "pending", CreatedAt: time.Now()}
			_, err := svc.createMessageWithDeduplication(context.Background(), candidate, "Identical content received concurrently")
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("knowledge_base_id = ?", "concurrent").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestP4SameBodyInDifferentKnowledgeBasesKeepsSeparateOwners(t *testing.T) {
	db := pgtest.Open(t)
	svc := &knowledgeService{db: db}
	body := "The same original article remains a separate source in each knowledge base."
	for _, kbID := range []string{"first-kb", "second-kb"} {
		message := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: kbID, Type: "manual", ParseStatus: "pending", CreatedAt: time.Now()}
		require.NoError(t, message.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://acme.example/report"}}))
		existing, err := svc.createMessageWithDeduplication(context.Background(), message, body)
		require.NoError(t, err)
		require.Nil(t, existing, "a knowledge base keeps its own source identity")
	}
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, db.Model(&types.KnowledgeRepostSource{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestP4ConcurrentPendingParserDuplicatesKeepOneOwner(t *testing.T) {
	db := pgtest.Open(t)
	svc := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db)}
	created := time.Now().UTC().Truncate(time.Microsecond)
	messages := []types.Knowledge{
		{ID: "pending-a", TenantID: 1, KnowledgeBaseID: "kb", Type: "file", ParseStatus: "pending", CreatedAt: created},
		{ID: "pending-b", TenantID: 1, KnowledgeBaseID: "kb", Type: "file", ParseStatus: "pending", CreatedAt: created},
	}
	for i := range messages {
		require.NoError(t, messages[i].SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{OriginalURL: "https://example.com/report", Publisher: messages[i].ID}}))
		setMessageFingerprint(&messages[i], "")
		require.NoError(t, db.Create(&messages[i]).Error)
	}
	type result struct {
		incoming *types.Knowledge
		owner    *types.Knowledge
		err      error
	}
	results := make(chan result, 2)
	for i := range messages {
		go func(message *types.Knowledge) {
			owner, err := svc.checkMessageDuplicate(context.Background(), message, "A complete original body shared by the two pending parsers.")
			results <- result{incoming: message, owner: owner, err: err}
		}(&messages[i])
	}
	// Both fingerprint transactions finish before either cleanup can delete a
	// row, reproducing the vulnerable interval between detection and deletion.
	completed := []result{<-results, <-results}
	duplicates := 0
	for _, item := range completed {
		require.NoError(t, item.err)
		if item.owner != nil {
			duplicates++
			require.Equal(t, "pending-a", item.owner.ID)
			require.Equal(t, "pending-b", item.incoming.ID)
			require.NoError(t, svc.stopDuplicateProcessing(context.Background(), item.incoming))
		}
	}
	require.Equal(t, 1, duplicates)
	var remaining []types.Knowledge
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "pending-a", remaining[0].ID)
	reposts, err := loadKnowledgeRepostSources(db, 1, []string{remaining[0].ID}, nil)
	require.NoError(t, err)
	require.Len(t, reposts, 1)
}

func TestP4FileDuplicateCleansSavedFileAndDoesNotEnqueue(t *testing.T) {
	db := pgtest.Open(t)
	owner := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Type: "manual", ParseStatus: "completed", NormalizedURL: "https://example.com/report"}
	require.NoError(t, db.Create(owner).Error)
	files := &createKnowledgeFileServiceStub{}
	queue := &createKnowledgeTaskEnqueuerStub{}
	svc := &knowledgeService{db: db, repo: repository.NewKnowledgeRepository(db), kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb"}}, fileSvc: files, task: queue}
	message, err := svc.CreateKnowledgeFromFile(newCreateKnowledgeFileContext(), "kb", newMultipartFileHeader(t, "article.txt", "The downloaded article body"), map[string]string{"original_url": "https://EXAMPLE.COM/REPORT/?utm_source=file", "publisher": "Reprint"}, nil, "", "", nil)
	var duplicate *types.DuplicateKnowledgeError
	require.ErrorAs(t, err, &duplicate)
	require.Equal(t, owner.ID, message.ID)
	require.Equal(t, 1, files.saveCalls)
	require.Equal(t, 1, files.deleteCalls, "the newly saved duplicate file is removed")
	require.Zero(t, queue.calls)
	var count int64
	require.NoError(t, db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestP4RepostMissingRedeliveryPreservesVerifiedPublication(t *testing.T) {
	db := pgtest.Open(t)
	publication := time.Now().UTC().Format(time.RFC3339)
	owner := types.Knowledge{ID: "dated-owner", TenantID: 9, KnowledgeBaseID: "kb", ParseStatus: "completed", EnableStatus: "enabled"}
	require.NoError(t, db.Create(&owner).Error)
	for _, date := range []string{publication, ""} {
		incoming := types.Knowledge{TenantID: 9, KnowledgeBaseID: "kb"}
		require.NoError(t, incoming.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{SourceInfo: types.SourceInfo{Publisher: "News", OriginalURL: "https://news.example/announcement", PublishedAt: date}}))
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return recordMessageRepost(tx, &owner, &incoming, "content_hash", 0) }))
	}
	reposts, err := loadKnowledgeRepostSources(db, 9, []string{owner.ID}, nil)
	require.NoError(t, err)
	require.Len(t, reposts, 1)
	require.Equal(t, publication, reposts[0].PublishedAt)
	require.False(t, reposts[0].PublishedAtMissing)

}
