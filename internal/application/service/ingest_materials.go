package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/application/service/file"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type ingestIdentityError struct{ *werrors.AppError }

func (e *ingestIdentityError) Unwrap() error { return e.AppError }

type verifiedIngestIdentityKey struct{}
type verifiedIngestIdentity struct {
	spaceID   uint64
	principal types.Principal
	keyID     uint64
	keyName   string
}

// AuthorizeIngestRequest rechecks credentials before the MCP transport writes
// its response. The returned context shares that result within this request.
func (s *entryService) AuthorizeIngestRequest(ctx context.Context) (context.Context, error) {
	ctx, id, name, err := s.lookupIngestIdentity(ctx)
	if err != nil {
		return ctx, err
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	principal, _ := types.PrincipalFromContext(ctx)
	return context.WithValue(ctx, verifiedIngestIdentityKey{}, verifiedIngestIdentity{spaceID, principal, id, name}), nil
}

// Recheck durable credentials for each operation unless its HTTP request has
// already done so. Reading public key metadata avoids loading the secret.
func (s *entryService) ingestIdentity(ctx context.Context) (context.Context, uint64, string, error) {
	if verified, ok := ctx.Value(verifiedIngestIdentityKey{}).(verifiedIngestIdentity); ok {
		spaceID, _ := types.SpaceIDFromContext(ctx)
		principal, _ := types.PrincipalFromContext(ctx)
		if verified.spaceID == spaceID && verified.principal == principal {
			return ctx, verified.keyID, verified.keyName, nil
		}
	}
	return s.lookupIngestIdentity(ctx)
}

func (s *entryService) lookupIngestIdentity(ctx context.Context) (context.Context, uint64, string, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok || spaceID == 0 {
		return ctx, 0, "", &ingestIdentityError{werrors.NewUnauthorizedError("personal space is required")}
	}
	principal, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return ctx, 0, "", &ingestIdentityError{werrors.NewUnauthorizedError("authenticated collector required")}
	}
	if s.db == nil {
		return ctx, 0, "", &ingestIdentityError{werrors.NewServiceUnavailableError("Authentication is temporarily unavailable")}
	}
	if principal.Type != types.PrincipalAPIKey {
		return ctx, 0, "", nil
	}
	id, err := strconv.ParseUint(principal.ID, 10, 64)
	if err != nil || id == 0 {
		return ctx, 0, "", &ingestIdentityError{werrors.NewUnauthorizedError("API Key is invalid or revoked")}
	}
	var key struct {
		ID                   uint64
		Name                 string
		RevokedAt, ExpiresAt *time.Time
	}
	err = s.db.WithContext(ctx).Table("api_keys").Select("id,name,revoked_at,expires_at").Where("id=? AND space_id=?", id, spaceID).Take(&key).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ctx, 0, "", &ingestIdentityError{werrors.NewServiceUnavailableError("Authentication is temporarily unavailable")}
	}
	if err != nil || key.RevokedAt != nil || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
		return ctx, 0, "", &ingestIdentityError{werrors.NewUnauthorizedError("API Key is invalid or revoked")}
	}
	principal.Name = key.Name
	return types.WithPrincipal(ctx, principal), id, key.Name, nil
}

func decodeIngestInline(raw string) ([]byte, error) {
	payload := strings.TrimSpace(raw)
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return nil, fmt.Errorf("file data must be base64")
		}
		payload = parts[1]
	}
	// Reject before allocating the decoded buffer.
	if base64.StdEncoding.DecodedLen(len(payload)) > types.MaxIngestInlineAssetBytes+2 {
		return nil, fmt.Errorf("inline file exceeds the 10 MiB item limit")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode file data: %w", err)
	}
	if len(data) > types.MaxIngestInlineAssetBytes {
		return nil, fmt.Errorf("inline file exceeds the 10 MiB item limit")
	}
	return data, nil
}

func ingestInlineSize(file *types.IngestFile, images []types.IngestImage) int64 {
	size := func(raw string) int64 {
		if strings.HasPrefix(raw, "data:") {
			if parts := strings.SplitN(raw, ",", 2); len(parts) == 2 {
				raw = parts[1]
			}
		}
		// The exact size excludes padding and transport line breaks.
		raw = strings.NewReplacer("\r", "", "\n", "").Replace(raw)
		return int64(base64.StdEncoding.DecodedLen(len(raw))) - int64(len(raw)-len(strings.TrimRight(raw, "=")))
	}
	var total int64
	if file != nil && file.Data != "" {
		total += size(file.Data)
	}
	for _, image := range images {
		if image.Data != "" {
			total += size(image.Data)
		}
	}
	return total
}

