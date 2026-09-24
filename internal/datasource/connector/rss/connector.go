package rss

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"reliquary/internal/datasource"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// Compile-time proof that *Connector satisfies the datasource.Connector interface.
var _ datasource.Connector = (*Connector)(nil)

// Connector implements datasource.Connector for RSS/Atom/JSON feeds.
type Connector struct{}

// NewConnector creates a new RSS connector.
func NewConnector() *Connector { return &Connector{} }

// Type returns the connector type identifier.
func (c *Connector) Type() string { return types.ConnectorTypeRSS }

// Validate verifies that every configured feed URL is reachable and parses as
// a valid feed.
func (c *Connector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	cli := newClient(cfg.parseHeaders())
	parser := gofeed.NewParser()

	for _, feedURL := range cfg.feedURLList() {
		data, err := cli.fetchSource(ctx, feedURL)
		if err != nil {
			return fmt.Errorf("fetch feed %s: %w", feedURL, err)
		}
		if isQwenBlogURL(feedURL) {
			if _, err := parseQwenArticles(data); err != nil {
				return fmt.Errorf("parse Qwen articles %s: %w", feedURL, err)
			}
			continue
		}
		if isMiniMaxNewsURL(feedURL) {
			if _, err := parseMiniMaxNews(data); err != nil {
				return fmt.Errorf("parse MiniMax news %s: %w", feedURL, err)
			}
			continue
		}
		if _, err := parser.Parse(bytes.NewReader(data)); err != nil {
			if isDeepSeekUpdatesURL(feedURL) {
				if _, changelogErr := parseDeepSeekUpdates(data, feedURL); changelogErr != nil {
					return fmt.Errorf("parse changelog %s: %w", feedURL, changelogErr)
				}
				continue
			}
			if _, sitemapErr := parseSitemap(data); sitemapErr != nil || cfg.ArticlePathPrefix == "" {
				if _, listingErr := parseListing(data, feedURL, cfg.ArticlePathPrefix); listingErr != nil {
					return fmt.Errorf("parse feed or article listing %s: %w", feedURL, listingErr)
				}
			}
		}
	}
	return nil
}

// ResolveResourceAncestors has nothing to do: feeds are a flat list with no
// nesting, so a selection has no ancestors to reveal.
func (c *Connector) ResolveResourceAncestors(
	ctx context.Context, config *types.DataSourceConfig, resourceIDs []string,
) ([]string, error) {
	return []string{}, nil
}

// ListResources returns one resource per configured feed URL. The feed is
// fetched so the resource can carry its real title; a feed that fails to fetch
// still appears (named by URL) with an error note, so the user can deselect it
// instead of the whole listing failing.
func (c *Connector) ListResources(
	ctx context.Context, config *types.DataSourceConfig, parentID string,
) ([]types.Resource, error) {
	// Feeds are flat: a lazy-load request for a specific parent has nothing extra.
	if parentID != "" {
		return []types.Resource{}, nil
	}

	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}
	cli := newClient(cfg.parseHeaders())
	parser := gofeed.NewParser()

	feedURLs := cfg.feedURLList()
	out := make([]types.Resource, 0, len(feedURLs))
	for _, feedURL := range feedURLs {
		res := types.Resource{
			ExternalID: feedURL,
			Type:       "feed",
			Name:       feedURL,
			URL:        feedURL,
		}
		data, err := cli.fetchSource(ctx, feedURL)
		if err != nil {
			logger.Warnf(ctx, "[RSS] list: fetch %s failed: %v", feedURL, err)
			res.Description = "fetch failed: " + err.Error()
			out = append(out, res)
			continue
		}
		if isQwenBlogURL(feedURL) {
			entries, err := parseQwenArticles(data)
			if err == nil {
				res.Name = firstNonEmpty(cfg.Publisher, feedURL)
				res.Metadata = map[string]interface{}{"item_count": len(entries)}
			} else {
				res.Description = "parse failed: " + err.Error()
			}
			out = append(out, res)
			continue
		}
		if isMiniMaxNewsURL(feedURL) {
			entries, err := parseMiniMaxNews(data)
			if err == nil {
				res.Name = firstNonEmpty(cfg.Publisher, feedURL)
				res.Metadata = map[string]interface{}{"item_count": len(entries)}
			} else {
				res.Description = "parse failed: " + err.Error()
			}
			out = append(out, res)
			continue
		}
		feed, err := parser.Parse(bytes.NewReader(data))
		if err != nil {
			if isDeepSeekUpdatesURL(feedURL) {
				entries, changelogErr := parseDeepSeekUpdates(data, feedURL)
				if changelogErr == nil {
					res.Name = firstNonEmpty(cfg.Publisher, feedURL)
					res.Metadata = map[string]interface{}{"item_count": len(entries)}
					out = append(out, res)
					continue
				}
				res.Description = "parse failed: " + changelogErr.Error()
				out = append(out, res)
				continue
			}
			if entries, sitemapErr := parseSitemap(data); sitemapErr == nil && cfg.ArticlePathPrefix != "" {
				res.Name = firstNonEmpty(cfg.Publisher, feedURL)
				sitemapURL, _ := url.Parse(feedURL)
				count := 0
				for _, entry := range entries {
					if sitemapArticleURL(entry.Loc, sitemapURL, cfg.ArticlePathPrefix) {
						count++
					}
				}
				res.Metadata = map[string]interface{}{"item_count": count}
				out = append(out, res)
				continue
			}
			if articles, listingErr := parseListing(data, feedURL, cfg.ArticlePathPrefix); listingErr == nil {
				res.Name = firstNonEmpty(cfg.Publisher, feedURL)
				res.Metadata = map[string]interface{}{"item_count": len(articles)}
				out = append(out, res)
				continue
			}
			logger.Warnf(ctx, "[RSS] list: parse %s failed: %v", feedURL, err)
			res.Description = "parse failed: " + err.Error()
			out = append(out, res)
			continue
		}
		if title := strings.TrimSpace(feed.Title); title != "" {
			res.Name = title
		}
		res.Description = strings.TrimSpace(feed.Description)
		if feed.Link != "" {
			res.URL = feed.Link
		}
		if feed.UpdatedParsed != nil {
			res.ModifiedAt = *feed.UpdatedParsed
		}
		res.Metadata = map[string]interface{}{"item_count": len(feed.Items)}
		out = append(out, res)
	}
	return out, nil
}

