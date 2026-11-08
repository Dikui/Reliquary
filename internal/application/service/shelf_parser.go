package service

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

const shelfParserMaxOutput = 20 << 20

// Queue insertion shares the version/changeset transaction, including preview
// savepoints. Only confirmed operations become visible to existing workers.
func (s *EntityService) enqueueShelfParse(ctx context.Context, version *types.ShelfDocumentVersion, library *types.Library) error {
	parser, ok := library.ShelfConfig.Parser(version.ParserKey)
	if !ok || parser.Type == "none" || version.AttachmentID == nil {
		return nil
	}
	if version.ParseAttempt == 0 {
		version.ParseAttempt = 1
	}
	version.ParseStatus = "pending"
	version.ParseError = ""
	if err := s.db.WithContext(ctx).Save(version).Error; err != nil {
		return err
	}
	payload, _ := json.Marshal(types.ShelfParsePayload{SpaceID: version.SpaceID, VersionID: version.ID, JobID: version.ID, Attempt: version.ParseAttempt})
	_, err := queue.NewClient(s.db).EnqueueTx(ctx, s.db, queue.NewTask(types.TypeShelfParse, payload), queue.Queue(types.QueueDefault), queue.MaxRetry(3), queue.Timeout(time.Hour))
	return err
}

// HandleShelfParse publishes text only if this is still the requested attempt.
// A repeated delivery after a committed publication is an idempotent no-op.
func (s *EntityService) HandleShelfParse(ctx context.Context, task *queue.Task) error {
	var p types.ShelfParsePayload
	if err := types.UnmarshalSpacePayload(task.Payload(), &p); err != nil || p.SpaceID != types.SingleSpaceID || p.VersionID == "" || p.JobID != p.VersionID || p.Attempt < 1 {
		return fmt.Errorf("invalid shelf parsing payload: %w", queue.SkipRetry)
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, p.SpaceID)
	ctx = types.WithBackgroundTask(ctx)
	var version types.ShelfDocumentVersion
	err := s.db.WithContext(ctx).Where("id=? AND space_id=? AND parse_attempt=?", p.VersionID, p.SpaceID, p.Attempt).First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if version.ParseStatus == "completed" || version.ParseStatus == "not_parsed" {
		return nil
	}
	library, err := managedLibrary(s.db, p.SpaceID, version.LibraryID)
	if err != nil {
		return err
	}
	parser, ok := library.ShelfConfig.Parser(version.ParserKey)
	if !ok || parser.Type == "none" {
		return fmt.Errorf("selected parser is no longer available")
	}
	if err = s.db.WithContext(ctx).Model(&version).Where("parse_attempt=?", p.Attempt).Update("parse_status", "parsing").Error; err != nil {
		return err
	}
	attachment, original, err := s.OpenShelfDocumentVersion(ctx, version.FileID, version.ID)
	if err != nil {
		return err
	}
	defer original.Close()
	var markdown string
	switch parser.Type {
	case "http":
		markdown, err = s.parseShelfHTTP(ctx, library.ID, parser, attachment, original)
	case "vision":
		markdown, err = s.parseShelfVision(ctx, parser, io.LimitReader(original, attachment.Size+1))
	}
	if err != nil {
		return err
	}
	return s.publishShelfText(ctx, &version, library, markdown)
}

type shelfParsedPage struct {
	Page     int    `json:"page"`
	Markdown string `json:"markdown"`
}

func shelfPagesMarkdown(pages []shelfParsedPage) (string, error) {
	if len(pages) == 0 {
		return "", fmt.Errorf("parser returned no pages")
	}
	var body strings.Builder
	last := 0
	for _, page := range pages {
		if page.Page <= last || strings.TrimSpace(page.Markdown) == "" || !utf8.ValidString(page.Markdown) {
			return "", fmt.Errorf("parser pages require increasing positive page numbers and nonblank UTF-8 Markdown")
		}
		fmt.Fprintf(&body, "<!-- page: %d -->\n\n%s\n\n", page.Page, page.Markdown)
		if body.Len() > shelfParserMaxOutput {
			return "", fmt.Errorf("parser output is too large")
		}
		last = page.Page
	}
	if utf8.RuneCountInString(body.String()) > types.MaxIngestContentRunes {
		return "", fmt.Errorf("parser output exceeds the body character limit")
	}
	return body.String(), nil
}

