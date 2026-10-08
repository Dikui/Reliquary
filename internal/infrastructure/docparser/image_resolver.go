package docparser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"reliquary/internal/searchutil"
	secutils "reliquary/internal/utils"

	"github.com/google/uuid"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const (
	// minImageDimension is the minimum width/height in pixels; images smaller
	// than this on either axis are treated as icons and filtered out.
	minImageDimension = 64
	// minImageBytes is the minimum file size in bytes; very small images are
	// almost certainly icons or decorative elements.
	minImageBytes = 512 // 512 bytes
)

// isIconImage returns true if the image data looks like a small icon or
// decorative element that should be filtered out. It checks pixel dimensions
// when decodable, and falls back to raw byte size otherwise.
func isIconImage(data []byte) bool {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// Cannot decode dimensions — fall back to size-only heuristic.
		return len(data) < minImageBytes
	}
	if cfg.Width < minImageDimension && cfg.Height < minImageDimension {
		return true
	}
	return false
}

// StoredImage describes an image that has been saved to storage.
type StoredImage struct {
	OriginalRef string // reference in the original markdown
	ServingURL  string // stored image URL (for example local://1/exports/image.png)
	MimeType    string
}

// ImageResolver reads images from a parser ReadResult (inline bytes only)
// and saves them via FileService, replacing markdown references with unified URLs.
type ImageResolver struct {
	// TenantID for storage path namespacing
	TenantID uint64
}

// NewImageResolver creates a resolver.
func NewImageResolver() *ImageResolver {
	return &ImageResolver{}
}

// ResolveAndStore reads images from the convert result, persists them via fileSvc,
// and replaces markdown references with provider:// URLs.
// It returns the updated markdown and a list of stored images.
func (r *ImageResolver) ResolveAndStore(
	ctx context.Context,
	result *types.ReadResult,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	markdown := UnwrapLinkedImages(result.MarkdownContent)
	md2, imgDataURIs, resolveErr := r.ResolveDataURIImages(ctx, markdown, fileSvc, tenantID)
	markdown = md2
	images = append(images, imgDataURIs...)

	md3, imgHTML, htmlErr := r.ResolveHTMLDataURIImages(ctx, markdown, fileSvc, tenantID)
	markdown = md3
	images = append(images, imgHTML...)

	md4, imgBare, bareErr := r.ResolveBareBase64Content(ctx, markdown, fileSvc, tenantID)
	markdown = md4
	images = append(images, imgBare...)

	if len(result.ImageRefs) == 0 {
		return markdown, images, errors.Join(resolveErr, htmlErr, bareErr)
	}

	// Build a map of original_ref -> image ref for fast lookup
	refMap := make(map[string]types.ImageRef)
	for _, ref := range result.ImageRefs {
		refMap[ref.OriginalRef] = ref
	}
	savedRefs := make(map[string]StoredImage)

	matches := scanMarkdownImageTargets(markdown)

	// Process in reverse order to preserve positions when replacing
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		rawTarget := markdown[match.TargetStart:match.TargetEnd]
		refPath, pathStart, pathEnd, ok := splitMarkdownImageTarget(rawTarget, refMap)
		if !ok {
			continue
		}

		// Skip already-resolved URLs (http/https, unified /files/, or provider:// scheme)
		if strings.HasPrefix(refPath, "http://") || strings.HasPrefix(refPath, "https://") ||
			isProviderScheme(refPath) {
			continue
		}

		// Find inline image bytes from the result
		stored, saveErr := r.saveReferencedImage(ctx, fileSvc, tenantID, refPath, refMap, savedRefs)
		if saveErr != nil {
			resolveErr = errors.Join(resolveErr, saveErr)
			continue
		}
		images = appendStoredImage(images, stored)

		// Replace in markdown
		absolutePathStart := match.TargetStart + pathStart
		absolutePathEnd := match.TargetStart + pathEnd
		markdown = markdown[:absolutePathStart] + stored.ServingURL + markdown[absolutePathEnd:]
	}

	md5, imgRelativeHTML, relativeErr := r.ResolveRelativeHTMLImages(ctx, markdown, fileSvc, tenantID, refMap, savedRefs)
	markdown = md5
	images = append(images, imgRelativeHTML...)

	return markdown, images, errors.Join(resolveErr, htmlErr, bareErr, relativeErr)
}