func hasIngestMissing(items []types.IngestMissing, kind, source string) bool {
	for _, item := range items {
		if item.Type == kind && (source == "" || item.SourceURL == source) && strings.TrimSpace(item.Reason) != "" {
			return true
		}
	}
	return false
}

func stampIngestMissing(items []types.IngestMissing, id uint64, name string) []types.IngestMissing {
	result := make([]types.IngestMissing, 0, len(items))
	for _, item := range items {
		item.APIKeyID, item.APIKeyName, item.SubmittedAt = id, name, time.Now()
		exists := false
		for _, previous := range result {
			if previous.Type == item.Type && previous.SourceURL == item.SourceURL {
				exists = true
				break
			}
		}
		if !exists {
			result = append(result, item)
		}
	}
	return result
}

// Content must be actual supplied material, rather than the link that locates it.
func hasIngestBody(request *types.IngestRequest, parsed string) bool {
	content := strings.TrimSpace(utils.CleanMarkdown(request.Content))
	if content != "" && !validIngestURL(content) {
		return true
	}
	return request.File != nil && (request.File.AssetID != "" || request.File.Data != "") && strings.TrimSpace(parsed) != ""
}

func (s *entryService) readIngestFile(ctx context.Context, file *types.IngestFile) (*types.IngestAsset, []byte, error) {
	if file.AssetID != "" {
		return s.loadIngestAsset(ctx, file.AssetID)
	}
	data, err := decodeIngestInline(file.Data)
	if err != nil {
		return nil, nil, werrors.NewBadRequestError(err.Error())
	}
	name := strings.TrimSpace(file.Name)
	if name == "" {
		switch http.DetectContentType(data) {
		case "application/pdf":
			name = "original.pdf"
		case "image/png":
			name = "original.png"
		case "image/jpeg":
			name = "original.jpg"
		case "image/gif":
			name = "original.gif"
		case "text/plain; charset=utf-8":
			name = "original.txt"
		default:
			return nil, nil, werrors.NewBadRequestError("base64 file requires name including its extension")
		}
	}

	if err := validateImportFileType(getFileType(name)); err != nil {
		return nil, nil, err
	}
	return &types.IngestAsset{Name: name, Type: getFileType(name), Size: int64(len(data))}, data, nil
}

// The optional reader is an injected parsing dependency for integration tests.
// Production always selects an in-process parser with no URL or OCR fallback.
func (s *entryService) parseIngestDocument(ctx context.Context, asset *types.IngestAsset, data []byte) (*types.ReadResult, error) {
	reader := s.ingestDocReader
	if reader == nil {
		reader = &docparser.SimpleFormatReader{}
		if !docparser.IsSimpleFormat(asset.Type) {
			reader = docparser.NewAnydocReader(nil, nil)
		}
	}
	return reader.Read(ctx, &types.ReadRequest{FileContent: data, FileName: asset.Name, FileType: asset.Type})
}