func (s *EntityService) parseShelfHTTP(ctx context.Context, libraryID string, parser types.ShelfParser, attachment *types.EntityAttachment, original io.Reader) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(parser.TimeoutSeconds)*time.Second)
	defer cancel()
	// The database semaphore is shared across all instances of EntityService.
	release, err := s.acquireShelfParser(ctx, libraryID, parser)
	if err != nil {
		return "", err
	}
	defer release()
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		part, err := form.CreateFormFile("file", attachment.Name)
		if err == nil {
			_, err = io.Copy(part, original)
		}
		if err == nil {
			err = form.Close()
		}
		writer.CloseWithError(err)
	}()
	defer reader.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parser.URL, reader)
	if err != nil {
		return "", fmt.Errorf("invalid parser request")
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	if parser.Authorization != "" {
		request.Header.Set("Authorization", parser.Authorization)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("parser HTTP request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("parser HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, shelfParserMaxOutput+1))
	if err != nil || len(data) > shelfParserMaxOutput {
		return "", fmt.Errorf("invalid or oversized parser response")
	}
	var result struct {
		Pages []shelfParsedPage `json:"pages"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return "", fmt.Errorf("parser response must contain pages with page and markdown")
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("parser response must be one complete JSON object")
	}
	return shelfPagesMarkdown(result.Pages)
}

// Advisory lock slots bound configured service concurrency across processes.
// A dedicated connection owns one slot only for the duration of the HTTP call.
func (s *EntityService) acquireShelfParser(ctx context.Context, libraryID string, p types.ShelfParser) (func(), error) {
	db, err := s.db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	for {
		for slot := 0; slot < p.Concurrency; slot++ {
			key := fmt.Sprintf("shelf-parser:%s:%s:%d", libraryID, p.Key, slot)
			var acquired bool
			if err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", key).Scan(&acquired); err != nil {
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				conn.Close()
				return nil, err
			}
			if acquired {
				return func() {
					cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if _, err := conn.ExecContext(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", key); err != nil {
						conn.Raw(func(any) error { return driver.ErrBadConn })
					}
					conn.Close()
				}, nil
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			conn.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *EntityService) publishShelfText(ctx context.Context, version *types.ShelfDocumentVersion, library *types.Library, markdown string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, version.SpaceID); err != nil {
			return err
		}
		var current types.ShelfDocumentVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id=?", version.ID).Error; err != nil {
			return err
		}
		if current.ParseAttempt != version.ParseAttempt || current.ParseStatus == "completed" || current.ParseStatus == "not_parsed" {
			return nil
		}
		var file types.Entity
		if err := tx.First(&file, "id=? AND space_id=?", current.FileID, current.SpaceID).Error; err != nil {
			return err
		}
		id := shelfTextID(current.ID)
		path := vaultEntryPath(library.Directory, file.CanonicalName, id)
		now := time.Now()
		entry := types.Entry{ID: id, SpaceID: current.SpaceID, LibraryID: current.LibraryID, Type: types.EntryTypeManual, Title: file.CanonicalName, SourceKind: "article", Source: path, VaultPath: path, VaultBody: markdown, FileName: file.CanonicalName + ".md", FileType: "md", Acquisition: "verbatim", EnableStatus: "enabled", ParseStatus: types.ParseStatusPending, SummaryStatus: types.SummaryStatusNone, CreatedAt: now, UpdatedAt: now}
		setMessageFingerprint(&entry, markdown)
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{
			"vault_body", "source_body_hash", "content_hash", "content_sim_hash", "content_rune_count", "parse_status", "summary_status", "structured_summary", "enable_status", "updated_at", "metadata", "deleted_at",
		})}).Create(&entry).Error; err != nil {
			return err
		}
		bound := s.shelfBound(tx)
		linkCtx := types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "shelf-parser"})
		linkCtx = context.WithValue(linkCtx, shelfWriteContextKey{}, shelfWriteContext{current.Source, current.ChangesetID})
		for _, entityID := range []string{current.FileID, documentIDValue(current.OwnerID)} {
			if entityID == "" {
				continue
			}
			if err := bound.SetEntryEntityLink(linkCtx, id, entityID, false, nil); err != nil {
				return err
			}
		}
		payload, _ := json.Marshal(types.ManualProcessPayload{SpaceID: current.SpaceID, EntryID: id, LibraryID: current.LibraryID, Content: markdown})
		if _, err := queue.NewClient(tx).EnqueueTx(ctx, tx, queue.NewTask(types.TypeManualProcess, payload), queue.Queue(types.QueueDefault), queue.MaxRetry(3)); err != nil {
			return err
		}
		return tx.Model(&current).Updates(map[string]any{"entry_id": id, "parse_status": "completed", "parse_error": ""}).Error
	})
}
