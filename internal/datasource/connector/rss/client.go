package rss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/PuerkitoBio/goquery"
	webfetch "reliquary/internal/infrastructure/webfetch"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

const (
	// requestTimeout bounds a single feed or article fetch.
	requestTimeout = 20 * time.Second

	// maxFeedSize caps the feed body to avoid memory blowups on hostile feeds.
	maxFeedSize = 10 * 1024 * 1024 // 10 MB

	// maxArticleSize caps a single article page body.
	maxArticleSize = 5 * 1024 * 1024 // 5 MB

	// defaultUserAgent is sent on every request; some feeds reject empty UAs.
	defaultUserAgent = "Mozilla/5.0 (compatible; Reliquary-RSS/1.0; +https://weknora.weixin.qq.com)"
)

// client performs SSRF-safe HTTP fetches with optional custom headers.
type client struct {
	httpClient *http.Client
	headers    map[string]string
	adapters   *SiteAdapterRegistry
}

func newClient(headers map[string]string) *client {
	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = requestTimeout
	return &client{
		httpClient: utils.NewSSRFSafeHTTPClient(cfg),
		headers:    headers,
	}
}

// fetch retrieves rawURL with SSRF validation and size limiting. Custom auth
// headers are only attached when withAuthHeaders is true (feed fetches); article
// pages on third-party domains must not receive feed credentials.
func (c *client) fetch(ctx context.Context, rawURL string, maxSize int64, withAuthHeaders bool) ([]byte, error) {
	if err := utils.ValidateURLForSSRF(rawURL); err != nil {
		return nil, fmt.Errorf("URL rejected: %w", err)
	}
	if _, err := url.Parse(rawURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	if withAuthHeaders {
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept",
			"application/rss+xml, application/atom+xml, application/xml, text/xml, application/json, text/html;q=0.9, */*;q=0.8")
	}
	if !withAuthHeaders {
		c.adapterRegistry().configureRequest(req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}
	if int64(len(body)) > maxSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxSize)
	}
	return body, nil
}

// fetchFeed retrieves the raw bytes of a feed document.
func (c *client) fetchFeed(ctx context.Context, feedURL string) ([]byte, error) {
	return c.fetch(ctx, feedURL, maxFeedSize, true)
}

func (c *client) fetchSource(ctx context.Context, feedURL string) ([]byte, error) {
	return c.fetchFeed(ctx, c.adapterRegistry().sourceURL(feedURL))
}

// extractArticle fetches an article page and returns the readability-cleaned
// main content as HTML, plus the extracted title and source fields. Returns an
// error if the page can't be fetched or no readable content is found.
type extractedArticle struct {
	contentHTML, title, byline, siteName, publishedAt string
	failures                                          []types.SourceAdapterFailure
}

func (c *client) extractArticle(ctx context.Context, articleURL string) (contentHTML, title, byline, siteName, publishedAt string, err error) {
	article, err := c.extractArticleWithFailures(ctx, articleURL)
	return article.contentHTML, article.title, article.byline, article.siteName, article.publishedAt, err
}

func (c *client) extractArticleWithFailures(ctx context.Context, articleURL string) (extractedArticle, error) {
	body, err := c.fetch(ctx, articleURL, maxArticleSize, false)
	if err != nil {
		return extractedArticle{}, err
	}
	return c.extractArticleBody(ctx, body, articleURL)
}

