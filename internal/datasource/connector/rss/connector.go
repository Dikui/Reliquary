package rss

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
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
		data, err := cli.fetchFeed(ctx, feedURL)
		if err != nil {
			return fmt.Errorf("fetch feed %s: %w", feedURL, err)
		}
		if _, err := parser.Parse(bytes.NewReader(data)); err != nil {
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
		data, err := cli.fetchFeed(ctx, feedURL)
		if err != nil {
			logger.Warnf(ctx, "[RSS] list: fetch %s failed: %v", feedURL, err)
			res.Description = "fetch failed: " + err.Error()
			out = append(out, res)
			continue
		}
		feed, err := parser.Parse(bytes.NewReader(data))
		if err != nil {
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
		data, err := cli.fetchFeed(ctx, feedURL)
		if err != nil {
			logger.Warnf(ctx, "[RSS] sync: fetch %s failed: %v", feedURL, err)
			feedErrors = append(feedErrors, fmt.Sprintf("%s: %v", feedURL, err))
			copyFeedCursor(newCursor, prev, feedURL)
			continue
		}
		feed, err := parser.Parse(bytes.NewReader(data))
		fromSitemap := false
		if err != nil {
			entries, sitemapErr := parseSitemap(data)
			fromSitemap = true
			feed = &gofeed.Feed{Title: cfg.Publisher}
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
			} else if articles, listingErr := parseListing(data, feedURL, cfg.ArticlePathPrefix); listingErr == nil {
				feed.Items = articles
			} else {
				logger.Warnf(ctx, "[RSS] sync: parse %s failed: %v", feedURL, listingErr)
				feedErrors = append(feedErrors, fmt.Sprintf("%s: invalid feed, sitemap or article listing", feedURL))
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
			published := item.PublishedParsed
			if fromSitemap {
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

			resolved := c.resolveItem(ctx, cli, feed, item, feedURL, itemID, cfg.Publisher)
			if fromSitemap && resolved.item.Metadata["error"] == "" {
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
// best available content (full-text article > feed content) and converting it
// to Markdown.
func (c *Connector) resolveItem(
	ctx context.Context,
	cli *client,
	feed *gofeed.Feed,
	item *gofeed.Item,
	feedURL, itemID, publisher string,
) resolvedFeedItem {
	title := firstNonEmpty(item.Title, "untitled")

	// A feed summary is never evidence that the article was fully captured.
	if strings.TrimSpace(item.Link) == "" {
		return resolvedFeedItem{item: types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID), Title: title,
			Metadata: map[string]string{"error": "missing article URL", "channel": types.ChannelRSS}}}
	}
	contentHTML, articleTitle, articleByline, articleSiteName, pagePublishedAt, err := cli.extractArticle(ctx, item.Link)
	if err != nil {
		logger.Warnf(ctx, "[RSS] full-text fetch failed for %s: %v", item.Link, err)
		return resolvedFeedItem{item: types.FetchedItem{ExternalID: itemExternalID(feedURL, itemID), Title: title,
			Metadata: map[string]string{"error": "article fetch failed: " + err.Error(), "channel": types.ChannelRSS}}}
	}
	if item.Title == "" && articleTitle != "" {
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

// htmlToMarkdown converts HTML to Markdown, returning the trimmed original on
// conversion failure so we never silently drop content.
func htmlToMarkdown(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	md, err := htmltomd.ConvertString(html)
	if err != nil || strings.TrimSpace(md) == "" {
		return strings.TrimSpace(html)
	}
	return strings.TrimSpace(md)
}