func appendStoredImage(images []StoredImage, stored StoredImage) []StoredImage {
	for _, existing := range images {
		if existing.OriginalRef == stored.OriginalRef && existing.ServingURL == stored.ServingURL {
			return images
		}
	}
	return append(images, stored)
}

func (r *ImageResolver) saveReferencedImage(
	ctx context.Context,
	fileSvc interfaces.FileService,
	tenantID uint64,
	refPath string,
	refMap map[string]types.ImageRef,
	savedRefs map[string]StoredImage,
) (StoredImage, error) {
	if stored, ok := savedRefs[refPath]; ok {
		return stored, nil
	}

	ref, found := refMap[refPath]
	if !found || len(ref.ImageData) == 0 {
		return StoredImage{}, fmt.Errorf("image %q has no extracted bytes", refPath)
	}

	// Reuse a previously saved upload when the same source image (identified by
	// ref.Filename) has already been persisted under a different markdown ref
	// path (e.g. "images/foo.png" vs "./images/foo.png"). This avoids writing
	// the same bytes to object storage multiple times.
	if ref.Filename != "" {
		if cached, ok := savedRefs["__filename__:"+ref.Filename]; ok {
			stored := StoredImage{
				OriginalRef: refPath,
				ServingURL:  cached.ServingURL,
				MimeType:    cached.MimeType,
			}
			savedRefs[refPath] = stored
			return stored, nil
		}
	}

	ext := extFromMime(ref.MimeType)
	if ext == "" {
		ext = filepath.Ext(ref.Filename)
	}
	if ext == "" {
		ext = ".png"
	}

	fileName := uuid.New().String() + ext
	servingURL, saveErr := fileSvc.SaveBytes(ctx, ref.ImageData, tenantID, fileName, false)
	if saveErr != nil {
		return StoredImage{}, fmt.Errorf("save image %q: %w", refPath, saveErr)
	}

	stored := StoredImage{
		OriginalRef: refPath,
		ServingURL:  servingURL,
		MimeType:    ref.MimeType,
	}
	savedRefs[refPath] = stored
	if ref.Filename != "" {
		savedRefs["__filename__:"+ref.Filename] = stored
	}
	return stored, nil
}

func extFromMime(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ""
	}
}

// isProviderScheme recognizes managed resource handles and storage paths.
func isProviderScheme(p string) bool {
	_, resource := types.ParseResourcePath(p)
	return resource || types.ParseProviderScheme(p) != ""
}

