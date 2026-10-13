package service

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func sourcedMetadataPDF(text, metadata string) []byte {
	stream := fmt.Sprintf("BT /F1 12 Tf 50 700 Td (%s) Tj ET", text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< " + metadata + " >>",
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return pdf.Bytes()
}

func TestFileSourceLinkExtractionEntersModelReview(t *testing.T) {
	for _, test := range []struct {
		name, filename, body, want string
	}{
		{"PDF metadata URL", "metadata.pdf", string(sourcedMetadataPDF("Original research measurements are reproducible.", "/Title (Original paper) /URL (https://example.org/paper)")), "https://example.org/paper"},
		{"DOI", "doi.md", "# Paper\n\nOriginal research data. DOI: 10.1234/source.456", "https://doi.org/10.1234/source.456"},
		{"arXiv", "arxiv.md", "# Research\n\nOriginal research findings. arXiv: 2601.12345v2", "https://arxiv.org/abs/2601.12345v2"},
		{"canonical", "canonical.md", `<link href="https://example.org/canonical" rel="canonical"><meta property="og:url" content="https://example.org/other">` + "\n\n# Original\n\nThe source presents complete original results.", "https://example.org/canonical"},
		{"original link", "original.md", "# Original\n\n原文链接：[阅读原文](https://example.org/original)\n\nComplete original measurements.", "https://example.org/original"},
		{"og url", "og.md", `<meta content="https://example.org/og" property="og:url">` + "\n\n# Source\n\nComplete original research.", "https://example.org/og"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, _ := newVaultArticleHarness(t)
			var overrides *types.EntryProcessOverrides
			if strings.HasSuffix(test.filename, ".pdf") {
				t.Setenv("SSRF_WHITELIST", "127.0.0.1")
				utils.ResetSSRFWhitelistForTest()
				t.Cleanup(utils.ResetSSRFWhitelistForTest)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					file, _, err := r.FormFile("files")
					if err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					defer file.Close()
					data, err := io.ReadAll(file)
					if err != nil || string(data) != test.body {
						http.Error(w, "unexpected PDF", http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"results":{"metadata":{"md_content":"# Original paper\n\nOriginal research measurements are reproducible.","images":{}}}}`)
				}))
				t.Cleanup(server.Close)
				overrides = &types.EntryProcessOverrides{ParserEngineRules: []types.ParserEngineRule{{FileTypes: []string{"pdf"}, Engine: "mineru"}}, ParserEngineOverrides: map[string]string{"mineru_endpoint": server.URL}}
			}
			k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, test.filename, test.body), nil, nil, "", types.ChannelWeb, overrides)
			require.NoError(t, err)
			h.drain(t)
			k = h.article(t, k.ID)
			require.Equal(t, types.ParseStatusCompleted, k.ParseStatus, k.ErrorMessage)
			intake, err := k.IntakeMetadata()
			require.NoError(t, err)
			require.Equal(t, test.want, intake.SourceInfo.OriginalURL)
			require.Equal(t, "from_file", intake.SourceInfo.OriginalURLSource)
			require.Equal(t, []string{"intake_review", "article_processing"}, h.processor.stages)
		})
	}
}

func TestOriginalSourceExtractionDoesNotTreatOrdinaryLinksAsSource(t *testing.T) {
	require.Empty(t, extractOriginalSourceLink("# Notes\n\nA related page is https://example.org/reference.\n\n[Reference](https://example.org/another)", nil, "md", nil))
	require.Empty(t, extractOriginalSourceLink(`<link rel="canonical" href="file:///etc/passwd">`, nil, "md", nil))
	for _, ordinary := range []string{"Resources: https://example.org/tool", "This resource is https://example.org/tool", "Source reference https://example.org/tool", "A paper discusses 2601.12345."} {
		require.Empty(t, extractOriginalSourceLink(ordinary, nil, "md", nil), ordinary)
	}
	require.Equal(t, "https://example.org/paper(title)", extractOriginalSourceLink("Source: https://example.org/paper(title)", nil, "md", nil))
	require.Equal(t, "https://example.org/paper(title)", extractOriginalSourceLink("原文链接：[文章](https://example.org/paper(title))", nil, "md", nil))
	require.Equal(t, "https://arxiv.org/abs/2601.12345v2", extractOriginalSourceLink("A paper.", nil, "md", map[string]string{"arxiv": "2601.12345v2"}))
	require.Equal(t, "https://arxiv.org/abs/cs/9901001", extractOriginalSourceLink("arXiv:cs/9901001", nil, "md", nil))
	require.Equal(t, "https://doi.org/10.1234/example", extractOriginalSourceLink("(DOI: 10.1234/example)", nil, "md", nil))
	utf16URL := []byte{0xfe, 0xff}
	for _, b := range []byte("https://example.org/hex-url") {
		utf16URL = append(utf16URL, 0, b)
	}
	pdf := sourcedMetadataPDF("An original.", "/Subject <"+hex.EncodeToString(utf16URL)+">")
	require.Equal(t, "https://example.org/hex-url", extractOriginalSourceLink("An original.", pdf, "pdf", nil))
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, err := writer.Write([]byte(`<dc:source>https://example.org/xmp-source</dc:source>`))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	metadata := []byte("%PDF-1.7\n1 0 obj\n<< /Type /Metadata /Subtype /XML /Filter /FlateDecode >>\nstream\n")
	metadata = append(metadata, compressed.Bytes()...)
	metadata = append(metadata, []byte("\nendstream\nendobj")...)
	require.Equal(t, "https://example.org/xmp-source", extractOriginalSourceLink("An original.", metadata, "pdf", nil))
	confirmation, err := json.Marshal(types.SourceInfo{Confirmed: true})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(confirmation))
}

func TestSupplementOriginalURLApprovalProcessesAfterModelReview(t *testing.T) {
	h, _ := newVaultArticleHarness(t)
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "supplemented.md", "# Original research\n\nA complete new paper measures performance and reproducibility."), nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "supplement_url", types.SourceInfo{OriginalURL: "file:///private/file"})
	require.Error(t, err)
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "supplement_url", types.SourceInfo{OriginalURL: "https://example.org/research"})
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, k.ParseStatus)
	require.Equal(t, []string{"intake_review"}, h.processor.stages)
	h.drain(t)
	k = h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus)
	require.Equal(t, []string{"intake_review", "article_processing"}, h.processor.stages)
	require.False(t, k.SourceUnverified)
}