func (s *entryService) storeIngestMaterials(ctx context.Context, library *types.Library, article *types.Entry, request *types.IngestRequest) (string, string, error) {
	copy := *request
	if validIngestURL(strings.TrimSpace(copy.Content)) {
		copy.Content = ""
	}
	intake, _ := article.IntakeMetadata()
	files := s.resolveFileService(ctx, library)
	missing := stampIngestMissing(request.Missing, article.LastIngestAPIKeyID, article.LastIngestAPIKeyName)
	canonicalRefs := make([]string, len(copy.Images))
	imageInputs := append([]types.IngestImage(nil), copy.Images...)
	for i := range imageInputs {
		if imageInputs[i].AssetID != "" {
			asset, data, err := s.loadIngestAsset(ctx, imageInputs[i].AssetID)
			if err != nil {
				return "", "", err
			}
			if !docparser.IsImageFormat(asset.Type) || !strings.HasPrefix(http.DetectContentType(data), "image/") {
				return "", "", werrors.NewBadRequestError("image asset is not an image")
			}
			imageInputs[i].Data = base64.StdEncoding.EncodeToString(data)
		}
		canonicalRefs[i] = ingestCanonicalImageReference(imageInputs[i])
	}
	if request.File != nil && (request.File.AssetID != "" || request.File.Data != "") {
		asset, data, err := s.readIngestFile(ctx, request.File)
		if err != nil {
			return "", "", err
		}
		path, err := files.SaveBytes(ctx, data, article.SpaceID, asset.Name, false)
		if err != nil {
			return "", "", err
		}
		if err := rememberIngestStoredFile(ctx, files, path); err != nil {
			return "", "", err
		}
		article.FilePath, article.FileName, article.FileType, article.FileSize = path, asset.Name, asset.Type, int64(len(data))
		attachments := []types.IngestAttachment{{AssetID: asset.ID, Name: asset.Name, Type: asset.Type, Path: path, Size: int64(len(data)), SourceURL: request.File.SourceURL}}
		if err := article.SetIngestAttachments(attachments); err != nil {
			return "", "", err
		}
		if strings.TrimSpace(copy.Content) == "" {
			parsed, err := s.parseIngestDocument(ctx, asset, data)
			if err != nil {
				return "", "", werrors.NewBadRequestError("attachment could not be parsed locally: " + err.Error())
			}
			if parsed == nil {
				return "", "", werrors.NewBadRequestError("attachment parser returned no content")
			}
			parsedBody, parsedImages, err := docparser.NewImageResolver().ResolveAndStore(ctx, parsed, files, article.SpaceID)
			if err != nil {
				return "", "", err
			}
			copy.Content = parsedBody
			for _, image := range parsedImages {
				intake.Images = append(intake.Images, image.ServingURL)
				if err := rememberIngestStoredFile(ctx, files, image.ServingURL); err != nil {
					return "", "", err
				}
			}
			if err := article.SetIntakeMetadata(intake); err != nil {
				return "", "", err
			}
		}
		if err := extractIntakeOriginalLink(article, copy.Content, data, asset.Type, nil); err != nil {
			return "", "", err
		}
		intake, _ = article.IntakeMetadata()
	}
	refs, stored, failures := s.resolveIngestImages(ctx, imageInputs, files, article.SpaceID)
	if len(failures) > 0 {
		return "", "", werrors.NewBadRequestError("provided image file could not be stored: " + failures[0].Reason)
	}
	for _, image := range stored {
		intake.Images = append(intake.Images, image.ServingURL)
		if err := rememberIngestStoredFile(ctx, files, image.ServingURL); err != nil {
			return "", "", err
		}
	}
	body := assembleIngestBody(&copy, refs)
	canonical := assembleIngestBody(&copy, canonicalRefs)
	for i, ref := range refs {
		source := ingestImageSource(copy.Images[i])
		if source != "" && imageInputs[i].Data != "" {
			body = replaceIngestImageTargets(body, map[string]string{source: ref})
			canonical = replaceIngestImageTargets(canonical, map[string]string{source: canonicalRefs[i]})
		}
	}
	missing = addRemoteIngestImageMissing(body, missing, article.LastIngestAPIKeyID, article.LastIngestAPIKeyName)
	if !hasIngestBody(request, body) && !hasIngestMissing(missing, "content", "") {
		missing = append(missing, types.IngestMissing{Type: "content", SourceURL: request.URL, Reason: "未提供正文", APIKeyID: article.LastIngestAPIKeyID, APIKeyName: article.LastIngestAPIKeyName, SubmittedAt: time.Now()})
	}
	article.Incomplete = len(missing) > 0
	if err := article.SetIngestMissing(missing); err != nil {
		return "", "", err
	}
	path, err := files.SaveBytes(ctx, []byte(body), article.SpaceID, article.ID+".md", false)
	if err != nil {
		return "", "", err
	}
	if err := rememberIngestStoredFile(ctx, files, path); err != nil {
		return "", "", err
	}
	intake.OriginalPath = path
	if err := article.SetIntakeMetadata(intake); err != nil {
		return "", "", err
	}
	return body, canonical, nil
}

func (s *entryService) referenceIngestMaterials(ctx context.Context, article *types.Entry, request *types.IngestRequest) error {
	ids := []string{}
	if request.File != nil && request.File.AssetID != "" {
		ids = append(ids, request.File.AssetID)
	}
	for _, image := range request.Images {
		if image.AssetID != "" {
			ids = append(ids, image.AssetID)
		}
	}
	for _, id := range ids {
		if err := s.referenceIngestAsset(ctx, id, article.ID); err != nil {
			return err
		}
	}
	return nil
}