// isWhitelistedImageHost checks if the image URL's host is in the whitelist.
// Whitelisted hosts are trusted (e.g. internal MinerU service) — images are
// still downloaded for validation and OCR/caption analysis, but not uploaded
// to object storage. The markdown keeps the original URL.
// Configure via IMAGE_HOST_KEEP_URL env var (comma-separated hosts).
func isWhitelistedImageHost(rawURL string) bool {
	whitelist := strings.TrimSpace(os.Getenv("IMAGE_HOST_KEEP_URL"))
	if whitelist == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	hostname := strings.ToLower(u.Hostname())
	for _, h := range strings.Split(whitelist, ",") {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		// Exact host match (includes port) or hostname match (any port)
		if host == h || hostname == h {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helper functions for base64 image handling
// ---------------------------------------------------------------------------

// cleanBase64Payload removes whitespace characters from a base64 payload string.
func cleanBase64Payload(payload string) string {
	payload = strings.ReplaceAll(payload, "\n", "")
	payload = strings.ReplaceAll(payload, "\r", "")
	payload = strings.ReplaceAll(payload, "\t", "")
	payload = strings.ReplaceAll(payload, " ", "")
	return payload
}

// decodeBase64Flexible tries standard, raw, URL-safe, and raw-URL-safe base64 decodings.
func decodeBase64Flexible(payload string) ([]byte, error) {
	if data, err := base64.StdEncoding.DecodeString(payload); err == nil {
		return data, nil
	}
	if data, err := base64.RawStdEncoding.DecodeString(payload); err == nil {
		return data, nil
	}
	if data, err := base64.URLEncoding.DecodeString(payload); err == nil {
		return data, nil
	}
	return base64.RawURLEncoding.DecodeString(payload)
}

// sniffImageMime detects the MIME type by examining the magic bytes of image data.
func sniffImageMime(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return "image/png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return "image/gif"
	}
	if len(data) >= 12 &&
		data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}
	if data[0] == 'B' && data[1] == 'M' {
		return "image/bmp"
	}
	return ""
}

// ---------------------------------------------------------------------------
// HTML <img> tag data URI resolution
// ---------------------------------------------------------------------------

// imgHTMLDataURI matches HTML <img> tags with inline data:image/*;base64,... in the src attribute.
var imgHTMLDataURI = regexp.MustCompile(
	`(?i)<img\s[^>]*?src\s*=\s*["'](data:image/[^;]+;base64,[^"']+)["'][^>]*?/?\s*>`,
)

// imgHTMLSrc matches an HTML <img> tag carrying a quoted src attribute. It does
// not care what the src points at — callers select the references they handle by
// inspecting the scheme, which is how the relative and the remote paths divide
// the same tags between them.
//
// Shared with the search layer so that an image stored here can always be
// matched back to the tag it came from. See searchutil.HTMLImageSrcRegex for
// the submatch layout and the known limits.
var imgHTMLSrc = searchutil.HTMLImageSrcRegex

// ResolveHTMLDataURIImages finds <img src="data:image/*;base64,..."> tags in markdown,
// decodes the images, stores them via fileSvc, and replaces each tag with a markdown
// image reference using the storage URL.
func (r *ImageResolver) ResolveHTMLDataURIImages(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	matches := imgHTMLDataURI.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	processed := 0
	for i := len(matches) - 1; i >= 0; i-- {
		if processed >= maxInlineImages {
			err = errors.Join(err, fmt.Errorf("HTML data URI image limit of %d reached", maxInlineImages))
			break
		}
		m := matches[i]
		dataURI := markdown[m[2]:m[3]]
		mimeType, payload, ok := parseImageDataURI(dataURI)
		if !ok {
			continue
		}
		payload = cleanBase64Payload(payload)
		if payload == "" {
			continue
		}
		data, decErr := decodeBase64Flexible(payload)
		if decErr != nil {
			err = errors.Join(err, fmt.Errorf("decode HTML data URI image: %w", decErr))
			continue
		}
		if len(data) > maxRemoteImageSize {
			err = errors.Join(err, fmt.Errorf("HTML data URI image exceeds %d bytes", maxRemoteImageSize))
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, tenantID, fileName, false)
		if saveErr != nil {
			err = errors.Join(err, fmt.Errorf("save HTML data URI image: %w", saveErr))
			continue
		}
		images = append(images, StoredImage{
			OriginalRef: "html-img-data-uri",
			ServingURL:  servingURL,
			MimeType:    mimeType,
		})
		markdown = markdown[:m[0]] + fmt.Sprintf("![image](%s)", servingURL) + markdown[m[1]:]
		processed++
	}
	return markdown, images, err
}

// ResolveRelativeHTMLImages finds HTML <img> tags whose src points at a
// relative document image reference, stores the corresponding bytes via
// fileSvc, and replaces only the src attribute value with the storage URL.
func (r *ImageResolver) ResolveRelativeHTMLImages(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
	refMap map[string]types.ImageRef,
	savedRefs map[string]StoredImage,
) (updatedMarkdown string, images []StoredImage, err error) {
	matches := imgHTMLSrc.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		src := strings.TrimSpace(markdown[m[4]:m[5]])
		if src == "" || strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") ||
			isProviderScheme(src) || strings.HasPrefix(strings.ToLower(src), "data:image/") {
			continue
		}

		stored, saveErr := r.saveReferencedImage(ctx, fileSvc, tenantID, src, refMap, savedRefs)
		if saveErr != nil {
			err = errors.Join(err, saveErr)
			continue
		}
		images = appendStoredImage(images, stored)
		markdown = markdown[:m[4]] + stored.ServingURL + markdown[m[5]:]
	}

	return markdown, images, err
}