// extractArticleBody separates pure page extraction from HTTP for offline regression tests.
func (c *client) extractArticleBody(ctx context.Context, body []byte, articleURL string) (extractedArticle, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return extractedArticle{}, fmt.Errorf("article HTML parse: %w", err)
	}
	content := genericArticleContent(doc)
	sourceHeading := strings.TrimSpace(content.Find("h1").First().Text())
	pageURL, err := url.Parse(articleURL)
	if err != nil {
		return extractedArticle{}, err
	}
	videoCount, err := c.preserveVideoLinks(ctx, content, pageURL)
	if err != nil {
		return extractedArticle{}, err
	}
	// Generic fallback discards unresolved components from a separate document.
	// Adapters retain the original placeholders and streamed data for recovery.
	genericHTML, err := doc.Html()
	if err != nil {
		return extractedArticle{}, err
	}
	genericDoc, err := goquery.NewDocumentFromReader(strings.NewReader(genericHTML))
	if err != nil {
		return extractedArticle{}, err
	}
	genericContent := genericArticleContent(genericDoc)
	discardUnsupportedComponents(genericContent)
	genericHTML, err = genericDoc.Html()
	if err != nil {
		return extractedArticle{}, err
	}
	article, err := readability.FromReader(strings.NewReader(genericHTML), pageURL)
	var genericErr error
	if err != nil {
		genericErr = fmt.Errorf("readability parse: %w", err)
	}
	if article.Node == nil {
		if genericErr == nil {
			genericErr = fmt.Errorf("no readable content extracted")
		}
		article.Node = genericContent.Get(0)
	}
	if genericContent.Is("div#body.prose") {
		absolutizeContentURLs(genericContent, pageURL)
		article.Node = genericContent.Get(0)
	}
	if videoCount > 0 {
		got := goquery.NewDocumentFromNode(article.Node).Find(`a[data-reliquary-video]`).Length()
		if got == 0 && (content.Is("article") || content.Is("main")) {
			content.Children().EachWithBreak(func(_ int, header *goquery.Selection) bool {
				links := header.Find(`a[data-reliquary-video]`)
				if header.Find("h1").Length() == 0 || links.Length() != videoCount {
					return true
				}
				for i := links.Length() - 1; i >= 0; i-- {
					video := links.Eq(i).Parent().Get(0)
					video.Parent.RemoveChild(video)
					article.Node.InsertBefore(video, article.Node.FirstChild)
				}
				return false
			})
		}
	}
	page := &SitePage{Document: doc, Content: content, URL: pageURL, Article: &article, client: c}
	recovered, failures := c.adapterRegistry().recover(ctx, page)
	if ctx.Err() != nil {
		return extractedArticle{failures: failures}, ctx.Err()
	}
	if strings.TrimSpace(recovered.ContentHTML) == "" {
		var fetchErr *webfetch.FetchError
		if err := webfetch.ValidateReadableHTML([]byte(genericHTML)); errors.As(err, &fetchErr) {
			if fetchErr.Code == webfetch.ErrorJavaScriptRequired ||
				(fetchErr.Code == webfetch.ErrorEmptyContent && genericContent.Find("img").Length() == 0) {
				return extractedArticle{failures: failures}, err
			}
		}
	}
	if genericErr != nil && strings.TrimSpace(recovered.ContentHTML) == "" {
		return extractedArticle{failures: failures}, genericErr
	}
	article = *page.Article
	preferLargestImageVariant(goquery.NewDocumentFromNode(article.Node).Selection, pageURL)
	var buf bytes.Buffer
	if err := article.RenderHTML(&buf); err != nil {
		return extractedArticle{failures: failures}, fmt.Errorf("render article html: %w", err)
	}
	result := extractedArticle{contentHTML: firstNonEmpty(recovered.ContentHTML, buf.String()), title: firstNonEmpty(sourceHeading, article.Title()),
		byline: article.Byline(), siteName: article.SiteName(), publishedAt: firstNonEmpty(recovered.PublishedAt, articlePublishedAt(doc, articleURL)), failures: failures}
	if recovered.Author != "" {
		result.byline = recovered.Author
	}
	if recovered.Publisher != "" {
		result.siteName = recovered.Publisher
		if recovered.Author == "" {
			result.byline = ""
		}
	}
	return result, nil
}