func replaceIngestImageTargets(body string, mappings map[string]string) string {
	refs := docparser.MarkdownImageReferences(body)
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		replacement, ok := mappings[ref.URL]
		if !ok {
			continue
		}
		position := ref.Position
		for position < len(body) && strings.ContainsRune(" \t\r\n", rune(body[position])) {
			position++
		}
		if position < len(body) && body[position] == '<' {
			position++
		}
		if strings.HasPrefix(body[position:], ref.URL) {
			body = body[:position] + replacement + body[position+len(ref.URL):]
		}
	}
	matches := searchutil.HTMLImageSrcRegex.FindAllStringSubmatchIndex(body, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		group := searchutil.HTMLImageSrcURLGroup * 2
		if len(match) > group+1 && match[group] >= 0 {
			source := body[match[group]:match[group+1]]
			if replacement, ok := mappings[source]; ok {
				body = body[:match[group]] + replacement + body[match[group+1]:]
			}
		}
	}
	return body
}

func addRemoteIngestImageMissing(body string, items []types.IngestMissing, id uint64, name string) []types.IngestMissing {
	for _, ref := range articleImagesInOrder(body, nil) {
		if !validIngestURL(ref) || hasIngestMissing(items, "image", ref) {
			continue
		}
		items = append(items, types.IngestMissing{Type: "image", SourceURL: ref, Reason: "未提供图片文件", APIKeyID: id, APIKeyName: name, SubmittedAt: time.Now()})
	}
	return items
}

// This dedicated update preserves collection-owned columns across processing
// snapshots. It never attempts to resolve or fetch the scanned image targets.
func (s *entryService) recordMissingIngestImages(ctx context.Context, k *types.Entry, body string) error {
	old := k.IngestMissing()
	missing := addRemoteIngestImageMissing(body, old, k.LastIngestAPIKeyID, k.LastIngestAPIKeyName)
	if len(old) == len(missing) {
		return nil
	}
	if err := k.SetIngestMissing(missing); err != nil {
		return err
	}
	k.Incomplete = true
	return s.repo.UpdateEntryColumns(ctx, k.ID, map[string]any{"missing_materials": k.MissingMaterials, "incomplete": true, "updated_at": time.Now()})
}

func (s *entryService) readIngestBody(ctx context.Context, library *types.Library, k *types.Entry) (string, error) {
	if k.VaultPath != "" && !(k.Incomplete && k.ProcessingMode == types.ProcessingModeSummaryOnly) {
		return k.VaultBody, nil
	}
	intake, err := k.IntakeMetadata()
	if err != nil {
		return "", err
	}
	if intake == nil || intake.OriginalPath == "" {
		return "", nil
	}
	file, err := s.resolveFileServiceForPath(ctx, library, intake.OriginalPath).GetFile(ctx, intake.OriginalPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	return string(data), err
}

// The transaction wrapper restores published bytes if any later durable write
// fails, and removes newly saved unreferenced material.
type ingestFileChangesKey struct{}
type ingestStoredFile struct {
	provider interfaces.FileService
	physical string
	ref      string
}
type ingestVaultFile struct {
	article *types.Entry
	path    string
}
type ingestFileChanges struct {
	supplements []*vaultArticleSupplement
	stored      []ingestStoredFile
	vaultFiles  []ingestVaultFile
}

func rememberIngestStoredFile(ctx context.Context, files interfaces.FileService, path string) error {
	if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
		// Parser aliases can share one upload. Deleting its resource reference
		// twice would try to resolve an already deleted resource.
		for _, stored := range changes.stored {
			if stored.ref == path {
				return nil
			}
		}
		provider, physical, err := file.PhysicalFile(ctx, files, path)
		if err != nil {
			return err
		}
		changes.stored = append(changes.stored, ingestStoredFile{provider: provider, physical: physical, ref: path})
	}
	return nil
}
func (c *ingestFileChanges) rollback(v *VaultService) error {
	var err error
	for i := len(c.supplements) - 1; i >= 0; i-- {
		err = errors.Join(err, c.supplements[i].rollback())
	}
	if v != nil {
		for _, file := range c.vaultFiles {
			err = errors.Join(err, v.DeleteStoredArticleAttachment(file.article, file.path))
		}
	}
	// Only physical providers escape the transaction. Resource rows have already
	// rolled back, so resolving their handles here would use an expired catalog.
	for _, stored := range c.stored {
		if cleanupErr := stored.provider.DeleteFile(context.Background(), stored.physical); !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
	}
	return err
}
func (c *ingestFileChanges) finish() error {
	var err error
	for _, change := range c.supplements {
		err = errors.Join(err, change.finish())
	}
	return err
}

func (c *ingestFileChanges) removeUnusedStoredFiles(ctx context.Context, files interfaces.FileService) error {
	var err error
	for _, stored := range c.stored {
		if cleanupErr := files.DeleteFile(ctx, stored.ref); !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
	}
	return err
}