func TestMissingOriginalURLSkipsModelsAndSupplementRechecks(t *testing.T) {
	h, _ := newVaultArticleHarness(t)
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "no-link.md", "# No source\n\nOriginal results with observations and measurements."), nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "缺少原始链接", intake.Reason)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Empty(t, h.processor.stages)
	require.Empty(t, k.VaultPath)
	h.processor.reject = true
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "supplement_url", types.SourceInfo{OriginalURL: "https://example.org/confirmed-original"})
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Equal(t, []string{"intake_review"}, h.processor.stages)
	require.Len(t, h.queue.tasks, 1, "a rejected supplemented source must not enqueue processing")
	h.processor.reject = false
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "retry", types.SourceInfo{})
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "review_supplement_url", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
}

func TestAdmitWithoutSourceForcesFullAndRecordsUserConfirmation(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	body := "# Unlinked instructions\n\nsourceunverifiedfixture steps remain retrievable in full despite the selected tutorial type."
	k, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, &legacyManualFixturePayload{Title: "Unlinked instructions", Content: body, Status: types.ManualEntryStatusPublish}, types.ChannelWeb)
	require.NoError(t, err)
	h.drain(t)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "admit_unverified", types.SourceInfo{})
	require.ErrorContains(t, err, "confirmation")
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "admit_unverified", types.SourceInfo{Confirmed: true, OriginalURL: "https://example.org/contradictory-source"})
	require.Error(t, err)
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "admit_unverified", types.SourceInfo{Confirmed: true})
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	require.True(t, k.SourceUnverified)
	require.Equal(t, types.ProcessingModeFull, k.ProcessingMode)
	require.Equal(t, types.ContentTypeTutorial, k.ContentType)
	require.Equal(t, []string{"article_processing"}, h.processor.stages)
	_, header, storedBody := readVaultArticle(t, vault, k)
	require.Contains(t, string(header), "url: null")
	require.Contains(t, string(storedBody), "sourceunverifiedfixture")
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, LibraryIDs: []string{h.library.ID}, Query: "sourceunverifiedfixture", TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "review_admit_unverified", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
}