// FetchAll performs a full sync of the specified feeds (or all configured feeds
// when resourceIDs is empty).
func (c *Connector) FetchAll(
	ctx context.Context, config *types.DataSourceConfig, resourceIDs []string,
) ([]types.FetchedItem, error) {
	items, _, err := c.walk(ctx, config, resourceIDs, nil, false)
	return items, err
}

// FetchIncremental returns only items whose content fingerprint changed since
// the prior cursor. Deletions are intentionally not emitted (feeds drop old
// items as a matter of course).
func (c *Connector) FetchIncremental(
	ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor,
) ([]types.FetchedItem, *types.SyncCursor, error) {
	var prev *rssCursor
	if cursor != nil && cursor.ConnectorCursor != nil {
		var p rssCursor
		b, err := json.Marshal(cursor.ConnectorCursor)
		if err != nil {
			logger.Warnf(ctx, "[RSS] marshal connector cursor: %v", err)
		} else if err := json.Unmarshal(b, &p); err != nil {
			logger.Warnf(ctx, "[RSS] unmarshal connector cursor: %v", err)
		} else {
			prev = &p
		}
	}

	items, newCursor, err := c.walk(ctx, config, config.ResourceIDs, prev, true)
	if err != nil && newCursor == nil {
		return nil, nil, err
	}

	cursorMap := make(map[string]interface{})
	if newCursor != nil {
		b, marshalErr := json.Marshal(newCursor)
		if marshalErr != nil {
			logger.Warnf(ctx, "[RSS] marshal new cursor: %v", marshalErr)
		} else if unmarshalErr := json.Unmarshal(b, &cursorMap); unmarshalErr != nil {
			logger.Warnf(ctx, "[RSS] unmarshal new cursor to map: %v", unmarshalErr)
		}
	}

	var syncCursor *types.SyncCursor
	if newCursor != nil {
		syncCursor = &types.SyncCursor{
			LastSyncTime:    newCursor.LastSyncTime,
			ConnectorCursor: cursorMap,
		}
	}

	return items, syncCursor, err
}