// ---------------------------------------------------------------------------
// Bare base64/data URI resolution (catch-all)
// ---------------------------------------------------------------------------

// bareDataURIPattern matches standalone data:image/*;base64,... strings.
var bareDataURIPattern = regexp.MustCompile(
	`(?i)data:image/([^;\s]+);base64,([A-Za-z0-9+/=]{100,})`,
)

// bareBase64CommaPrefixed matches base64,DATA patterns (partial data URIs missing the mime prefix).
var bareBase64CommaPrefixed = regexp.MustCompile(
	`base64,([A-Za-z0-9+/=]{200,})`,
)

// ResolveBareBase64Content finds remaining bare data URIs and base64 image content
// in the markdown text, decodes and stores them, and replaces with image references.
// This acts as a catch-all after the standard markdown and HTML resolvers.
func (r *ImageResolver) ResolveBareBase64Content(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	md, imgs1, dataErr := r.resolveBareDataURIs(ctx, markdown, fileSvc, tenantID)
	markdown = md
	images = append(images, imgs1...)

	md2, imgs2, base64Err := r.resolveBareBase64Prefix(ctx, markdown, fileSvc, tenantID)
	markdown = md2
	images = append(images, imgs2...)

	return markdown, images, errors.Join(dataErr, base64Err)
}