func (c *client) preserveVideoLinks(ctx context.Context, content *goquery.Selection, pageURL *url.URL) (int, error) {
	count := 0
	for _, selector := range []string{"[or-mp4-video-url]", "video"} {
		content.Find(selector).Each(func(_ int, node *goquery.Selection) {
			raw, _ := node.Attr("or-mp4-video-url")
			if raw == "" {
				raw, _ = node.Attr("src")
			}
			if raw == "" {
				raw, _ = node.Find("source[src]").First().Attr("src")
			}
			ref, err := url.Parse(strings.TrimSpace(raw))
			if raw == "" || err != nil || ref.User != nil {
				node.Remove()
				return
			}
			videoURL := pageURL.ResolveReference(ref).String()
			if err := c.verifyVideoURL(ctx, videoURL); err != nil {
				node.Remove()
				return
			}
			label, _ := node.Attr("or-mp4-video-title")
			label = strings.TrimSpace(label)
			if label == "" && !node.Is("video") {
				label = strings.TrimSpace(node.Text())
			}
			if label == "" {
				label = "播放原始视频"
			}
			poster := ""
			if rawPoster, ok := node.Attr("poster"); ok && strings.TrimSpace(rawPoster) != "" {
				posterRef, posterErr := url.Parse(strings.TrimSpace(rawPoster))
				if posterErr != nil || posterRef.User != nil {
					node.Remove()
					return
				}
				posterURL := pageURL.ResolveReference(posterRef)
				if posterURL.Scheme != "http" && posterURL.Scheme != "https" {
					node.Remove()
					return
				}
				poster = `<img src="` + html.EscapeString(posterURL.String()) + `" alt="视频封面">`
				node.Siblings().Filter("img").FilterFunction(func(_ int, image *goquery.Selection) bool {
					src, _ := image.Attr("src")
					imageRef, err := url.Parse(strings.TrimSpace(src))
					return err == nil && pageURL.ResolveReference(imageRef).String() == posterURL.String()
				}).Remove()
			}
			node.ReplaceWithHtml(`<p>` + poster + `<a data-reliquary-video="true" href="` + html.EscapeString(videoURL) + `">` + html.EscapeString(label) + `</a></p>`)
			count++
		})
	}
	return count, ctx.Err()
}

func (c *client) verifyVideoURL(ctx context.Context, rawURL string) error {
	if err := utils.ValidateURLForSSRF(rawURL); err != nil {
		return fmt.Errorf("article video URL rejected: %w", err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("article video URL invalid: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("article video unavailable: %w", err)
	}
	defer resp.Body.Close()
	if (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent) || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "video/") {
		return fmt.Errorf("article video unavailable: HTTP %d, type %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var firstByte [1]byte
	if _, err := io.ReadFull(resp.Body, firstByte[:]); err != nil {
		return fmt.Errorf("article video unavailable: %w", err)
	}
	return nil
}

func discardUnsupportedComponents(content *goquery.Selection) bool {
	media := content.Find("iframe, video, audio, object, embed, canvas, [or-mp4-video-url]")
	placeholders := content.Find("div.animate-pulse").FilterFunction(func(_ int, node *goquery.Selection) bool {
		return strings.TrimSpace(node.Text()) == "" && node.Find("img, picture, svg").Length() == 0
	})
	dropped := media.Length()+placeholders.Length() > 0
	media.Remove()
	placeholders.Remove()
	return dropped
}

func preferLargestImageVariant(content *goquery.Selection, pageURL *url.URL) {
	content.Filter("img").AddSelection(content.Find("img")).Each(func(_ int, img *goquery.Selection) {
		variants, _ := img.Attr("data-srcset")
		if strings.TrimSpace(variants) == "" {
			variants, _ = img.Attr("srcset")
		}
		bestURL, bestSize := "", float64(0)
		for _, variant := range strings.Split(variants, ",") {
			fields := strings.Fields(variant)
			if len(fields) != 2 || (!strings.HasSuffix(fields[1], "w") && !strings.HasSuffix(fields[1], "x")) {
				continue
			}
			size, parseErr := strconv.ParseFloat(fields[1][:len(fields[1])-1], 64)
			if parseErr == nil && size > bestSize {
				bestURL, bestSize = fields[0], size
			}
		}
		if bestURL != "" {
			if variantURL, parseErr := url.Parse(bestURL); parseErr == nil {
				img.SetAttr("src", pageURL.ResolveReference(variantURL).String())
			}
		}
	})
}

func absolutizeContentURLs(content *goquery.Selection, pageURL *url.URL) {
	content.Filter("a[href],img[src]").AddSelection(content.Find("a[href],img[src]")).Each(func(_ int, element *goquery.Selection) {
		attr := "href"
		if element.Is("img") {
			attr = "src"
		}
		raw, _ := element.Attr(attr)
		ref, err := url.Parse(strings.TrimSpace(raw))
		if err == nil && ref.Scheme == "" {
			element.SetAttr(attr, pageURL.ResolveReference(ref).String())
		}
	})
}