// walk is the shared implementation for FetchAll / FetchIncremental.
//
// When incremental is true, items whose feed signal and content fingerprint are
// both unchanged are omitted from the result (ingest skipped) without fetching
// article pages. The returned cursor always reflects the best-known item state
// so a later sync can detect changes.
func (c *Connector) walk(
	ctx context.Context,
	config *types.DataSourceConfig,
	resourceIDs []string,
	prev *rssCursor,
	incremental bool,
) ([]types.FetchedItem, *rssCursor, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, nil, err
	}

	// Default to all configured feeds when no explicit selection was made.
	feedURLs := resourceIDs
	if len(feedURLs) == 0 {
		feedURLs = cfg.feedURLList()
	}

	cli := newClient(cfg.parseHeaders())
	parser := gofeed.NewParser()

	newCursor := &rssCursor{
		LastSyncTime: time.Now().UTC(),
		FeedItems:    make(map[string]map[string]string),
		FeedSignals:  make(map[string]map[string]string),
	}
	var out []types.FetchedItem
	var feedErrors []string

	for _, feedURL := range feedURLs {
		data, err := cli.fetchSource(ctx, feedURL)
		if err != nil {
			logger.Warnf(ctx, "[RSS] sync: fetch %s failed: %v", feedURL, err)
			feedErrors = append(feedErrors, fmt.Sprintf("%s: %v", feedURL, err))
			copyFeedCursor(newCursor, prev, feedURL)
			continue
		}
		var feed *gofeed.Feed
		if isQwenBlogURL(feedURL) {
			feed = &gofeed.Feed{Title: cfg.Publisher}
			feed.Items, err = parseQwenArticles(data)
		} else if isMiniMaxNewsURL(feedURL) {
			feed = &gofeed.Feed{Title: cfg.Publisher}
			feed.Items, err = parseMiniMaxNews(data)
		} else {
			feed, err = parser.Parse(bytes.NewReader(data))
		}
		fromMiniMax := isMiniMaxNewsURL(feedURL)
		needsArticleDate := fromMiniMax
		fromChangelog := false
		fromQwen := isQwenBlogURL(feedURL)
		if err != nil && (fromQwen || fromMiniMax) {
			logger.Warnf(ctx, "[RSS] sync: parse %s failed: %v", feedURL, err)
			feedErrors = append(feedErrors, fmt.Sprintf("%s: %v", feedURL, err))
			copyFeedCursor(newCursor, prev, feedURL)
			continue
		}
		if err != nil {
			feed = &gofeed.Feed{Title: cfg.Publisher}
			err = nil
			if isDeepSeekUpdatesURL(feedURL) {
				feed.Items, err = parseDeepSeekUpdates(data, feedURL)
				fromChangelog = true
			} else {
				entries, sitemapErr := parseSitemap(data)
				needsArticleDate = true
				if sitemapErr == nil && cfg.ArticlePathPrefix != "" {
					sitemapURL, _ := url.Parse(feedURL)
					for _, entry := range entries {
						if !sitemapArticleURL(entry.Loc, sitemapURL, cfg.ArticlePathPrefix) {
							continue
						}
						modified, valid := parseSiteDate(entry.LastMod)
						if !valid {
							continue
						}
						feed.Items = append(feed.Items, &gofeed.Item{GUID: entry.Loc, Link: entry.Loc,
							Updated: entry.LastMod, UpdatedParsed: &modified})
					}
				} else {
					feed.Items, err = parseListing(data, feedURL, cfg.ArticlePathPrefix)
				}
			}
			if err != nil {
				logger.Warnf(ctx, "[RSS] sync: parse %s failed: %v", feedURL, err)
				feedErrors = append(feedErrors, fmt.Sprintf("%s: %v", feedURL, err))
				copyFeedCursor(newCursor, prev, feedURL)
				continue
			}
		}

		newCursor.FeedItems[feedURL] = make(map[string]string)
		newCursor.FeedSignals[feedURL] = make(map[string]string)
		var prevItems map[string]string
		var prevSignals map[string]string
		if incremental && prev != nil {
			prevItems = prev.FeedItems[feedURL]
			if prev.FeedSignals != nil {
				prevSignals = prev.FeedSignals[feedURL]
			}
		}

		var kept, skipped int
		for _, item := range feed.Items {
			if item == nil {
				continue
			}
			itemID := firstNonEmpty(item.GUID, item.Link, item.Title)
			if itemID == "" {
				continue
			}
			if reason := item.Custom["error"]; fromQwen && reason != "" {
				if body := htmlToMarkdown(item.Content); body != "" {
					signal := feedSignalFingerprint(item, item.Content)
					if incremental && prevItems[itemID] != "" && prevSignals[itemID] == signal {
						newCursor.FeedItems[feedURL][itemID] = prevItems[itemID]
						newCursor.FeedSignals[feedURL][itemID] = signal
						skipped++
						continue
					}
					title := firstNonEmpty(item.Title, "Qwen article")
					author := ""
					if item.Author != nil {
						author = item.Author.Name
					}
					out = append(out, types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID), Title: title,
						Content: []byte(body), ContentType: "text/markdown", FileName: sanitizeFileName(title) + ".md",
						URL: item.Link, SourceResourceID: feedURL, Metadata: map[string]string{
							"channel": types.ChannelRSS, "feed_url": feedURL, "feed_title": feed.Title,
							"guid": item.GUID, "link": item.Link, "author": author, "publisher": cfg.Publisher,
							"published_at": item.Published, "intake_reject_reason": reason,
						}})
					newCursor.FeedItems[feedURL][itemID] = contentFingerprint(body)
					newCursor.FeedSignals[feedURL][itemID] = signal
					kept++
					continue
				}
				out = append(out, types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID),
					Title:    firstNonEmpty(item.Title, item.Link, item.GUID),
					Metadata: map[string]string{"error": reason, "channel": types.ChannelRSS}})
				kept++
				continue
			}
			published := item.PublishedParsed
			if needsArticleDate {
				published = item.UpdatedParsed // discovery filter only; the article supplies the real publication date.
			}
			if published == nil || published.IsZero() {
				logger.Warnf(ctx, "[RSS] skipping item without date signal: %s", itemID)
				continue
			}
			if !config.IngestSince.IsZero() && published.Before(config.IngestSince) {
				skipped++
				continue
			}

			feedContent := firstNonEmpty(item.Content, item.Description)
			feedSig := feedSignalFingerprint(item, feedContent)

			if incremental && prevItems != nil {
				prevFP := prevItems[itemID]
				prevSig := ""
				if prevSignals != nil {
					prevSig = prevSignals[itemID]
				}
				if prevFP != "" && feedSig == prevSig {
					newCursor.FeedItems[feedURL][itemID] = prevFP
					newCursor.FeedSignals[feedURL][itemID] = feedSig
					skipped++
					continue
				}
			}

			resolved := c.resolveItem(ctx, cli, feed, item, feedURL, itemID, cfg.Publisher, fromChangelog || fromQwen)
			if needsArticleDate && resolved.item.Metadata["error"] == "" {
				actualDate, valid := parseSiteDate(resolved.item.Metadata["published_at"])
				if !valid {
					resolved.item.Content, resolved.item.URL = nil, ""
					resolved.item.Metadata["error"] = "article publication date missing"
				} else if !config.IngestSince.IsZero() && actualDate.Before(config.IngestSince) {
					newCursor.FeedItems[feedURL][itemID] = resolved.fingerprint
					newCursor.FeedSignals[feedURL][itemID] = feedSig
					skipped++
					continue
				}
			}
			if resolved.item.Metadata["error"] != "" {
				if prevItems[itemID] != "" {
					newCursor.FeedItems[feedURL][itemID] = prevItems[itemID]
					newCursor.FeedSignals[feedURL][itemID] = prevSignals[itemID]
				}
				out = append(out, resolved.item)
				kept++
				continue
			}
			newCursor.FeedItems[feedURL][itemID] = resolved.fingerprint
			newCursor.FeedSignals[feedURL][itemID] = feedSig

			if incremental && prevItems != nil && prevItems[itemID] == resolved.fingerprint {
				skipped++
				continue
			}
			kept++
			out = append(out, resolved.item)
		}

		logger.Infof(ctx, "[RSS] feed %s: items=%d fetched=%d skipped=%d",
			feedURL, len(feed.Items), kept, skipped)
	}

	if len(feedErrors) > 0 {
		if len(out) == 0 && len(feedErrors) == len(feedURLs) {
			return nil, newCursor, fmt.Errorf("all feeds failed: %s", strings.Join(feedErrors, "; "))
		}
		return out, newCursor, &datasource.PartialFetchError{Details: feedErrors}
	}

	return out, newCursor, nil
}