func (r *ImageResolver) resolveBareDataURIs(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (string, []StoredImage, error) {
	matches := bareDataURIPattern.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	var images []StoredImage
	var err error
	processed := 0
	for i := len(matches) - 1; i >= 0; i-- {
		if processed >= maxInlineImages {
			err = errors.Join(err, fmt.Errorf("bare data URI image limit of %d reached", maxInlineImages))
			break
		}
		m := matches[i]
		// Check context: skip HTML src attributes, but handle broken markdown refs
		insideWrapper := false
		if m[0] > 0 {
			prev := markdown[m[0]-1]
			if prev == '"' || prev == '\'' {
				continue // inside HTML attribute — already handled by ResolveHTMLDataURIImages
			}
			if prev == '(' {
				insideWrapper = true // likely inside a broken ![...](...) ref
			}
		}
		mimeSubtype := strings.ToLower(markdown[m[2]:m[3]])
		payload := markdown[m[4]:m[5]]
		mimeType := "image/" + mimeSubtype

		payload = cleanBase64Payload(payload)
		if payload == "" {
			continue
		}
		data, decErr := decodeBase64Flexible(payload)
		if decErr != nil {
			err = errors.Join(err, fmt.Errorf("decode bare data URI image: %w", decErr))
			continue
		}
		if len(data) > maxRemoteImageSize {
			err = errors.Join(err, fmt.Errorf("bare data URI image exceeds %d bytes", maxRemoteImageSize))
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, tenantID, fileName, false)
		if saveErr != nil {
			err = errors.Join(err, fmt.Errorf("save bare data URI image: %w", saveErr))
			continue
		}
		images = append(images, StoredImage{
			OriginalRef: "bare-data-uri",
			ServingURL:  servingURL,
			MimeType:    mimeType,
		})
		if insideWrapper {
			// Inside a broken markdown ref like ![weird]alt](data:...) — replace data URI only
			markdown = markdown[:m[0]] + servingURL + markdown[m[1]:]
		} else {
			markdown = markdown[:m[0]] + fmt.Sprintf("![image](%s)", servingURL) + markdown[m[1]:]
		}
		processed++
	}
	return markdown, images, err
}

func (r *ImageResolver) resolveBareBase64Prefix(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (string, []StoredImage, error) {
	matches := bareBase64CommaPrefixed.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	var images []StoredImage
	var err error
	processed := 0
	for i := len(matches) - 1; i >= 0; i-- {
		if processed >= maxInlineImages {
			err = errors.Join(err, fmt.Errorf("bare base64 image limit of %d reached", maxInlineImages))
			break
		}
		m := matches[i]
		// Skip if preceded by ';' — this is part of a data URI handled above
		if m[0] > 0 && markdown[m[0]-1] == ';' {
			continue
		}
		payload := markdown[m[2]:m[3]]
		payload = cleanBase64Payload(payload)
		if payload == "" {
			continue
		}
		data, decErr := decodeBase64Flexible(payload)
		if decErr != nil {
			err = errors.Join(err, fmt.Errorf("decode bare base64 image: %w", decErr))
			continue
		}
		if len(data) > maxRemoteImageSize {
			err = errors.Join(err, fmt.Errorf("bare base64 image exceeds %d bytes", maxRemoteImageSize))
			continue
		}
		mimeType := sniffImageMime(data)
		if mimeType == "" {
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, tenantID, fileName, false)
		if saveErr != nil {
			err = errors.Join(err, fmt.Errorf("save bare base64 image: %w", saveErr))
			continue
		}
		images = append(images, StoredImage{
			OriginalRef: "bare-base64",
			ServingURL:  servingURL,
			MimeType:    mimeType,
		})
		markdown = markdown[:m[0]] + fmt.Sprintf("![image](%s)", servingURL) + markdown[m[1]:]
		processed++
	}
	return markdown, images, err
}

// ---------------------------------------------------------------------------
// Remote image resolution (for manual / web-clipped markdown content)
// ---------------------------------------------------------------------------

const (
	// maxRemoteImageSize is the maximum allowed size for a single remote image download.
	maxRemoteImageSize = 16 * 1024 * 1024 // 16 MB; current Qwen originals exceed 10 MB.
	// maxInlineImages bounds embedded image decoding; remote images use request timeouts.
	maxInlineImages = 30
	// remoteImageFetchTimeout is the per-image HTTP request timeout.
	remoteImageFetchTimeout = 60 * time.Second
)

// reLinkedImage matches the nested [![alt](img_url)](link_url) pattern where
// an image is wrapped inside a Markdown link. We unwrap it to just ![alt](img_url)
// so that downstream image-processing regexes only have to handle the flat form.
// The URL groups support one level of balanced parentheses.
var reLinkedImage = regexp.MustCompile(
	`\[!\[([^\]]*)\]\(([^()\s]*(?:\([^)]*\)[^()\s]*)*)\)\]` + // [![alt](img_url)]
		`\([^()\s]*(?:\([^)]*\)[^()\s]*)*\)`, // (link_url) — captured but discarded
)

// UnwrapLinkedImages replaces all [![alt](img_url)](link_url) occurrences in
// the markdown with just ![alt](img_url), stripping the outer link wrapper.
// This should be called before any image-extraction regex so that only the
// flat ![alt](url) form needs to be handled.
func UnwrapLinkedImages(markdown string) string {
	return reLinkedImage.ReplaceAllString(markdown, "![$1]($2)")
}

// imgMarkdownPattern matches Markdown image syntax: ![alt](url).
// The alt-text group uses .*? (non-greedy) to allow literal ] in alt text.
// The URL group supports one level of balanced parentheses so that URLs
// like https://example.com/item_(abc)/123 are captured in full.
var imgMarkdownPattern = regexp.MustCompile(`!\[(.*?)\]\(([^()\s]*(?:\([^)]*\)[^()\s]*)*)\)`)

// imgMarkdownDataURI matches markdown images whose URL is a data:image/*;base64,...
// payload. (?i) applies to the whole parenthesized data URI.
// The alt-text group uses .*? (non-greedy) to allow literal ] inside alt text
// (e.g. file paths like ![C:\img]name.png](data:...)).
var imgMarkdownDataURI = regexp.MustCompile(
	`!\[(.*?)\]\((?i:(data:image/[^;]+;base64,\s*[^)]+))\)`,
)

// parseImageDataURI splits a data URI into image MIME type and base64 payload.
func parseImageDataURI(dataURI string) (mimeType string, b64Payload string, ok bool) {
	const sep = ";base64,"
	idx := strings.Index(strings.ToLower(dataURI), sep)
	if idx < 0 {
		return "", "", false
	}
	meta := strings.TrimSpace(dataURI[:idx])
	const prefix = "data:image/"
	if len(meta) < len(prefix) || !strings.EqualFold(meta[:len(prefix)], prefix) {
		return "", "", false
	}
	sub := strings.TrimSpace(meta[len(prefix):])
	mimeType = "image/" + strings.ToLower(sub)
	b64Payload = strings.TrimSpace(dataURI[idx+len(sep):])
	if b64Payload == "" {
		return "", "", false
	}
	return mimeType, b64Payload, true
}

// ResolveDataURIImages finds embedded data:image/*;base64 images in markdown,
// decodes them, stores via fileSvc, and replaces each reference with the returned
// provider URL (same limits as remote images: count and decoded size).
func (r *ImageResolver) ResolveDataURIImages(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	markdown = UnwrapLinkedImages(markdown)
	matches := imgMarkdownDataURI.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	processed := 0
	for i := len(matches) - 1; i >= 0; i-- {
		if processed >= maxInlineImages {
			err = errors.Join(err, fmt.Errorf("data URI image limit of %d reached", maxInlineImages))
			break
		}
		m := matches[i]
		if len(m) < 6 {
			continue
		}
		dataURI := markdown[m[4]:m[5]]
		mimeType, payload, ok := parseImageDataURI(dataURI)
		if !ok {
			continue
		}
		payload = cleanBase64Payload(payload)
		if payload == "" {
			continue
		}
		data, decErr := decodeBase64Flexible(payload)
		if decErr != nil {
			err = errors.Join(err, fmt.Errorf("decode data URI image: %w", decErr))
			continue
		}
		if len(data) > maxRemoteImageSize {
			err = errors.Join(err, fmt.Errorf("data URI image exceeds %d bytes", maxRemoteImageSize))
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, tenantID, fileName, false)
		if saveErr != nil {
			err = errors.Join(err, fmt.Errorf("save data URI image: %w", saveErr))
			continue
		}
		images = append(images, StoredImage{
			OriginalRef: dataURI,
			ServingURL:  servingURL,
			MimeType:    mimeType,
		})
		markdown = markdown[:m[4]] + servingURL + markdown[m[5]:]
		processed++
	}
	return markdown, images, err
}

// remoteImageResult describes one image that was fetched successfully.
type remoteImageResult struct {
	// ServingURL is empty when KeepOriginalURL is set. The caller fills it in
	// with the normalized request URL — NOT the raw document bytes. Later stages
	// both search the document for this string and fetch it, so a padded or
	// entity-encoded value satisfies neither.
	ServingURL string
	MimeType   string
	// KeepOriginalURL marks a whitelisted host. Its bytes are downloaded so that
	// OCR/caption analysis can run, but it is not uploaded: the image keeps being
	// served from its original host, reached through the normalized URL.
	KeepOriginalURL bool
}

// fetchAndStoreRemoteImage applies the SSRF policy, downloads the image,
// rejects icons and uploads the bytes to storage.
//
// Both the Markdown and the HTML scan go through here so that the SSRF check,
// the icon filter and the whitelist behaviour cannot drift apart between the
// two syntaxes.
func fetchAndStoreRemoteImage(
	ctx context.Context,
	client *http.Client,
	fileSvc interfaces.FileService,
	tenantID uint64,
	imgURL string,
) (*remoteImageResult, error) {
	whitelisted := isWhitelistedImageHost(imgURL)

	if !whitelisted {
		if err := secutils.ValidateURLForSSRF(imgURL); err != nil {
			return nil, fmt.Errorf("blocked by SSRF policy: %w", err)
		}
	}

	data, mimeType, err := downloadImage(ctx, client, imgURL)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	if whitelisted {
		return &remoteImageResult{MimeType: mimeType, KeepOriginalURL: true}, nil
	}

	ext := extFromMime(mimeType)
	if ext == "" {
		ext = extFromURLPath(imgURL)
	}
	if ext == "" {
		ext = ".png" // safe default
	}
	servingURL, err := fileSvc.SaveBytes(ctx, data, tenantID, uuid.New().String()+ext, false)
	if err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	return &remoteImageResult{ServingURL: servingURL, MimeType: mimeType}, nil
}

// isRemoteHTTPURL reports whether raw is an absolute http(s) URL.
//
// The comparison is deliberately byte-exact. Downstream fetchers compare the
// scheme the same way, so anything accepted here has to be spelled the way they
// expect; per-syntax normalization belongs in the SrcOf of the scan that needs
// it, not in this predicate.
func isRemoteHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

// remotePassSpec describes one scan of a document: which references it matches,
// where the URL sits inside a match, and how to turn the raw document bytes into
// the URL to request.
type remotePassSpec struct {
	Pattern  *regexp.Regexp
	URLGroup int
	Syntax   string
	SrcOf    func(string) string
}

// resolveRemoteImagePass rewrites every remote http(s) reference matched by
// spec.Pattern, replacing the span of its URL capture group with the URL the
// image will be served from: the storage URL, or the normalized original for a
// whitelisted host. Matches are walked in reverse so the spans still to be
// processed stay valid after each replacement.
func resolveRemoteImagePass(
	ctx context.Context,
	markdown string,
	spec remotePassSpec,
	client func() *http.Client,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (string, []StoredImage, error) {
	matches := spec.Pattern.FindAllStringSubmatchIndex(markdown, -1)
	if len(matches) == 0 {
		return markdown, nil, nil
	}

	var (
		images []StoredImage
		err    error
	)

	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		start, end := m[2*spec.URLGroup], m[2*spec.URLGroup+1]
		if start < 0 {
			continue
		}
		imgURL := spec.SrcOf(markdown[start:end])
		if !isRemoteHTTPURL(imgURL) {
			continue
		}

		if ctx.Err() != nil {
			err = errors.Join(err, fmt.Errorf("resolve %s remote images: %w", spec.Syntax, ctx.Err()))
			break
		}

		res, fetchErr := fetchAndStoreRemoteImage(ctx, client(), fileSvc, tenantID, imgURL)
		if fetchErr != nil {
			err = errors.Join(err, fmt.Errorf("resolve %s image %q: %w", spec.Syntax, imgURL, fetchErr))
			continue
		}
		// A whitelisted host is served from where it already lives, so the
		// document keeps pointing there. The span is still rewritten with the
		// normalized URL: later stages both locate the image by searching the
		// document for its ServingURL and fetch that same string, so the two have
		// to agree, and an entity-encoded or padded attribute value satisfies
		// neither.
		servingURL := res.ServingURL
		if res.KeepOriginalURL {
			servingURL = imgURL
		}

		images = append(images, StoredImage{
			OriginalRef: imgURL,
			ServingURL:  servingURL,
			MimeType:    res.MimeType,
		})
		markdown = markdown[:start] + servingURL + markdown[end:]
	}

	return markdown, images, err
}

// htmlAttrSrc normalizes an HTML src attribute value into the URL to request.
//
// Three things are HTML-specific. An attribute value may be padded, so it is
// trimmed — on both sides of the decode, because an entity can itself expand to
// whitespace. It may carry entities, and `&amp;` is the correct spelling of a
// query separator, so it is unescaped. And the scheme may be written in any
// case, while every fetcher downstream compares it byte-for-byte, so it is
// lowercased.
//
// Markdown targets get none of this: they are handed through unchanged so that
// existing documents resolve exactly the references they resolve today.
func htmlAttrSrc(raw string) string {
	src := strings.TrimSpace(html.UnescapeString(strings.TrimSpace(raw)))
	if i := strings.Index(src, "://"); i > 0 {
		src = strings.ToLower(src[:i]) + src[i:]
	}
	return src
}

// markdownSrc hands a Markdown image target through untouched. Markdown has no
// attribute quoting or entity encoding to undo, and leaving it alone is what
// keeps existing documents resolving exactly what they resolve today.
func markdownSrc(raw string) string { return raw }

// ResolveRemoteImages downloads remote http(s) images into storage and points
// the document at the stored copies. Both Markdown image syntax and HTML <img>
// tags with a quoted src are covered.
func (r *ImageResolver) ResolveRemoteImages(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	tenantID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	markdown = UnwrapLinkedImages(markdown)

	// The client owns a dedicated transport, so it is built only once a scan
	// actually has something to fetch.
	var httpClient *http.Client
	client := func() *http.Client {
		if httpClient == nil {
			httpClient = secutils.NewSSRFSafeHTTPClient(secutils.SSRFSafeHTTPClientConfig{
				Timeout:      remoteImageFetchTimeout,
				MaxRedirects: 5,
			})
		}
		return httpClient
	}

	// Scan Markdown and HTML references separately so each syntax keeps its
	// existing URL normalization rules.
	markdown, mdImages, markdownErr := resolveRemoteImagePass(ctx, markdown, remotePassSpec{
		Pattern: imgMarkdownPattern,
		// Group 2 of imgMarkdownPattern is the target; group 1 is the alt text.
		URLGroup: 2,
		Syntax:   "markdown",
		SrcOf:    markdownSrc,
	}, client, fileSvc, tenantID)

	markdown, htmlImages, htmlErr := resolveRemoteImagePass(ctx, markdown, remotePassSpec{
		Pattern:  imgHTMLSrc,
		URLGroup: searchutil.HTMLImageSrcURLGroup,
		Syntax:   "html",
		SrcOf:    htmlAttrSrc,
	}, client, fileSvc, tenantID)

	images = append(images, mdImages...)
	images = append(images, htmlImages...)
	return markdown, images, errors.Join(markdownErr, htmlErr)
}

// HasUnstoredImages reports content image references that still point outside
// managed storage after resolution. It lets intake callers reject incomplete
// content while legacy callers can retain their configured original URLs.
func HasUnstoredImages(markdown string) bool {
	for _, match := range scanMarkdownImageTargets(markdown) {
		target := strings.TrimSpace(markdown[match.TargetStart:match.TargetEnd])
		if strings.HasPrefix(target, "<") {
			if end := strings.IndexByte(target, '>'); end > 0 {
				target = target[1:end]
			}
		}
		if target != "" && !isProviderScheme(target) && !strings.HasPrefix(target, "/files/") {
			return true
		}
	}
	for _, spec := range []remotePassSpec{
		{Pattern: imgMarkdownPattern, URLGroup: 2, SrcOf: markdownSrc},
		{Pattern: imgHTMLSrc, URLGroup: searchutil.HTMLImageSrcURLGroup, SrcOf: htmlAttrSrc},
	} {
		for _, m := range spec.Pattern.FindAllStringSubmatchIndex(markdown, -1) {
			if start := m[2*spec.URLGroup]; start >= 0 {
				target := spec.SrcOf(markdown[start:m[2*spec.URLGroup+1]])
				if !isProviderScheme(target) && !strings.HasPrefix(target, "/files/") {
					return true
				}
			}
		}
	}
	return false
}

// downloadImage fetches an image from remoteURL using the provided SSRF-safe
// client. It validates Content-Type and enforces maxRemoteImageSize.
func downloadImage(ctx context.Context, client *http.Client, remoteURL string) (data []byte, mimeType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	// Some CDNs require a browser-like User-Agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Reliquary/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	// Determine MIME type from Content-Type header.
	ct := resp.Header.Get("Content-Type")
	mimeType, _, _ = mime.ParseMediaType(ct)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Only allow image content types (or octet-stream which we sniff later).
	if !strings.HasPrefix(mimeType, "image/") && mimeType != "application/octet-stream" {
		return nil, "", fmt.Errorf("non-image content type: %s", mimeType)
	}

	// Read body with size limit.
	limited := io.LimitReader(resp.Body, maxRemoteImageSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxRemoteImageSize {
		return nil, "", fmt.Errorf("image exceeds %d bytes limit", maxRemoteImageSize)
	}

	// If MIME was octet-stream, sniff the real type from body.
	if mimeType == "application/octet-stream" {
		detected := http.DetectContentType(body)
		if strings.HasPrefix(detected, "image/") {
			mimeType = detected
		} else {
			return nil, "", fmt.Errorf("downloaded data is not an image (sniffed: %s)", detected)
		}
	}

	return body, mimeType, nil
}

// extFromURLPath extracts the image file extension from the URL path segment.
func extFromURLPath(rawURL string) string {
	p := path.Ext(path.Base(rawURL))
	switch strings.ToLower(p) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return strings.ToLower(p)
	default:
		return ""
	}
}
