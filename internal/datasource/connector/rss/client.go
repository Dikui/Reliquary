package rss

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/PuerkitoBio/goquery"
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
	if isQwenBlogURL(feedURL) {
		return c.fetchFeed(ctx, qwenArticlesURL)
	}
	if isMiniMaxNewsURL(feedURL) {
		return c.fetchFeed(ctx, minimaxNewsAPIURL)
	}
	return c.fetchFeed(ctx, feedURL)
}

// extractArticle fetches an article page and returns the readability-cleaned
// main content as HTML, plus the extracted title and source fields. Returns an
// error if the page can't be fetched or no readable content is found.
func (c *client) extractArticle(ctx context.Context, articleURL string) (contentHTML, title, byline, siteName, publishedAt string, err error) {
	body, err := c.fetch(ctx, articleURL, maxArticleSize, false)
	if err != nil {
		return "", "", "", "", "", err
	}

	pageURL, _ := url.Parse(articleURL)
	article, err := readability.FromReader(bytes.NewReader(body), pageURL)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("readability parse: %w", err)
	}
	if article.Node == nil {
		return "", "", "", "", "", fmt.Errorf("no readable content extracted")
	}
	// Readability can discard embeds. Inspect the longest article/main element
	// in the source page so unrelated header/footer media do not block intake.
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("article HTML parse: %w", err)
	}
	selection := doc.Find("article")
	if selection.Length() == 0 {
		selection = doc.Find("main")
	}
	var content *goquery.Selection
	selection.Each(func(_ int, candidate *goquery.Selection) {
		if content == nil || len(candidate.Text()) > len(content.Text()) {
			content = candidate
		}
	})
	if content == nil {
		// ponytail: page-wide fallback may reject decorative media on pages without a content container.
		content = doc.Selection
	}
	if err := articleContentError(content); err != nil {
		return "", "", "", "", "", err
	}
	preferLargestImageVariant(goquery.NewDocumentFromNode(article.Node).Selection, pageURL)

	var buf bytes.Buffer
	if err := article.RenderHTML(&buf); err != nil {
		return "", "", "", "", "", fmt.Errorf("render article html: %w", err)
	}
	return buf.String(), article.Title(), article.Byline(), article.SiteName(), articlePublishedAt(doc, articleURL), nil
}

func articleContentError(content *goquery.Selection) error {
	if content.Find("iframe, video, audio, object, embed, canvas").Length() > 0 ||
		content.Find("[or-mp4-video-url]").FilterFunction(func(_ int, node *goquery.Selection) bool {
			value, _ := node.Attr("or-mp4-video-url")
			return strings.TrimSpace(value) != ""
		}).Length() > 0 {
		return fmt.Errorf("article contains unsupported embedded media")
	}
	if content.Find("div.animate-pulse").FilterFunction(func(_ int, node *goquery.Selection) bool {
		return strings.TrimSpace(node.Text()) == "" && node.Find("img, picture, svg").Length() == 0
	}).Length() > 0 {
		return fmt.Errorf("article contains unresolved content placeholder")
	}
	return nil
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
