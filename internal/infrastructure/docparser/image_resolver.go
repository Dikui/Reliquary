package docparser

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"

	"path/filepath"
	"regexp"
	"strings"

	"reliquary/internal/searchutil"

	"github.com/google/uuid"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// StoredImage describes an image that has been saved to storage.
type StoredImage struct {
	OriginalRef string // reference in the original markdown
	ServingURL  string // stored image URL (for example local://1/exports/image.png)
	MimeType    string
}

// ImageResolver reads images from a parser ReadResult (inline bytes only)
// and saves them via FileService, replacing markdown references with unified URLs.
type ImageResolver struct {
	// SpaceID for storage path namespacing
	SpaceID uint64
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
	spaceID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	markdown := UnwrapLinkedImages(result.MarkdownContent)
	md2, imgDataURIs, resolveErr := r.ResolveDataURIImages(ctx, markdown, fileSvc, spaceID)
	markdown = md2
	images = append(images, imgDataURIs...)

	md3, imgHTML, htmlErr := r.ResolveHTMLDataURIImages(ctx, markdown, fileSvc, spaceID)
	markdown = md3
	images = append(images, imgHTML...)

	md4, imgBare, bareErr := r.ResolveBareBase64Content(ctx, markdown, fileSvc, spaceID)
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
		stored, saveErr := r.saveReferencedImage(ctx, fileSvc, spaceID, refPath, refMap, savedRefs)
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

	md5, imgRelativeHTML, relativeErr := r.ResolveRelativeHTMLImages(ctx, markdown, fileSvc, spaceID, refMap, savedRefs)
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
	spaceID uint64,
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
	servingURL, saveErr := fileSvc.SaveBytes(ctx, ref.ImageData, spaceID, fileName, false)
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
	spaceID uint64,
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
		if len(data) > maxInlineImageSize {
			err = errors.Join(err, fmt.Errorf("HTML data URI image exceeds %d bytes", maxInlineImageSize))
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, spaceID, fileName, false)
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
	spaceID uint64,
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

		stored, saveErr := r.saveReferencedImage(ctx, fileSvc, spaceID, src, refMap, savedRefs)
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
	spaceID uint64,
) (updatedMarkdown string, images []StoredImage, err error) {
	md, imgs1, dataErr := r.resolveBareDataURIs(ctx, markdown, fileSvc, spaceID)
	markdown = md
	images = append(images, imgs1...)

	md2, imgs2, base64Err := r.resolveBareBase64Prefix(ctx, markdown, fileSvc, spaceID)
	markdown = md2
	images = append(images, imgs2...)

	return markdown, images, errors.Join(dataErr, base64Err)
}

func (r *ImageResolver) resolveBareDataURIs(
	ctx context.Context,
	markdown string,
	fileSvc interfaces.FileService,
	spaceID uint64,
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
		if len(data) > maxInlineImageSize {
			err = errors.Join(err, fmt.Errorf("bare data URI image exceeds %d bytes", maxInlineImageSize))
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, spaceID, fileName, false)
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
	spaceID uint64,
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
		if len(data) > maxInlineImageSize {
			err = errors.Join(err, fmt.Errorf("bare base64 image exceeds %d bytes", maxInlineImageSize))
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
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, spaceID, fileName, false)
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
	// maxInlineImageSize bounds decoded image bytes.
	maxInlineImageSize = 16 * 1024 * 1024 // 16 MB; current Qwen originals exceed 10 MB.
	// maxInlineImages bounds embedded image decoding.
	maxInlineImages = 30
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
	spaceID uint64,
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
		if len(data) > maxInlineImageSize {
			err = errors.Join(err, fmt.Errorf("data URI image exceeds %d bytes", maxInlineImageSize))
			continue
		}
		ext := extFromMime(mimeType)
		if ext == "" {
			ext = ".png"
		}
		fileName := uuid.New().String() + ext
		servingURL, saveErr := fileSvc.SaveBytes(ctx, data, spaceID, fileName, false)
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

// HasUnstoredImages reports content image references that still point outside
// managed storage after resolution. It lets intake callers reject incomplete
// content while legacy callers can retain their configured original URLs.
func HasUnstoredImages(markdown string) bool {
	for _, ref := range MarkdownImageReferences(markdown) {
		target := ref.URL
		if target != "" && !isProviderScheme(target) && !strings.HasPrefix(target, "/files/") {
			return true
		}
	}
	for _, m := range imgHTMLSrc.FindAllStringSubmatchIndex(markdown, -1) {
		group := searchutil.HTMLImageSrcURLGroup * 2
		if m[group] >= 0 {
			target := htmlAttrSrc(markdown[m[group]:m[group+1]])
			if !isProviderScheme(target) && !strings.HasPrefix(target, "/files/") {
				return true
			}
		}
	}
	return false
}