type resolvedFeedItem struct {
	item        types.FetchedItem
	fingerprint string
}

// resolveItem assembles a FetchedItem for a single feed entry, resolving the
// best available content and converting it to Markdown. Only a parsed
// changelog section or official article API body is trusted as complete inline
// content; feeds are not.
func (c *Connector) resolveItem(
	ctx context.Context,
	cli *client,
	feed *gofeed.Feed,
	item *gofeed.Item,
	feedURL, itemID, publisher string,
	inlineOriginal bool,
) resolvedFeedItem {
	title := firstNonEmpty(item.Title, "untitled")

	// A feed summary is never evidence that the article was fully captured.
	if strings.TrimSpace(item.Link) == "" {
		return resolvedFeedItem{item: types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID), Title: title,
			Metadata: map[string]string{"error": "missing article URL", "channel": types.ChannelRSS}}}
	}
	var contentHTML, articleTitle, articleByline, articleSiteName, pagePublishedAt string
	var err error
	if inlineOriginal {
		contentHTML, pagePublishedAt = item.Content, item.Published
		var doc *goquery.Document
		doc, err = goquery.NewDocumentFromReader(strings.NewReader(contentHTML))
		if err == nil {
			var pageURL *url.URL
			pageURL, err = url.Parse(item.Link)
			if err == nil {
				var videoCount int
				videoCount, err = cli.preserveVideoLinks(ctx, doc.Selection, pageURL)
				if err == nil {
					err = articleContentError(doc.Selection)
				}
				if err == nil && videoCount > 0 {
					contentHTML, err = doc.Html()
				}
			}
		}
	} else {
		contentHTML, articleTitle, articleByline, articleSiteName, pagePublishedAt, err = cli.extractArticle(ctx, item.Link)
	}
	if err != nil {
		stage := "article fetch failed"
		if inlineOriginal {
			stage = "original content failed"
		}
		logger.Warnf(ctx, "[RSS] %s for %s: %v", stage, item.Link, err)
		return resolvedFeedItem{item: types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID), Title: title,
			Metadata: map[string]string{"error": stage + ": " + err.Error(), "channel": types.ChannelRSS}}}
	}
	if articleTitle != "" && (item.Title == "" || item.Custom["listing_title_needs_article"] == "true") {
		title = articleTitle
	}
	content := htmlToMarkdown(contentHTML)
	if strings.TrimSpace(content) == "" {
		return resolvedFeedItem{item: types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID), Title: title,
			Metadata: map[string]string{"error": "article body is empty", "channel": types.ChannelRSS}}}
	}

	updatedAt := time.Time{}
	switch {
	case item.UpdatedParsed != nil && !item.UpdatedParsed.IsZero():
		updatedAt = *item.UpdatedParsed
	case item.PublishedParsed != nil && !item.PublishedParsed.IsZero():
		updatedAt = *item.PublishedParsed
	}

	author := ""
	if item.Author != nil {
		author = item.Author.Name
	}
	if author == "" {
		author = articleByline
	}
	if articleSiteName != "" {
		publisher = articleSiteName
	}
	if strings.EqualFold(strings.TrimSpace(author), strings.TrimSpace(publisher)) {
		author = "" // An organization is the publisher, not a separate personal author.
	}

	publishedAt := item.Published
	if publishedAt == "" {
		publishedAt = pagePublishedAt
	}
	return resolvedFeedItem{
		fingerprint: contentFingerprint(content),
		item: types.FetchedItem{
			ExternalID:       itemExternalID(feedURL, itemID),
			Title:            title,
			Content:          []byte(content),
			ContentType:      "text/markdown",
			FileName:         sanitizeFileName(title) + ".md",
			URL:              item.Link,
			UpdatedAt:        updatedAt,
			SourceResourceID: feedURL,
			Metadata: map[string]string{
				"channel":      types.ChannelRSS,
				"feed_url":     feedURL,
				"feed_title":   feed.Title,
				"guid":         item.GUID,
				"link":         item.Link,
				"author":       author,
				"publisher":    publisher,
				"published_at": publishedAt,
			},
		},
	}
}

// FetchArticle extracts a discovered page using the same full-text and source
// evidence rules as RSS, without requiring the page to appear in a feed.
func FetchArticle(ctx context.Context, articleURL string) (*types.FetchedItem, error) {
	resolved := NewConnector().resolveItem(ctx, newClient(nil), &gofeed.Feed{}, &gofeed.Item{Link: articleURL}, "", articleURL, "", false)
	if reason := resolved.item.Metadata["error"]; reason != "" {
		return nil, fmt.Errorf("fetch original article: %s", reason)
	}
	return &resolved.item, nil
}

// htmlToMarkdown converts HTML to Markdown, returning the trimmed original on
// conversion failure so we never silently drop content.
func htmlToMarkdown(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	conv := converter.NewConverter(converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(), table.NewTablePlugin(table.WithNewlineBehavior(table.NewlineBehaviorPreserve))))
	md, err := conv.ConvertString(html)
	if err != nil || strings.TrimSpace(md) == "" {
		return strings.TrimSpace(html)
	}
	return strings.TrimSpace(md)
}
