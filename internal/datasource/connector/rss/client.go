package rss

import (
	"bytes"
	"context"
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
	// OpenAI article pages challenge feed-style requests; request the original document.
	if !withAuthHeaders && req.URL.Hostname() == "openai.com" && strings.HasPrefix(req.URL.Path, "/index/") {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
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
	if selection.Length() == 0 {
		selection = doc.Find("div#body.prose")
	}
	var content *goquery.Selection
	selection.Each(func(_ int, candidate *goquery.Selection) {
		if content == nil || len(candidate.Text()) > len(content.Text()) {
			content = candidate
		}
	})
	if content == nil {
		// ponytail: page-wide fallback can include decorative media on pages without a content container.
		content = doc.Selection
	}
	if content.Is("main") {
		articleBody := content.Find(".blog-content")
		if articleBody.Length() == 1 && articleBody.Find("h1").Length() == 1 {
			content = articleBody
		}
	}
	sourceHeading := strings.TrimSpace(content.Find("h1").First().Text())
	pageURL, _ := url.Parse(articleURL)
	hadVideo := content.Find("video, [or-mp4-video-url]").Length() > 0
	videoCount, err := c.preserveVideoLinks(ctx, content, pageURL)
	if err != nil {
		return "", "", "", "", "", err
	}
	recoveredK25, err := c.recoverKimiK25(ctx, doc, content, pageURL)
	if err != nil && ctx.Err() != nil {
		return "", "", "", "", "", err
	}
	if err != nil {
		recoveredK25 = 0
	}
	recoveredK26, err := c.recoverKimiK26(ctx, doc, content, pageURL)
	if err != nil && ctx.Err() != nil {
		return "", "", "", "", "", err
	}
	if err != nil {
		recoveredK26 = 0
	}
	recoveredTables, err := recoverKimiTables(doc, content, pageURL)
	if err != nil {
		recoveredTables = 0
	}
	recoveredWidgets := recoveredK25 + recoveredK26 + recoveredTables
	droppedComponents := discardUnsupportedComponents(content)
	if hadVideo || recoveredWidgets > 0 || droppedComponents {
		page, renderErr := doc.Html()
		if renderErr != nil {
			return "", "", "", "", "", fmt.Errorf("render article page: %w", renderErr)
		}
		body = []byte(page)
	}
	article, err := readability.FromReader(bytes.NewReader(body), pageURL)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("readability parse: %w", err)
	}
	if article.Node == nil {
		return "", "", "", "", "", fmt.Errorf("no readable content extracted")
	}
	if content.Is("div#body.prose") {
		// The explicit article body includes media figures that Readability can discard.
		absolutizeContentURLs(content, pageURL)
		article.Node = content.Get(0)
	}
	if videoCount > 0 && recoveredWidgets == 0 {
		got := goquery.NewDocumentFromNode(article.Node).Find(`a[data-reliquary-video]`).Length()
		if got == 0 && (content.Is("article") || content.Is("main")) {
			// Readability can omit a video in the title/hero block while retaining the body.
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
	if err := preserveMetaHeroImage(doc, &article, pageURL); err != nil {
		return "", "", "", "", "", err
	}
	if recoveredWidgets > 0 {
		body := content.Find("div.max-w-none.markdown").First()
		if body.Length() > 0 {
			preferLargestImageVariant(body, pageURL)
			absolutizeContentURLs(body, pageURL)
			original, renderErr := body.Html()
			marker := "data-reliquary-kimi-table"
			if recoveredK25+recoveredK26 > 0 {
				marker = "data-reliquary-kimi-widget"
			}
			if renderErr == nil && strings.Count(original, marker) == recoveredWidgets {
				return original, firstNonEmpty(sourceHeading, article.Title()), article.Byline(), article.SiteName(), articlePublishedAt(doc, articleURL), nil
			}
		}
	}
	preferLargestImageVariant(goquery.NewDocumentFromNode(article.Node).Selection, pageURL)

	var buf bytes.Buffer
	if err := article.RenderHTML(&buf); err != nil {
		return "", "", "", "", "", fmt.Errorf("render article html: %w", err)
	}
	byline, siteName = article.Byline(), article.SiteName()
	if author, publisher := mistralPageAttribution(doc, pageURL); author != "" || publisher != "" {
		if author != "" {
			byline = author
		} else {
			byline, siteName = "", publisher
		}
	}
	return buf.String(), firstNonEmpty(sourceHeading, article.Title()), byline, siteName, articlePublishedAt(doc, articleURL), nil
}

func preserveMetaHeroImage(doc *goquery.Document, article *readability.Article, pageURL *url.URL) error {
	if pageURL.Hostname() != "ai.meta.com" || !strings.HasPrefix(pageURL.Path, "/blog/") {
		return nil
	}
	header := doc.Find("h1").First().Closest("._amuh")
	if header.Length() != 1 {
		return fmt.Errorf("Meta article header unavailable")
	}
	hero := header.Find("img[src]")
	if hero.Length() > 1 {
		return fmt.Errorf("Meta article hero image ambiguous")
	}
	if hero.Length() == 0 {
		return nil
	}
	raw, _ := hero.Attr("src")
	raw = strings.TrimSpace(raw)
	ref, err := url.Parse(raw)
	if raw == "" || err != nil || ref.User != nil || (ref.Scheme != "" && ref.Scheme != "http" && ref.Scheme != "https") {
		return fmt.Errorf("Meta article hero image URL invalid")
	}
	imageURL := pageURL.ResolveReference(ref).String()
	if goquery.NewDocumentFromNode(article.Node).Find("img[src]").FilterFunction(func(_ int, img *goquery.Selection) bool {
		src, _ := img.Attr("src")
		return src == imageURL
	}).Length() > 0 {
		return nil
	}
	hero.SetAttr("src", imageURL)
	image := hero.Get(0)
	image.Parent.RemoveChild(image)
	article.Node.InsertBefore(image, article.Node.FirstChild)
	return nil
}

func mistralPageAttribution(doc *goquery.Document, pageURL *url.URL) (author, publisher string) {
	if pageURL == nil || pageURL.Hostname() != "mistral.ai" || !strings.HasPrefix(pageURL.Path, "/news/") {
		return "", ""
	}
	doc.Find("h1").First().Parent().Find("p").EachWithBreak(func(_ int, paragraph *goquery.Selection) bool {
		text := strings.TrimSpace(paragraph.Text())
		if !strings.HasPrefix(text, "By ") {
			return true
		}
		byline := strings.TrimSpace(strings.TrimPrefix(text, "By "))
		if strings.HasPrefix(strings.ToLower(byline), "mistral") {
			publisher = byline
		} else {
			author = byline
		}
		return false
	})
	return author, publisher
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
