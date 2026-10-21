package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"reliquary/internal/application/service/file"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	"gorm.io/gorm"
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
			return nil, fmt.Errorf("image data must be base64")
		}
		payload = parts[1]
	}
	// Reject before allocating the decoded buffer.
	if base64.StdEncoding.DecodedLen(len(payload)) > types.MaxIngestInlineAssetBytes+2 {
		return nil, fmt.Errorf("inline image exceeds the 10 MiB item limit")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode image data: %w", err)
	}
	if len(data) > types.MaxIngestInlineAssetBytes {
		return nil, fmt.Errorf("inline image exceeds the 10 MiB item limit")
	}
	return data, nil
}

func ingestInlineSize(images []types.IngestImage) int64 {
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
	for _, image := range images {
		if image.Data != "" {
			total += size(image.Data)
		}
	}
	return total
}

func (s *entryService) storeIngestMaterials(ctx context.Context, library *types.Library, article *types.Entry, request *types.IngestRequest) (string, string, error) {
	copy := *request
	if validIngestURL(strings.TrimSpace(copy.Content)) {
		copy.Content = ""
	}
	intake, _ := article.IntakeMetadata()
	files := s.resolveFileService(ctx, library)
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
	refs, stored, failures := s.resolveIngestImages(ctx, imageInputs, files, article.SpaceID)
	if len(failures) > 0 {
		return "", "", werrors.NewInternalServerError("provided image file could not be stored: " + failures[0].Reason)
	}
	for _, image := range stored {
		intake.Images = append(intake.Images, image.ServingURL)
		if err := rememberIngestStoredFile(ctx, files, image.ServingURL); err != nil {
			return "", "", err
		}
	}
	body := assembleIngestBody(&copy, refs)
	if utf8.RuneCountInString(strings.TrimSpace(body)) > types.MaxIngestContentRunes {
		return "", "", werrors.NewBadRequestError("assembled content exceeds 5000000 Unicode code points")
	}
	canonical := assembleIngestBody(&copy, canonicalRefs)
	for i, ref := range refs {
		source := ingestImageSource(copy.Images[i])
		if source != "" && imageInputs[i].Data != "" {
			body = replaceIngestImageTargets(body, map[string]string{source: ref})
			canonical = replaceIngestImageTargets(canonical, map[string]string{source: canonicalRefs[i]})
		}
	}
	article.VaultBody = body
	intake.OriginalPath = ""
	if err := article.SetIntakeMetadata(intake); err != nil {
		return "", "", err
	}
	return body, canonical, nil
}

func (s *entryService) referenceIngestMaterials(ctx context.Context, article *types.Entry, request *types.IngestRequest) error {
	ids := []string{}
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
	contentUpdates []*vaultArticleContentUpdate
	stored         []ingestStoredFile
	vaultFiles     []ingestVaultFile
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
	for i := len(c.contentUpdates) - 1; i >= 0; i-- {
		err = errors.Join(err, c.contentUpdates[i].rollback())
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
	for _, change := range c.contentUpdates {
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