func TestConvertMissingSourceToMineProcessesPreservesAttachmentsAndIndexes(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	body := "# My note\n\nconverttominefixture personal ideas contain complete observations and supporting evidence.\n\n![chart](" + articleImageDataURI(t, 42) + ")"
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "本人笔记.md", body), nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "convert_to_mine", types.SourceInfo{})
	require.ErrorContains(t, err, "confirmation")
	converted, err := h.service.ReviewEntryIntake(h.ctx, k.ID, "convert_to_mine", types.SourceInfo{Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, k.ID, converted.ID)
	require.Equal(t, "mine", converted.SourceKind)
	require.Equal(t, types.ParseStatusPending, converted.ParseStatus)
	require.Empty(t, converted.VaultPath)
	h.drain(t)
	mine := h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, mine.ParseStatus, mine.ErrorMessage)
	require.Equal(t, "mine/本人笔记-"+k.ID[:8]+".md", mine.VaultPath)
	require.Equal(t, types.ProcessingModeFull, mine.ProcessingMode)
	require.Equal(t, "event", mine.ContentType)
	require.NotNil(t, mine.StructuredSummary)
	require.False(t, mine.SourceUnverified)
	storedIntake, err := mine.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", storedIntake.Decision)
	content, err := os.ReadFile(filepath.Join(vault.root, mine.VaultPath))
	require.NoError(t, err)
	header, savedBody, err := ParseVaultArticle(content)
	require.NoError(t, err)
	require.Equal(t, "mine", header.SourceKind)
	require.Contains(t, string(savedBody), "converttominefixture")
	require.Contains(t, string(savedBody), "../attachments/"+k.ID+"/image-")
	require.NotContains(t, string(savedBody), "local://")
	attachments, err := os.ReadDir(filepath.Join(vault.root, "attachments", k.ID))
	require.NoError(t, err)
	require.Len(t, attachments, 2)
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, LibraryIDs: []string{h.library.ID}, EntryIDs: []string{mine.ID}, Query: "converttominefixture", TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.NotContains(t, h.processor.stages, "intake_review")
	require.Contains(t, h.processor.stages, "article_processing")
	logs, err := NewOperationLogService(h.db).List(h.ctx, "review_convert_to_mine", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
}

func TestConvertToMineAuditFailurePreservesSourceWithoutPublishedCopies(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	body := "# Retained original\n\nrollbackminefixture complete notes with supporting image.\n\n![chart](" + articleImageDataURI(t, 45) + ")"
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "保留来源.md", body), nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	files := h.service.fileSvc.(*articleProcessingFiles)
	before := map[string][]byte{}
	for path, content := range files.saved {
		before[path] = bytes.Clone(content)
	}
	require.NoError(t, h.db.Exec(`CREATE FUNCTION fail_mine_intake_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation_type='review_convert_to_mine' THEN RAISE EXCEPTION 'conversion audit unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_mine_intake_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION fail_mine_intake_audit()`).Error)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "convert_to_mine", types.SourceInfo{Confirmed: true})
	require.ErrorContains(t, err, "conversion audit unavailable")
	retained := h.article(t, k.ID)
	require.Equal(t, "article", retained.SourceKind)
	require.Equal(t, types.ParseStatusRejected, retained.ParseStatus)
	retainedIntake, err := retained.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, intake.OriginalPath, retainedIntake.OriginalPath)
	require.Equal(t, before, files.saved)
	entries, err := os.ReadDir(filepath.Join(vault.root, "mine"))
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = os.Stat(filepath.Join(vault.root, "attachments", k.ID))
	require.True(t, os.IsNotExist(err))
	logs, err := NewOperationLogService(h.db).List(h.ctx, "review_convert_to_mine", 1, 100)
	require.NoError(t, err)
	require.Zero(t, logs.Total)
}

func TestIngestModelRejectReturnsPendingReviewWithReason(t *testing.T) {
	h, vault := newIngestHarness(t)
	h.processor.reject = true
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Acquisition: "verbatim", URL: "https://example.org/intake-rejected", Publisher: "Acme", Content: "A complete original selected by an external agent.", AgentSummary: "The collector says this passes review."})
	require.NoError(t, err)
	require.Equal(t, "pending_review", result.Status)
	require.Equal(t, "Fixture sourced article", result.Reason)
	require.Empty(t, h.queue.tasks)
	k := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Empty(t, k.VaultPath)
	entries, err := os.ReadDir(filepath.Join(vault.root, "articles"))
	require.NoError(t, err)
	require.Empty(t, entries)
	require.Equal(t, []string{"intake_review"}, h.processor.stages)
}
